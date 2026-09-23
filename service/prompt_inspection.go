package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
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

// promptInspectionUsesBlockingSnapshot decides whether synchronous input
// inspection is scoped to the latest user turn.
//
// The wordlist gate and the model audit narrow together. Wordlists used to
// narrow unconditionally, which made the switch read as broken: turning it off
// widened only the model, so a wordlist hit from an older turn still arrived.
//
// Output direction is never narrowed — the whole generated answer is the payload
// under inspection, and there is no "latest turn" to narrow to.
func promptInspectionUsesBlockingSnapshot(direction string, configured prompt_audit_setting.PromptAuditSetting) bool {
	if direction != PromptAuditDirectionInput {
		return false
	}
	return configured.Mode == prompt_audit_setting.ModeBlocking && configured.BlockingLatestTurnOnly
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
	match, err := matchPromptWordlists(dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{{Scope: scope, Role: string(scope), Text: text}}}, configured)
	return match, configured.Mode != prompt_audit_setting.ModeOff && configured.PolicyFor(scope).ModelAudit, err
}

var probePattern = regexp.MustCompile(`^(?i)(hi|hello|ping|pong|test|1|1\+1|2|say 1|你好|测试|测活|探活)[.!?。！？\s]*$`)

// IsProbeRequest checks whether an incoming prompt is a single-turn probe or health-check call.
func IsProbeRequest(snapshot dto.PromptAuditSnapshot) bool {
	segments := snapshot.OrderedSegments()
	if len(segments) != 1 || segments[0].SourceScope() != dto.PromptScopeUser ||
		(!segments[0].User && segments[0].Role != "user") {
		return false
	}
	trimmed := segments[0].Text
	if utf8.RuneCountInString(trimmed) == 0 || utf8.RuneCountInString(trimmed) > 25 {
		return false
	}
	return probePattern.MatchString(trimmed)
}

func InspectPrompt(c *gin.Context, request PromptAuditRequest) (PromptAuditResult, *hosttypes.NewAPIError) {
	configured := prompt_audit_setting.GetSetting()
	group := effectivePromptAuditGroup(c)
	auditConfigured := configured.Mode != prompt_audit_setting.ModeOff && configured.AppliesToGroupForMode(group, configured.Mode)
	auditEnabled := auditConfigured || setting.ShouldCheckPromptSensitive()

	direction := strings.ToLower(strings.TrimSpace(request.Direction))
	if direction == "" {
		direction = PromptAuditDirectionInput
	}

	wordlistSnapshot := request.Snapshot
	if promptInspectionUsesBlockingSnapshot(direction, configured) {
		wordlistSnapshot = wordlistSnapshot.BlockingSnapshot()
	}
	match, err := matchPromptWordlists(wordlistSnapshot, configured)

	// Probe shortcuts apply only to complete, standalone user input after the
	// wordlist gate passes. Explicit rules and missing context cannot be bypassed.
	if direction == PromptAuditDirectionInput && !request.CoverageIncomplete && err == nil && match == nil && IsProbeRequest(request.Snapshot) {
		text := request.Snapshot.Text()
		digest := sha256.Sum256([]byte(text))
		result := PromptAuditResult{
			Enabled:          auditEnabled,
			Reviewed:         true,
			Blocked:          false,
			Mode:             configured.Mode,
			Direction:        direction,
			CoverageComplete: true,
			Decision:         PromptAuditDecisionPass,
			Outcome:          PromptAuditDecisionPass,
			Safety:           "Safe",
			ConfigVersion:    configured.ConfigVersion,
			ActualAction:     PromptAuditActionAllow,
			InputChars:       utf8.RuneCountInString(text),
			InputSHA256:      hex.EncodeToString(digest[:]),
			SegmentCount:     len(request.Snapshot.Segments),
			InspectionType:   "probe_fast_pass",
		}
		// Only an active pipeline records the bypass. A disabled one must not
		// turn every uptime probe into a database insert.
		if auditConfigured {
			audit := &model.PromptAudit{
				RequestID:        resultRequestID(c),
				UserID:           contextInt(c, "id"),
				TokenID:          contextInt(c, "token_id"),
				TokenName:        contextString(c, "token_name"),
				GroupName:        group,
				Protocol:         request.Protocol,
				ModelName:        request.Model,
				Stage:            normalizedPromptAuditStage(request.Stage),
				Direction:        direction,
				CoverageComplete: true,
				ConfigVersion:    configured.ConfigVersion,
				ExecutionMode:    result.Mode,
				Status:           model.PromptAuditStatusDone,
				PromptHash:       result.InputSHA256,
				PromptLength:     result.InputChars,
				SegmentCount:     result.SegmentCount,
				Decision:         PromptAuditDecisionPass,
				Safety:           "Safe",
				WouldAction:      PromptAuditActionAllow,
				InspectionType:   "probe_fast_pass",
				Action:           PromptAuditActionAllow,
				CompletedAt:      common.GetTimestamp(),
			}
			applyPromptAuditRequestContext(audit, c)
			setPromptAuditContent(audit, text)
			if err := model.CreatePromptAudit(audit); err != nil {
				logger.LogWarn(c, "probe fast pass audit persistence failed")
			} else {
				result.AuditID = audit.ID
			}
		}
		AttachPromptAuditResult(c, result)
		return result, nil
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
	setPromptAuditContent(audit, text)
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
