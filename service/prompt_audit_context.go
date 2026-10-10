package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"
	"github.com/gin-gonic/gin"
)

// Bounds on the per-session grouping state. A session entry is refreshed on
// every request, so a long-lived session needs its own caps; reaching one must
// stay local to the request that sees it instead of disabling a session.
const (
	promptAuditSummaryCheckpointCap = 128
	promptAuditContinuationAliasCap = 128
	// A pending summary predicts the continuation that immediately follows it.
	// An older one is left behind rather than matched by recency.
	promptAuditPendingSummaryTTL = 10 * time.Minute
)

type promptAuditSummaryCheckpoint struct {
	Key      string
	Pending  bool
	Deadline time.Time
	StoredAt time.Time
}

type promptAuditContinuationLink struct {
	Key        string
	Unresolved bool
	TouchedAt  time.Time
}

type promptAuditGroupEntry struct {
	Key           string
	Epoch         string
	ExpiresAt     time.Time
	Summaries     map[string]promptAuditSummaryCheckpoint
	Continuations map[string]promptAuditContinuationLink
}

var promptAuditGroupMap = struct {
	sync.Mutex
	entries map[string]promptAuditGroupEntry
}{entries: make(map[string]promptAuditGroupEntry)}

func promptAuditQuestionKey(identity, prompt string) string {
	digest := sha256.Sum256([]byte(identity + "|prompt|" + strings.TrimSpace(prompt)))
	return hex.EncodeToString(digest[:])
}

func (entry *promptAuditGroupEntry) originKey(identity string, origin dto.PromptAuditOrigin) string {
	if origin.Prompt != "" {
		return promptAuditQuestionKey(identity, origin.Prompt)
	}
	if link, ok := entry.Continuations[origin.Continuation]; ok && !link.Unresolved {
		return link.Key
	}
	return ""
}

// resolveContinuation uses an exact alias or supplied ancestry first. A single
// observed summary is only a bounded single-flight inference, never proof of
// client authorship, so it is read only while it is the one unexpired pending
// summary; two pending summaries for different questions stay unlinked.
func (entry *promptAuditGroupEntry) resolveContinuation(identity string, snapshot dto.PromptAuditSnapshot, allowInference bool, now time.Time) promptAuditContinuationLink {
	fingerprint := snapshot.GroupOrigin.Continuation
	if fingerprint != "" {
		if link, ok := entry.Continuations[fingerprint]; ok {
			link.TouchedAt = now
			entry.Continuations[fingerprint] = link
			return link
		}
	}
	key := entry.originKey(identity, snapshot.GroupPredecessor)
	if key == "" && allowInference {
		key = entry.consumePendingSummary(now)
	}
	link := promptAuditContinuationLink{Key: key, Unresolved: key == "", TouchedAt: now}
	if link.Unresolved {
		digest := sha256.Sum256([]byte(identity + "|unresolved|" + entry.Epoch + "|" + fingerprint))
		link.Key = hex.EncodeToString(digest[:])
	}
	entry.storeContinuation(fingerprint, link)
	return link
}

// consumePendingSummary returns the question of the one unexpired pending
// summary and retires it. Pending summaries for several questions at once are
// not evidence: picking one would be guessing, so the continuation stays
// unlinked and the ambiguous summaries simply age out.
func (entry *promptAuditGroupEntry) consumePendingSummary(now time.Time) string {
	target, ambiguous := "", false
	for fingerprint, checkpoint := range entry.Summaries {
		if !checkpoint.Pending || checkpoint.Key == "" {
			continue
		}
		if !now.Before(checkpoint.Deadline) {
			checkpoint.Pending = false
			entry.Summaries[fingerprint] = checkpoint
			continue
		}
		if target != "" && checkpoint.Key != target {
			ambiguous = true
			continue
		}
		target = checkpoint.Key
	}
	if ambiguous || target == "" {
		return ""
	}
	for fingerprint, checkpoint := range entry.Summaries {
		if checkpoint.Pending && checkpoint.Key == target {
			checkpoint.Pending = false
			entry.Summaries[fingerprint] = checkpoint
		}
	}
	return target
}

// storeContinuation keeps at most one binding per observed continuation text and
// drops the least recently seen one at the cap. A dropped text is resolved again
// if it returns, which bounds this map without rebinding the texts still in use.
func (entry *promptAuditGroupEntry) storeContinuation(fingerprint string, link promptAuditContinuationLink) {
	if fingerprint == "" {
		return
	}
	if _, exists := entry.Continuations[fingerprint]; exists {
		entry.Continuations[fingerprint] = link
		return
	}
	if len(entry.Continuations) >= promptAuditContinuationAliasCap {
		oldest := ""
		var touched time.Time
		for key, value := range entry.Continuations {
			if oldest == "" || value.TouchedAt.Before(touched) {
				oldest, touched = key, value.TouchedAt
			}
		}
		delete(entry.Continuations, oldest)
	}
	entry.Continuations[fingerprint] = link
}

