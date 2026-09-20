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
		entry := promptWordlistRuntime{ID: id, Name: row.Name, TargetVersion: row.ContentHash, Generation: row.Generation, Enabled: row.Enabled && row.ContentHash != ""}
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
}

func matchPromptWordlists(snapshot dto.PromptAuditSnapshot, configured prompt_audit_setting.PromptAuditSetting) (*PromptWordlistMatch, error) {
	if !setting.ShouldCheckPromptSensitive() {
		return nil, nil
	}
	libraries := promptWordlists.Load()
	for _, segment := range snapshot.PrioritizedSegments() {
		scope := segment.SourceScope()
		for _, id := range configured.PolicyFor(scope).LibraryIDs {
			var entry promptWordlistRuntime
			if id == prompt_audit_setting.ManualWordlistID {
				if !configured.ManualWordlistActive() {
					continue
				}
				entry = promptWordlistRuntime{ID: id, Name: "Custom wordlist", Version: strconv.FormatUint(setting.SensitiveWordsVersion(), 10), Enabled: true, Matcher: currentManualWordMatcher()}
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
				return &PromptWordlistMatch{ID: id, Name: entry.Name, Version: entry.Version, Scope: scope}, nil
			}
		}
	}
	return nil, nil
}

// TestPromptWordlists uses exactly the active enforcement rules. It does not
// persist supplied text, bill, or call an external model.
func TestPromptWordlists(scope dto.PromptAuditScope, text string) (*PromptWordlistMatch, bool, error) {
	configured := prompt_audit_setting.GetSetting()
	match, err := matchPromptWordlists(dto.PromptAuditSnapshot{Segments: []dto.PromptAuditSegment{{Scope: scope, Role: string(scope), Text: text}}}, configured)
	return match, configured.Mode != prompt_audit_setting.ModeOff && configured.PolicyFor(scope).ModelAudit, err
}

func InspectPrompt(c *gin.Context, request PromptAuditRequest) (PromptAuditResult, *hosttypes.NewAPIError) {
	configured := prompt_audit_setting.GetSetting()
	match, err := matchPromptWordlists(request.Snapshot, configured)
	if match == nil && err == nil {
		return checkPromptAuditWithSetting(c, request, configured)
	}
	text := request.Snapshot.Text()
	digest := sha256.Sum256([]byte(text))
	result := PromptAuditResult{
		Enabled: true, Reviewed: err == nil, Blocked: true, Mode: prompt_audit_setting.ModeBlocking,
		Decision: PromptAuditDecisionBlock, Outcome: PromptAuditDecisionBlock, ConfigVersion: configured.ConfigVersion,
		InputChars: utf8.RuneCountInString(text), InputSHA256: hex.EncodeToString(digest[:]), SegmentCount: len(request.Snapshot.Segments),
		InspectionType: "wordlist", Wordlist: match,
	}
	code, status, message := hosttypes.ErrorCodeSensitiveWordsDetected, http.StatusBadRequest, "sensitive words detected"
	if err != nil {
		result.Decision, result.Outcome, result.FailureKind = PromptAuditDecisionUnavailable, PromptAuditDecisionUnavailable, "wordlist_unavailable"
		code, status, message = hosttypes.ErrorCodePromptAuditUnavailable, http.StatusServiceUnavailable, "prompt inspection is unavailable"
	}
	audit := &model.PromptAudit{
		RequestID: resultRequestID(c), UserID: contextInt(c, "id"), TokenID: contextInt(c, "token_id"), TokenName: contextString(c, "token_name"),
		GroupName: effectivePromptAuditGroup(c), Protocol: request.Protocol, ModelName: request.Model, Stage: normalizedPromptAuditStage(request.Stage),
		ConfigVersion: configured.ConfigVersion, ExecutionMode: result.Mode, Status: model.PromptAuditStatusDone,
		PromptHash: result.InputSHA256, PromptLength: result.InputChars, SegmentCount: result.SegmentCount,
		Decision: result.Decision, WouldAction: result.Decision, InspectionType: "wordlist", ErrorCode: result.FailureKind,
		CompletedAt: common.GetTimestamp(),
	}
	if match != nil {
		audit.WordlistID, audit.WordlistName, audit.WordlistVersion, audit.MatchedScope = match.ID, match.Name, match.Version, string(match.Scope)
	}
	setPromptAuditContent(audit, text)
	if err := model.CreatePromptAudit(audit); err != nil {
		logger.LogWarn(c, "wordlist audit persistence failed")
	} else {
		result.AuditID = audit.ID
	}
	AttachPromptAuditResult(c, result)
	logger.LogWarn(c, "user sensitive words detected")
	return result, hosttypes.NewErrorWithStatusCode(errors.New(message), code, status, hosttypes.ErrOptionWithSkipRetry())
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
					logger.LogWarn(context.Background(), "prompt inspection settings sync failed")
				}
				if err := RefreshPromptWordlists(); err != nil {
					logger.LogWarn(context.Background(), "wordlist snapshot refresh failed")
				}
			}
		}()
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if err := compilePromptWordlists(); err != nil {
					logger.LogWarn(context.Background(), "wordlist compilation failed")
				}
			}
		}()
		go runPromptWordlistImports()
	})
}
