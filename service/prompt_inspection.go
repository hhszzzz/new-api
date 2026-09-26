package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	goahocorasick "github.com/anknown/ahocorasick"
	"github.com/gin-gonic/gin"
)

type promptWordlistRuntime struct {
	ID            string
	Name          string
	Version       string
	TargetVersion string
	Generation    int64
	Enabled       bool
	Action        string
	Matcher       *goahocorasick.Machine
}

type promptWordlistSnapshot struct {
	Libraries map[string]promptWordlistRuntime
}

var promptWordlists atomic.Pointer[promptWordlistSnapshot]
var promptWordlistRefreshMu sync.Mutex
var promptWordlistCompileMu sync.Mutex
var promptWordlistRunnerOnce sync.Once
var manualMatcherMu sync.Mutex
var manualMatcherVersion uint64
var manualMatcherInitialized bool
var manualMatcher *goahocorasick.Machine

func currentManualWordMatcher() *goahocorasick.Machine {
	manualMatcherMu.Lock()
	defer manualMatcherMu.Unlock()
	version := setting.SensitiveWordsVersion()
	if !manualMatcherInitialized || manualMatcherVersion != version {
		words := setting.SensitiveWordsSnapshot()
		manualMatcher = nil
		if len(words) > 0 {
			manualMatcher = InitAc(words)
		}
		manualMatcherVersion, manualMatcherInitialized = version, true
	}
	return manualMatcher
}

// Refresh publishes metadata immediately, independently of dictionary loading.
// A slow or failed compilation must never delay a disable/delete operation.
func RefreshPromptWordlists() error {
	promptWordlistRefreshMu.Lock()
	defer promptWordlistRefreshMu.Unlock()
	rows, err := model.ListPromptWordlists()
	if err != nil {
		return err
	}
	previous := promptWordlists.Load()
	next := &promptWordlistSnapshot{Libraries: make(map[string]promptWordlistRuntime, len(rows))}
	for _, row := range rows {
		id := strconv.FormatInt(row.ID, 10)
		entry := promptWordlistRuntime{ID: id, Name: row.Name, TargetVersion: row.ContentHash, Generation: row.Generation, Enabled: row.Enabled && row.ContentHash != "", Action: prompt_audit_setting.NormalizeWordlistAction(row.Action)}
		if previous != nil {
			old := previous.Libraries[id]
			entry.Matcher, entry.Version = old.Matcher, old.Version
		}
		next.Libraries[id] = entry
	}
	promptWordlists.Store(next)
	return nil
}