// recordSummary remembers the question a summary was made from so the
// continuation it produces can be linked back to it. That question is not
// necessarily the active one: a session may work on several questions at once,
// and a new question does not invalidate the older conversation summarized.
func (entry *promptAuditGroupEntry) recordSummary(fingerprint, key string, now time.Time) {
	if fingerprint == "" || key == "" {
		return
	}
	if _, known := entry.Summaries[fingerprint]; known {
		return
	}
	if len(entry.Summaries) >= promptAuditSummaryCheckpointCap {
		oldest := ""
		var stored time.Time
		for candidate, checkpoint := range entry.Summaries {
			if oldest == "" || checkpoint.StoredAt.Before(stored) {
				oldest, stored = candidate, checkpoint.StoredAt
			}
		}
		delete(entry.Summaries, oldest)
	}
	entry.Summaries[fingerprint] = promptAuditSummaryCheckpoint{
		Key: key, Pending: true, Deadline: now.Add(promptAuditPendingSummaryTTL), StoredAt: now,
	}
}

// Grouping is only presentation metadata. It never determines which content is
// inspected or permits reusing a verdict for a different payload.
func preparePromptAuditRequest(c *gin.Context, request PromptAuditRequest) PromptAuditRequest {
	if request.OriginalSnapshot == nil {
		snapshot := request.Snapshot
		snapshot.Segments = append([]dto.PromptAuditSegment(nil), snapshot.Segments...)
		request.OriginalSnapshot, request.OriginalOutput = &snapshot, request.Output
	}
	if request.groupPrepared || request.GroupKey != "" {
		return request
	}
	request.groupPrepared = true
	request.SessionKey = request.Snapshot.SessionKey
	request.RequestKind = request.Snapshot.RequestKind
	request.HumanPrompt = request.Snapshot.HumanPrompt
	if request.RequestKind == "" {
		request.RequestKind = "prompt"
		request.HumanPrompt = dto.HumanPrompt(request.Snapshot)
	}
	if c != nil && c.Request != nil {
		if request.SessionKey == "" {
			if session := strings.TrimSpace(c.GetHeader("session_id")); session != "" {
				digest := sha256.Sum256([]byte(session))
				request.SessionKey = hex.EncodeToString(digest[:])
			}
		}
		if c.GetHeader("x-openai-subagent") != "" {
			request.RequestKind, request.HumanPrompt = "subagent", ""
		} else if c.GetHeader("x-openai-memgen-request") != "" {
			request.RequestKind, request.HumanPrompt = "side:memory", ""
		}
	}
	// Presence matters: unresolved input must not be reclassified by output.
	if request.Direction == PromptAuditDirectionOutput && c != nil {
		if value, exists := c.Get("prompt_audit_input_identity"); exists {
			input := value.(PromptAuditRequest)
			request.GroupKey, request.SessionKey = input.GroupKey, input.SessionKey
			request.RequestKind, request.HumanPrompt = input.RequestKind, input.HumanPrompt
			return request
		}
		if key := c.GetString("prompt_audit_group_key"); key != "" {
			request.GroupKey = key
			return request
		}
	}
	identity := strconv.Itoa(contextInt(c, "id")) + "|" + request.SessionKey
	if request.HumanPrompt != "" {
		request.GroupKey = promptAuditQuestionKey(identity, request.HumanPrompt)
	}
	if request.SessionKey != "" && request.Direction != PromptAuditDirectionOutput {
		now := time.Now()
		promptAuditGroupMap.Lock()
		entry, exists := promptAuditGroupMap.entries[identity]
		if !exists || !now.Before(entry.ExpiresAt) {
			if len(promptAuditGroupMap.entries) >= 4096 {
				for key, value := range promptAuditGroupMap.entries {
					if !now.Before(value.ExpiresAt) {
						delete(promptAuditGroupMap.entries, key)
					}
				}
				if len(promptAuditGroupMap.entries) >= 4096 {
					var oldest string
					var expires time.Time
					for key, value := range promptAuditGroupMap.entries {
						if oldest == "" || value.ExpiresAt.Before(expires) {
							oldest, expires = key, value.ExpiresAt
						}
					}
					delete(promptAuditGroupMap.entries, oldest)
				}
			}
			entry = promptAuditGroupEntry{Epoch: common.GetUUID(), Summaries: make(map[string]promptAuditSummaryCheckpoint), Continuations: make(map[string]promptAuditContinuationLink)}
		}
		origin := request.Snapshot.GroupOrigin
		if request.RequestKind == "subagent" || request.RequestKind == "side:memory" {
			origin = dto.PromptAuditOrigin{}
		}
		originKey := entry.originKey(identity, origin)
		originGroup := originKey
		if origin.Continuation != "" && request.RequestKind != "continuation" {
			// Auxiliary traffic keeps an exact continuation ancestry, including an
			// unresolved one, but neither consumes a pending summary nor creates a
			// binding the continuation request itself would have resolved.
			if link, ok := entry.Continuations[origin.Continuation]; ok {
				originGroup = link.Key
				if !link.Unresolved {
					originKey = link.Key
				}
			}
		}
		switch {
		case request.RequestKind == "continuation":
			link := entry.resolveContinuation(identity, request.Snapshot, true, now)
			request.GroupKey = link.Key
			if link.Unresolved {
				request.RequestKind = "continuation:unresolved"
			}
		case request.HumanPrompt != "":
			predecessor := entry.originKey(identity, request.Snapshot.GroupPredecessor)
			if request.RequestKind == "prompt" || entry.Key == "" || predecessor == entry.Key {
				entry.Key = request.GroupKey
			}
		default:
			request.GroupKey = originGroup
			if request.GroupKey == "" {
				request.GroupKey = entry.Key
			}
		}
		if request.RequestKind == "side:summary" && request.Snapshot.SummaryKey != "" {
			// The summary belongs to the conversation it was made from, which the
			// transcript still names. With no ancestry at all that is the active
			// question; an unknown continuation names a different one and must not
			// be answered with the active question instead.
			target := originKey
			if target == "" && request.Snapshot.GroupOrigin.Continuation == "" {
				target = entry.Key
			}
			entry.recordSummary(request.Snapshot.SummaryKey, target, now)
		}
		entry.ExpiresAt = now.Add(30 * time.Minute)
		promptAuditGroupMap.entries[identity] = entry
		promptAuditGroupMap.Unlock()
	}
	if request.RequestKind == "continuation" && request.GroupKey == "" {
		request.RequestKind = "continuation:unresolved"
		request.GroupKey = common.GetUUID()
	}
	if request.GroupKey == "" && request.SessionKey != "" && (strings.HasPrefix(request.RequestKind, "side:") || request.RequestKind == "subagent") {
		digest := sha256.Sum256([]byte(identity + "|" + request.RequestKind))
		request.GroupKey = hex.EncodeToString(digest[:])
	}
	if c != nil && request.Direction != PromptAuditDirectionOutput {
		c.Set("prompt_audit_group_key", request.GroupKey)
		c.Set("prompt_audit_input_identity", PromptAuditRequest{GroupKey: request.GroupKey, SessionKey: request.SessionKey, RequestKind: request.RequestKind, HumanPrompt: request.HumanPrompt})
	}
	return request
}

