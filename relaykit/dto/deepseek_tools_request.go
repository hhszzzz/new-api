package dto

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// DeepSeekToolsRequest is a DeepSeek Search or Fetch API request
// (/v1/tools/search, /v1/tools/fetch). The body names no model; Model holds
// the routing name. RawBody preserves the original JSON so the body is
// forwarded with unknown fields intact.
type DeepSeekToolsRequest struct {
	Model   string          `json:"model,omitempty"`
	RawBody json.RawMessage `json:"-"`
}

func (r *DeepSeekToolsRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return &types.TokenCountMeta{
		CombineText: string(r.RawBody),
		TokenType:   types.TokenTypeTokenizer,
	}
}

func (r *DeepSeekToolsRequest) GetSensitiveText() string {
	if r == nil {
		return ""
	}
	return string(r.RawBody)
}

func (r *DeepSeekToolsRequest) GetPromptAuditSnapshot() PromptAuditSnapshot {
	if r == nil || len(r.RawBody) == 0 {
		return PromptAuditSnapshot{}
	}
	var body struct {
		Query         string `json:"query"`
		OriginalQuery string `json:"ori_question"`
		URL           string `json:"url"`
	}
	if err := kitutil.Unmarshal(r.RawBody, &body); err != nil {
		return PromptAuditSnapshot{}
	}

	query := strings.TrimSpace(body.Query)
	originalQuery := strings.TrimSpace(body.OriginalQuery)
	url := strings.TrimSpace(body.URL)
	segments := make([]PromptAuditSegment, 0, 3)
	if originalQuery != "" {
		segments = appendRoleMessage(segments, "user", true, []string{originalQuery})
	} else if query != "" {
		segments = appendRoleMessage(segments, "user", true, []string{query})
	}
	if query != "" && originalQuery != "" {
		segments = append(segments, promptAuditToolSegments(PromptScopeToolCall, "assistant", "web_search", "", []string{query})...)
	}
	if url != "" {
		segments = append(segments, promptAuditToolSegments(PromptScopeToolCall, "assistant", "web_fetch", "", []string{url})...)
	}

	snapshot := PromptAuditSnapshot{Segments: segments}
	if originalQuery != "" {
		snapshot.RequestKind = "side:web_search"
		snapshot.HumanPrompt = originalQuery
	} else if query != "" {
		snapshot.HumanPrompt = query
	} else if url != "" {
		snapshot.RequestKind = "side:web_fetch"
	}
	return snapshot
}

func (r *DeepSeekToolsRequest) IsStream(_ *http.Request) bool {
	return false
}

func (r *DeepSeekToolsRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