func compilePromptWordlists() error {
	promptWordlistCompileMu.Lock()
	defer promptWordlistCompileMu.Unlock()
	snapshot := promptWordlists.Load()
	if snapshot == nil {
		return nil
	}
	var failures []error
	for id, entry := range snapshot.Libraries {
		if !entry.Enabled || (entry.Matcher != nil && entry.Version == entry.TargetVersion) {
			continue
		}
		rowID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		stored, err := model.GetPromptWordlist(rowID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !stored.Enabled || stored.ContentHash != entry.TargetVersion || stored.Generation != entry.Generation {
			continue
		}
		matcher := InitAc(strings.Split(string(stored.Content), "\n"))
		if matcher == nil {
			failures = append(failures, errors.New("wordlist compilation failed"))
			continue
		}
		promptWordlistRefreshMu.Lock()
		current := promptWordlists.Load()
		latest, exists := current.Libraries[id]
		if exists && latest.Enabled && latest.Generation == entry.Generation && latest.TargetVersion == entry.TargetVersion {
			libraries := maps.Clone(current.Libraries)
			latest.Matcher, latest.Version = matcher, entry.TargetVersion
			libraries[id] = latest
			promptWordlists.Store(&promptWordlistSnapshot{Libraries: libraries})
		}
		promptWordlistRefreshMu.Unlock()
	}
	return errors.Join(failures...)
}

type PromptWordlistMatch struct {
	ID      string               `json:"id"`
	Name    string               `json:"name"`
	Version string               `json:"version"`
	Scope   dto.PromptAuditScope `json:"scope"`
	Action  string               `json:"action"`
}

// promptInspectionUsesBlockingSnapshot decides whether the model audit of an
// input is scoped to the latest turn. Only blocking mode narrows: async
// observation reads the whole request after the fact, off the request path.
//
// Output direction is never narrowed — the whole generated answer is the payload
// under inspection, and there is no "latest turn" to narrow to.
func promptInspectionUsesBlockingSnapshot(direction string, configured prompt_audit_setting.PromptAuditSetting) bool {
	if direction != PromptAuditDirectionInput {
		return false
	}
	return configured.Mode == prompt_audit_setting.ModeBlocking && configured.BlockingLatestTurnOnly
}

// promptWordlistUsesBlockingSnapshot decides whether the wordlist gate reads
// only the latest turn of an input. The gate blocks synchronously in every mode,
// including with the model audit off, so it follows the latest-turn switch
// whatever the model audit's mode is: with the switch on, a listed word left in
// an older turn must not refuse every later request of the conversation; with
// it off, the gate reads the whole request, as the model audit then does.
func promptWordlistUsesBlockingSnapshot(direction string, configured prompt_audit_setting.PromptAuditSetting) bool {
	return direction == PromptAuditDirectionInput && configured.BlockingLatestTurnOnly
}

func matchPromptWordlists(snapshot dto.PromptAuditSnapshot, configured prompt_audit_setting.PromptAuditSetting) (*PromptWordlistMatch, error) {
	if !setting.ShouldCheckPromptSensitive() {
		return nil, nil
	}
	libraries := promptWordlists.Load()
	var reviewMatch *PromptWordlistMatch
	for _, segment := range snapshot.PrioritizedSegments() {
		scope := segment.SourceScope()
		for _, id := range configured.PolicyFor(scope).LibraryIDs {
			var entry promptWordlistRuntime
			if id == prompt_audit_setting.ManualWordlistID {
				if !configured.ManualWordlistActive() {
					continue
				}
				entry = promptWordlistRuntime{ID: id, Name: "Custom wordlist", Version: strconv.FormatUint(setting.SensitiveWordsVersion(), 10), Enabled: true, Action: prompt_audit_setting.NormalizeWordlistAction(configured.ManualWordlistAction), Matcher: currentManualWordMatcher()}
			} else if libraries == nil {
				return nil, errors.New("wordlists are not initialized")
			} else {
				var exists bool
				entry, exists = libraries.Libraries[id]
				if !exists {
					return nil, errors.New("wordlist is unavailable")
				}
			}
			if !entry.Enabled {
				continue
			}
			if id != prompt_audit_setting.ManualWordlistID &&
				(entry.Matcher == nil || entry.Version != entry.TargetVersion) {
				return nil, errors.New("wordlist is unavailable")
			}
			if len(sensitiveMachineMatches(segment.Text, entry.Matcher, true)) > 0 {
				match := &PromptWordlistMatch{ID: id, Name: entry.Name, Version: entry.Version, Scope: scope, Action: prompt_audit_setting.NormalizeWordlistAction(entry.Action)}
				if match.Action == prompt_audit_setting.WordlistActionBlock {
					return match, nil
				}
				if reviewMatch == nil {
					reviewMatch = match
				}
			}
		}
	}
	return reviewMatch, nil
}

// TestPromptWordlists uses exactly the active enforcement rules. It does not
// persist supplied text, bill, or call an external model.
func TestPromptWordlists(scope dto.PromptAuditScope, text string) (*PromptWordlistMatch, bool, error) {
	configured := prompt_audit_setting.GetSetting()
	if configured.ExpandBase64 {
		// Test the same text the live gate tests: a list that catches the decoded
		// form would otherwise report a miss here.
		text = expandPromptAuditBase64(text)
	}
	match, err := matchPromptWordlists(dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{{Scope: scope, Role: string(scope), Text: text}}}, configured)
	return match, configured.Mode != prompt_audit_setting.ModeOff && configured.PolicyFor(scope).ModelAudit, err
}

