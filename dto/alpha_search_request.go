package dto

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
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

	var body struct {
		Input    json.RawMessage `json:"input"`
		Commands struct {
			SearchQueries []struct {
				Query string `json:"q"`
			} `json:"search_query"`
		} `json:"commands"`
	}
	if err := common.Unmarshal(r.RawBody, &body); err != nil {
		return ""
	}

	responsesRequest := OpenAIResponsesRequest{Input: body.Input}
	texts := make([]string, 0, len(body.Commands.SearchQueries)+1)
	if input := responsesRequest.GetSensitiveText(); input != "" {
		texts = append(texts, input)
	}
	for _, searchQuery := range body.Commands.SearchQueries {
		if searchQuery.Query != "" {
			texts = append(texts, searchQuery.Query)
		}
	}
	return strings.Join(texts, "\n")
}

func (r *AlphaSearchRequest) IsStream(c *gin.Context) bool {
	return false
}

func (r *AlphaSearchRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
