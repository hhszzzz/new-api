package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

type promptAuditGroupEntry struct {
	Key       string
	ExpiresAt time.Time
}

var promptAuditGroupMap = struct {
	sync.Mutex
	entries map[string]promptAuditGroupEntry
}{entries: make(map[string]promptAuditGroupEntry)}

// Grouping is only presentation metadata. It never determines which content is
// inspected or permits reusing a verdict for a different payload.
func preparePromptAuditRequest(c *gin.Context, request PromptAuditRequest) PromptAuditRequest {
	if request.GroupKey != "" {
		return request
	}
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
	// Output checks use the input's identity when available, without advancing
	// the session's current question a second time.
	if request.Direction == PromptAuditDirectionOutput && c != nil {
		request.GroupKey = c.GetString("prompt_audit_group_key")
		if request.GroupKey != "" {
			return request
		}
	}
	identity := strconv.Itoa(contextInt(c, "id")) + "|" + request.SessionKey
	if request.HumanPrompt != "" {
		// The fixed-width session hash separates client text from the numeric user
		// identifier. Preserve internal spaces in the actual question.
		digest := sha256.Sum256([]byte(identity + "|prompt|" + strings.TrimSpace(request.HumanPrompt)))
		request.GroupKey = hex.EncodeToString(digest[:])
	}
	if request.SessionKey != "" && request.Direction != PromptAuditDirectionOutput {
		now := time.Now()
		promptAuditGroupMap.Lock()
		if request.GroupKey == "" {
			if entry, ok := promptAuditGroupMap.entries[identity]; ok && now.Before(entry.ExpiresAt) {
				request.GroupKey = entry.Key
			}
		} else if entry, exists := promptAuditGroupMap.entries[identity]; request.RequestKind == "prompt" || !exists || !now.Before(entry.ExpiresAt) {
			if len(promptAuditGroupMap.entries) >= 4096 {
				for key, entry := range promptAuditGroupMap.entries {
					if !now.Before(entry.ExpiresAt) {
						delete(promptAuditGroupMap.entries, key)
					}
				}
				if len(promptAuditGroupMap.entries) >= 4096 {
					var oldest string
					var expires time.Time
					for key, entry := range promptAuditGroupMap.entries {
						if oldest == "" || entry.ExpiresAt.Before(expires) {
							oldest, expires = key, entry.ExpiresAt
						}
					}
					delete(promptAuditGroupMap.entries, oldest)
				}
			}
			promptAuditGroupMap.entries[identity] = promptAuditGroupEntry{Key: request.GroupKey, ExpiresAt: now.Add(30 * time.Minute)}
		}
		promptAuditGroupMap.Unlock()
	}
	if request.GroupKey == "" && request.SessionKey != "" && (strings.HasPrefix(request.RequestKind, "side:") || request.RequestKind == "subagent") {
		digest := sha256.Sum256([]byte(identity + "|" + request.RequestKind))
		request.GroupKey = hex.EncodeToString(digest[:])
	}
	if c != nil && request.Direction != PromptAuditDirectionOutput {
		c.Set("prompt_audit_group_key", request.GroupKey)
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
