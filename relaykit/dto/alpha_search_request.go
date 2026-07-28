package dto

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/tidwall/gjson"
)

// AlphaSearchRequest is the Codex standalone web search request.
// RawBody preserves the original JSON so unknown fields are forwarded intact.
type AlphaSearchRequest struct {
	Model   string          `json:"model"`
	Id      string          `json:"id,omitempty"`
	Stream  *bool           `json:"stream,omitempty"`
	RawBody json.RawMessage `json:"-"`
}

func (r *AlphaSearchRequest) GetTokenCountMeta() *types.TokenCountMeta {
	combineText := ""
	if len(r.RawBody) > 0 {
		combineText = string(r.RawBody)
	}
	return &types.TokenCountMeta{
		CombineText: combineText,
		TokenType:   types.TokenTypeTokenizer,
	}
}

func (r *AlphaSearchRequest) GetSensitiveText() string {
	if r == nil || len(r.RawBody) == 0 {
		return ""
	}

	input := gjson.GetBytes(r.RawBody, "input")
	searchQueries := gjson.GetBytes(r.RawBody, "commands.search_query").Array()
	responsesRequest := OpenAIResponsesRequest{Input: json.RawMessage(input.Raw)}
	texts := make([]string, 0, len(searchQueries)+1)
	if input := responsesRequest.GetSensitiveText(); input != "" {
		texts = append(texts, input)
	}
	for _, searchQuery := range searchQueries {
		if query := searchQuery.Get("q").String(); query != "" {
			texts = append(texts, query)
		}
	}
	return strings.Join(texts, "\n")
}

func (r *AlphaSearchRequest) IsStream(_ *http.Request) bool {
	return false
}

func (r *AlphaSearchRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
