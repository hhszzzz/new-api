package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

type promptAuditSummaryCheckpoint struct {
	Key     string
	Pending bool
}

type promptAuditContinuationLink struct {
	Key        string
	Unresolved bool
}

type promptAuditGroupEntry struct {
	Key              string
	Epoch            string
	ExpiresAt        time.Time
	Summaries        map[string]promptAuditSummaryCheckpoint
	Continuations    map[string]promptAuditContinuationLink
	InferenceBlocked bool
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
// client authorship. Lost or conflicting evidence must not become "the latest".
func (entry *promptAuditGroupEntry) resolveContinuation(identity string, snapshot dto.PromptAuditSnapshot, allowInference bool) promptAuditContinuationLink {
	fingerprint := snapshot.GroupOrigin.Continuation
	if link, ok := entry.Continuations[fingerprint]; ok {
		return link
	}
	key := ""
	if len(entry.Continuations) >= 32 || fingerprint == "" {
		// Without space for an immutable alias, even explicit ancestry cannot be
		// remembered consistently across later requests carrying only the summary.
		entry.InferenceBlocked = true
	} else {
		key = entry.originKey(identity, snapshot.GroupPredecessor)
	}
	if key == "" && allowInference && !entry.InferenceBlocked {
		pending := ""
		for fingerprint, checkpoint := range entry.Summaries {
			if !checkpoint.Pending {
				continue
			}
			if pending != "" {
				entry.InferenceBlocked = true
				break
			}
			pending = fingerprint
		}
		if pending != "" && !entry.InferenceBlocked {
			checkpoint := entry.Summaries[pending]
			key = checkpoint.Key
			checkpoint.Pending = false
			entry.Summaries[pending] = checkpoint
		}
	}
	if key != "" {
		for fingerprint, checkpoint := range entry.Summaries {
			if checkpoint.Key == key {
				checkpoint.Pending = false
				entry.Summaries[fingerprint] = checkpoint
			}
		}
	}
	link := promptAuditContinuationLink{Key: key, Unresolved: key == ""}
	if link.Unresolved {
		digest := sha256.Sum256([]byte(identity + "|unresolved|" + entry.Epoch + "|" + fingerprint))
		link.Key = hex.EncodeToString(digest[:])
	}
	if len(entry.Continuations) < 32 && fingerprint != "" {
		entry.Continuations[fingerprint] = link
	} else {
		// Do not evict old aliases and later bind their text to a newer question.
		entry.InferenceBlocked = true
	}
	return link
}

// Grouping is only presentation metadata. It never determines which content is
// inspected or permits reusing a verdict for a different payload.
func preparePromptAuditRequest(c *gin.Context, request PromptAuditRequest) PromptAuditRequest {
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
			// Auxiliary traffic can retain exact ancestry, including an unresolved
			// group, but cannot consume a pending summary by recency.
			link := entry.resolveContinuation(identity, request.Snapshot, false)
			originGroup = link.Key
			if !link.Unresolved {
				originKey = link.Key
			}
		}
		switch {
		case request.RequestKind == "continuation":
			link := entry.resolveContinuation(identity, request.Snapshot, true)
			request.GroupKey = link.Key
			if link.Unresolved {
				request.RequestKind = "continuation:unresolved"
			}
		case request.HumanPrompt != "":
			predecessor := entry.originKey(identity, request.Snapshot.GroupPredecessor)
			if request.RequestKind == "prompt" || entry.Key == "" || predecessor == entry.Key {
				if entry.Key != request.GroupKey {
					// A first-seen continuation after a new question is ambiguous.
					// Already bound aliases stay pinned to their original question.
					for key, checkpoint := range entry.Summaries {
						if checkpoint.Pending {
							entry.InferenceBlocked = true
						}
						checkpoint.Pending = false
						entry.Summaries[key] = checkpoint
					}
					entry.Key = request.GroupKey
				}
			}
		default:
			request.GroupKey = originGroup
			if request.GroupKey == "" {
				request.GroupKey = entry.Key
			}
		}
		if request.RequestKind == "side:summary" && request.Snapshot.SummaryKey != "" {
			if _, known := entry.Summaries[request.Snapshot.SummaryKey]; !known {
				if len(entry.Summaries) < 32 {
					// An auxiliary's active-question fallback is not ancestry evidence.
					pending := originKey != "" && (entry.Key == "" || originKey == entry.Key)
					if originKey != "" && !pending {
						entry.InferenceBlocked = true
					}
					entry.Summaries[request.Snapshot.SummaryKey] = promptAuditSummaryCheckpoint{Key: originKey, Pending: pending}
				} else {
					entry.InferenceBlocked = true
				}
			}
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
	if request.HumanPrompt != "" {
		value := redactPromptAuditText(request.HumanPrompt)
		runes := []rune(strings.TrimSpace(value))
		if len(runes) > promptAuditPreviewSourceRunes {
			value = string(runes[:promptAuditPreviewSourceRunes]) + "…"
		}
		audit.RedactedPreview = value
	}
}