func normalizeProbePhrase(value string) string {
	return strings.ToLower(strings.TrimFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) }))
}

// standaloneProbeInput returns the text of a request that is nothing but one
// standalone user turn: no history, no media, no assistant or tool traffic, and
// exactly one non-empty user segment. Both probe detectors share it, so the
// semantic one is only asked about the shape the phrase list would have been
// compared against.
func standaloneProbeInput(snapshot dto.PromptAuditSnapshot) (string, bool) {
	if snapshot.HasHistory || snapshot.HasMedia {
		return "", false
	}
	var inputs []string
	for _, segment := range snapshot.OrderedSegments() {
		if segment.ToolDefinition {
			continue
		}
		if segment.ToolPart != "" {
			return "", false
		}
		switch segment.SourceScope() {
		case dto.PromptScopeAssistant, dto.PromptScopeToolCall, dto.PromptScopeToolResult, dto.PromptScopeTask, dto.PromptScopeMCP:
			return "", false
		case dto.PromptScopeUser:
			inputs = append(inputs, segment.Text)
		}
	}
	if len(inputs) != 1 {
		return "", false
	}
	trimmed := strings.TrimSpace(inputs[0])
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

func isBlockedProbe(snapshot dto.PromptAuditSnapshot, phrases []string) bool {
	input, ok := standaloneProbeInput(snapshot)
	if !ok {
		return false
	}
	normalized := normalizeProbePhrase(input)
	if normalized == "" {
		return false
	}
	for _, phrase := range phrases {
		if normalized == normalizeProbePhrase(phrase) {
			return true
		}
	}
	return false
}

// semanticProbeQuestionID is the answer key of the liveness-probe question. It
// is not a category ID, so a probability kept under it can never decide a
// category-based verdict.
const semanticProbeQuestionID = "probe"

// semanticProbeTimeoutMillis bounds one probe. A probe holds the caller's
// request just like an audit chunk does, so it gets the node's own timeout.
//
// semanticProbeQuestions is the single question asked on this path. It is asked
// alone rather than with the category set: the category questions are about harm,
// and a liveness check is not harmful, so a node asked both at once tends to
// score the probe-safe decision on the category answers instead of on its own.
//
// The question goes in instructions and the boundary in criteria, the way every
// TypeSafe question is shaped. The true side covers the three shapes a real
// probe takes: a bare liveness check, a probe of which model answers, and a
// fragment with no work to perform. The false side names what real work looks
// like, so a short genuine question is not pulled into the true side by its
// brevity alone.
var semanticProbeQuestions = map[string]promptAuditTypeSafeQuestionBody{
	semanticProbeQuestionID: {
		Type:         "noul",
		Instructions: "Does this message carry nothing to act on — is it sent only to check that the service is reachable, to learn which model answers, or as a bare greeting, rather than to ask for content?",
		Criteria: &promptAuditTypeSafeCriteria{
			True:  "A liveness check, a connectivity test, a 'ping', an 'are you there', a 'can you be used', a 'which model are you', a 'test', a 'reply OK', a bare greeting, or a meaningless fragment. There is no work to perform.",
			False: "It asks for real work: an explanation, an answer, code, analysis, data, a translation, an opinion, or any other content the assistant produces.",
		},
	},
}

// semanticProbeCacheKey identifies one probe verdict. The question version and
// the threshold are part of it: raising the threshold must not reuse verdicts
// that were computed below the new one.
func semanticProbeCacheKey(setting prompt_audit_setting.PromptAuditSetting, endpoint prompt_audit_setting.Endpoint, text string) string {
	digest := sha256.Sum256([]byte(setting.ConfigVersion + "|" + promptAuditTypeSafeProbeVersion + "|" + endpoint.ID + "|" + endpoint.Model + "|" + strconv.FormatFloat(setting.ProbeSemanticThreshold, 'f', -1, 64) + "|" + text))
	return "new-api:prompt-audit:probe:v1:" + hex.EncodeToString(digest[:])
}

// semanticProbeScores asks the first enabled TypeSafe node whether a short
// standalone message is a liveness probe. The returned map always carries the
// probability when a verdict was reached, blocked or not, so the caller can
// record it; an empty map means the probe was skipped or failed.
//
// The probe is an extra gate on top of the wordlist and the model audit, and it
// must never be the reason traffic is refused. A node that is saturated, down,
// slow, or answering nonsense therefore logs and lets the request continue to
// the ordinary audit instead of failing the call.
func semanticProbeScores(c *gin.Context, setting prompt_audit_setting.PromptAuditSetting, snapshot dto.PromptAuditSnapshot) (map[string]float64, string, string) {
	text, ok := standaloneProbeInput(snapshot)
	if !ok || utf8.RuneCountInString(text) > prompt_audit_setting.MaxProbeSemanticRunes {
		return nil, "", ""
	}
	var endpoint prompt_audit_setting.Endpoint
	found := false
	for _, candidate := range enabledPromptAuditEndpoints(setting, PromptAuditDirectionInput) {
		if candidate.IsTypeSafeEndpoint() {
			endpoint, found = candidate, true
			break
		}
	}
	if !found {
		return nil, "", ""
	}
	cacheKey := semanticProbeCacheKey(setting, endpoint, text)
	if cached, ok := getPromptAuditCache(c.Request.Context(), cacheKey); ok {
		return cached.Scores, cached.EndpointID, cached.EndpointModel
	}
	limit := endpoint.Concurrency
	if limit <= 0 {
		limit = setting.EndpointConcurrency
	}
	slots := promptAuditSlots(&promptAuditEndpointSlots, setting.ConfigVersion+"|"+endpoint.ID+"|"+strconv.Itoa(limit), limit)
	select {
	case slots <- struct{}{}:
	default:
		return nil, "", ""
	}
	defer func() { <-slots }()

	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(endpoint.TimeoutMS)*time.Millisecond)
	defer cancel()
	scores, model, err := callTypeSafe(ctx, endpoint, promptAuditTypeSafeState{Message: text}, semanticProbeQuestions, promptAuditTypeSafeProbeVersion)
	if err != nil {
		logger.LogWarn(c, "prompt audit semantic probe failed: "+promptAuditErrorCode(err))
		return nil, "", ""
	}
	probability := scores[semanticProbeQuestionID]
	if model == "" {
		model = endpoint.Model
	}
	cached := PromptAuditResult{EndpointID: endpoint.ID, EndpointModel: model, Blocked: probability >= setting.ProbeSemanticThreshold, Scores: map[string]float64{semanticProbeQuestionID: probability}}
	// The negative verdict is cached as well: an uptime probe is the same few
	// bytes on every call, and it must not cost a TypeSafe call each time.
	setPromptAuditCache(cacheKey, cached, time.Duration(setting.CacheTTLSeconds)*time.Second)
	return cached.Scores, cached.EndpointID, cached.EndpointModel
}