func setPromptAuditInputContext(audit *model.PromptAudit, request PromptAuditRequest) {
	audit.GroupKey, audit.SessionKey, audit.RequestKind = request.GroupKey, request.SessionKey, request.RequestKind
	snapshot, output := request.Snapshot, request.Output
	if request.OriginalSnapshot != nil {
		snapshot, output = *request.OriginalSnapshot, request.OriginalOutput
	}
	if prompt_audit_setting.GetSetting().SharedContentEnabled {
		audit.RawPayload, _ = common.Marshal(promptAuditPayload{Version: 1, Direction: audit.Direction, CoverageComplete: audit.CoverageComplete, Segments: snapshot.OrderedSegments(), Output: output})
		if audit.Direction != PromptAuditDirectionOutput {
			originalText := promptAuditJoinedText(dto.PromptAuditSnapshot{Segments: snapshot.OrderedSegments()}, output)
			setPromptAuditContent(audit, originalText, 0)
			audit.PromptLength = utf8.RuneCountInString(originalText)
		}
	}
	if audit.Direction == PromptAuditDirectionOutput {
		setPromptAuditContent(audit, output, prompt_audit_setting.GetSetting().FullPromptRetentionLimit())
		audit.PromptLength = utf8.RuneCountInString(output)
		return
	}
	if request.HumanPrompt != "" {
		value := redactPromptAuditText(request.HumanPrompt)
		runes := []rune(strings.TrimSpace(value))
		if len(runes) > promptAuditPreviewSourceRunes {
			value = string(runes[:promptAuditPreviewSourceRunes]) + "…"
		}
		audit.RedactedPreview = value
	}
}