func InspectPrompt(c *gin.Context, request PromptAuditRequest) (PromptAuditResult, *hosttypes.NewAPIError) {
	request = preparePromptAuditRequest(c, request)
	configured := prompt_audit_setting.GetSetting()
	group := effectivePromptAuditGroup(c)
	auditConfigured := configured.Mode != prompt_audit_setting.ModeOff && configured.AppliesToGroupForMode(group, configured.Mode)
	auditEnabled := auditConfigured || setting.ShouldCheckPromptSensitive()

	direction := strings.ToLower(strings.TrimSpace(request.Direction))
	if direction == "" {
		direction = PromptAuditDirectionInput
	}

	// An inline base64 run is expanded before any gate reads the text: a client
	// that encodes its real request must not be matched on the encoding. The
	// original snapshot stays as it was — it is what the request actually
	// carried, and it is what the stored full prompt shows.
	rawSnapshot := request.Snapshot
	if configured.ExpandBase64 {
		expanded, changed := expandPromptAuditSnapshotBase64(request.Snapshot)
		if changed {
			request.RawFullText = promptAuditJoinedText(request.Snapshot, "")
			request.Snapshot = expanded
		}
	}

	wordlistSnapshot := request.Snapshot
	if promptWordlistUsesBlockingSnapshot(direction, configured) {
		wordlistSnapshot = wordlistSnapshot.BlockingSnapshot()
	}
	// The probe gate runs before the wordlist because a listed greeting would
	// otherwise be reported as a wordlist hit rather than as a probe. The
	// exemptions match the pre-existing phrase gate exactly: token-count calls,
	// incomplete coverage, and compaction requests keep the shortcut they had.
	probeText := request.Snapshot.HumanPrompt
	if probeText == "" {
		probeText = request.Snapshot.Text()
	}
	// Administrators keep it too unless the operator asks otherwise, because the
	// gate refuses liveness probes and an administrator's own health checks are
	// the traffic it would otherwise refuse by accident.
	probeExempt := request.WordlistOnly || request.CoverageIncomplete ||
		request.Protocol == "openai_responses_compaction"
	if !configured.ProbeIncludeAdmins {
		probeExempt = probeExempt || contextInt(c, "role") >= common.RoleAdminUser
	}
	probeBlocked := false
	var probeScores map[string]float64
	var probeEndpointID, probeEndpointModel string
	if direction == PromptAuditDirectionInput && configured.ProbeBlockEnabled && !probeExempt {
		// The probe judgement reads the text as the client sent it: an expanded
		// blob decodes to something long, and only the raw turn is what the
		// caller literally sent as its whole request.
		probeBlocked = isBlockedProbe(rawSnapshot, configured.ProbePhrases)
		// The phrase list matches only the exact greetings it was given. The
		// semantic gate catches the rest of a probe's surface, at the cost of one
		// TypeSafe call, and only for text short enough to be one. The scores are
		// kept even when the verdict is below the threshold, so every probe call
		// leaves a measurable trace.
		if !probeBlocked && configured.ProbeSemanticEnabled {
			probeScores, probeEndpointID, probeEndpointModel = semanticProbeScores(c, configured, rawSnapshot)
			probeBlocked = probeScores != nil && probeScores[semanticProbeQuestionID] >= configured.ProbeSemanticThreshold
		}
	}
	if !probeBlocked && len(probeScores) > 0 {
		// The semantic probe ran but the text scored below the threshold. Stash
		// the score on the context so the ordinary audit can pick it up and
		// record it, without changing the outcome.
		c.Set("prompt_audit_probe_scores", probeScores)
		c.Set("prompt_audit_probe_endpoint", probeEndpointID+":"+probeEndpointModel)
	}
	if probeBlocked {
		text := probeText
		digest := sha256.Sum256([]byte(text))
		result := PromptAuditResult{Enabled: true, Reviewed: true, Blocked: true, Mode: configured.Mode, Direction: direction, CoverageComplete: true, Decision: PromptAuditDecisionBlock, Outcome: PromptAuditDecisionBlock, Safety: "", ConfigVersion: configured.ConfigVersion, ActualAction: PromptAuditActionBlock, InputChars: utf8.RuneCountInString(text), InputSHA256: hex.EncodeToString(digest[:]), SegmentCount: len(request.Snapshot.Segments), InspectionType: "probe_block", EndpointID: probeEndpointID, EndpointModel: probeEndpointModel, Scores: probeScores}
		audit := &model.PromptAudit{RequestID: resultRequestID(c), UserID: contextInt(c, "id"), TokenID: contextInt(c, "token_id"), TokenName: contextString(c, "token_name"), GroupName: group, Protocol: request.Protocol, ModelName: request.Model, Stage: normalizedPromptAuditStage(request.Stage), Direction: direction, CoverageComplete: true, ConfigVersion: configured.ConfigVersion, ExecutionMode: result.Mode, Status: model.PromptAuditStatusDone, PromptHash: result.InputSHA256, PromptLength: result.InputChars, SegmentCount: result.SegmentCount, Decision: result.Decision, Safety: result.Safety, WouldAction: result.ActualAction, InspectionType: "probe_block", Action: result.ActualAction, EndpointID: result.EndpointID, EndpointModel: result.EndpointModel, CompletedAt: common.GetTimestamp()}
		if len(result.Scores) > 0 {
			if data, marshalErr := common.Marshal(result.Scores); marshalErr == nil {
				audit.Scores = string(data)
			}
		}
		applyPromptAuditRequestContext(audit, c)
		setPromptAuditContent(audit, text, configured.FullPromptRetentionLimit())
		setPromptAuditInputContext(audit, request)
		payload, _ := common.Marshal(promptAuditPayload{Version: 1, Direction: direction, CoverageComplete: true, Segments: request.Snapshot.OrderedSegments()})
		audit.ScanPayload, audit.ScanPayloadTruncated = model.RetainPromptAuditPayload(payload)
		if createErr := model.CreatePromptAudit(audit); createErr == nil {
			result.AuditID = audit.ID
		} else {
			logger.LogWarn(c, "probe block audit persistence failed")
		}
		AttachPromptAuditResult(c, result)
		return result, hosttypes.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgProbeRequestBlocked)), hosttypes.ErrorCodeProbeRequestBlocked, http.StatusBadRequest, hosttypes.ErrOptionWithSkipRetry(), hosttypes.ErrOptionWithNoRecordErrorLog())
	}
	match, err := matchPromptWordlists(wordlistSnapshot, configured)

	// A request that generates nothing (a token count) is held to the wordlist
	// gate alone: that gate is local and keeps listed text from reaching the
	// upstream account, while the model audit would spend the audit node's own
	// capacity on every counting step and turn its limits into refused counts.
	// The text is audited in full by the request that generates from it. Only a
	// block match stops a count; nothing is recorded for one that passes.
	if request.WordlistOnly && err == nil && (match == nil || match.Action != prompt_audit_setting.WordlistActionBlock) {
		return PromptAuditResult{Enabled: auditEnabled, Mode: configured.Mode, Direction: direction, ConfigVersion: configured.ConfigVersion, Outcome: "skipped_wordlist_only"}, nil
	}

	// Only the model's own mode and scope switch determine its conversation window.
	inspectionSnapshot := request.Snapshot
	if promptInspectionUsesBlockingSnapshot(direction, configured) {
		inspectionSnapshot = inspectionSnapshot.BlockingSnapshot()
	}

	if err == nil && (match == nil || match.Action == prompt_audit_setting.WordlistActionReview) {
		request.Wordlist = match
		inspectionRequest := request
		inspectionRequest.Snapshot = inspectionSnapshot
		if strings.TrimSpace(inspectionRequest.RawFullText) == "" {
			inspectionRequest.RawFullText = request.Snapshot.Text()
		}
		result, apiErr := checkPromptAuditWithSetting(c, inspectionRequest, configured)
		return result, apiErr
	}
	text := request.Snapshot.Text()
	digest := sha256.Sum256([]byte(text))
	result := PromptAuditResult{
		Enabled: true, Reviewed: err == nil, Blocked: true, Mode: prompt_audit_setting.ModeBlocking,
		Direction: PromptAuditDirectionInput, CoverageComplete: true,
		Decision: PromptAuditDecisionBlock, Outcome: PromptAuditDecisionBlock, ConfigVersion: configured.ConfigVersion,
		ActualAction: "block",
		InputChars:   utf8.RuneCountInString(text), InputSHA256: hex.EncodeToString(digest[:]), SegmentCount: len(request.Snapshot.Segments),
		InspectionType: "wordlist", Wordlist: match,
	}
	code, status, message := hosttypes.ErrorCodeSensitiveWordsDetected, http.StatusBadRequest, "sensitive words detected"
	auditStatus := model.PromptAuditStatusDone
	if err != nil {
		result.Decision, result.Outcome, result.FailureKind = PromptAuditDecisionUnavailable, PromptAuditDecisionUnavailable, "wordlist_unavailable"
		result.ActualAction = PromptAuditActionUnavailable
		auditStatus = model.PromptAuditStatusFailed
		code, status, message = hosttypes.ErrorCodePromptAuditUnavailable, http.StatusServiceUnavailable, "prompt inspection is unavailable"
	}
	audit := &model.PromptAudit{
		RequestID: resultRequestID(c), UserID: contextInt(c, "id"), TokenID: contextInt(c, "token_id"), TokenName: contextString(c, "token_name"),
		GroupName: effectivePromptAuditGroup(c), Protocol: request.Protocol, ModelName: request.Model, Stage: normalizedPromptAuditStage(request.Stage),
		Direction: PromptAuditDirectionInput, CoverageComplete: true,
		ConfigVersion: configured.ConfigVersion, ExecutionMode: result.Mode, Status: auditStatus,
		PromptHash: result.InputSHA256, PromptLength: result.InputChars, SegmentCount: result.SegmentCount,
		Decision: result.Decision, WouldAction: promptAuditActionForDecision(result.Decision), InspectionType: "wordlist", ErrorCode: result.FailureKind,
		Action:      result.ActualAction,
		CompletedAt: common.GetTimestamp(),
	}
	if match != nil {
		audit.WordlistID, audit.WordlistName, audit.WordlistVersion, audit.MatchedScope = match.ID, match.Name, match.Version, string(match.Scope)
	}
	applyPromptAuditRequestContext(audit, c)
	setPromptAuditContent(audit, text, configured.FullPromptRetentionLimit())
	setPromptAuditInputContext(audit, request)
	var inspected []dto.PromptAuditSegment
	for _, segment := range wordlistSnapshot.OrderedSegments() {
		if len(configured.PolicyFor(segment.SourceScope()).LibraryIDs) > 0 {
			inspected = append(inspected, segment)
		}
	}
	payload, _ := common.Marshal(promptAuditPayload{Version: 1, Direction: direction, CoverageComplete: true, Segments: inspected})
	audit.ScanPayload, audit.ScanPayloadTruncated = model.RetainPromptAuditPayload(payload)
	if err := model.CreatePromptAudit(audit); err != nil {
		logger.LogWarn(c, "wordlist audit persistence failed")
	} else {
		result.AuditID = audit.ID
	}
	AttachPromptAuditResult(c, result)
	logger.LogWarn(c, "user sensitive words detected")
	// Both outcomes are persisted to the prompt audit log (block or
	// wordlist_unavailable); neither must pollute the user-visible error log.
	options := []hosttypes.NewAPIErrorOptions{
		hosttypes.ErrOptionWithSkipRetry(),
		hosttypes.ErrOptionWithNoRecordErrorLog(),
	}
	return result, hosttypes.NewErrorWithStatusCode(errors.New(message), code, status, options...)
}

func StartPromptWordlistRunner() {
	promptWordlistRunnerOnce.Do(func() {
		if err := RefreshPromptWordlists(); err != nil {
			common.SysError("initial wordlist load failed: " + err.Error())
		}
		if err := compilePromptWordlists(); err != nil {
			common.SysError("initial wordlist compilation failed: " + err.Error())
		}
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if err := model.SyncPromptInspectionOptions(); err != nil {
					logger.LogWarn(context.Background(), "prompt inspection settings sync failed: "+err.Error())
				}
				if err := RefreshPromptWordlists(); err != nil {
					logger.LogWarn(context.Background(), "wordlist snapshot refresh failed: "+err.Error())
				}
			}
		}()
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if err := compilePromptWordlists(); err != nil {
					logger.LogWarn(context.Background(), "wordlist compilation failed: "+err.Error())
				}
			}
		}()
		go runPromptWordlistImports()
	})
}
