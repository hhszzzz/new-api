package relay

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

// applySystemPromptIfNeeded prepends the configured system prompt layers to an
// OpenAI chat request. The route-injected prompt always leads, so it outranks
// both the channel system prompt and any system prompt the client sent.
func applySystemPromptIfNeeded(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) {
	if info == nil || request == nil {
		return
	}

	systemRole := request.GetSystemRoleName()
	// A Kimi K3 dynamic tool loading message ({"role":"system","tools":[...]})
	// declares tools rather than a system prompt and must never receive content.
	clientPromptIndex := -1
	for i, message := range request.Messages {
		if message.Role == systemRole && len(message.Tools) == 0 {
			clientPromptIndex = i
			break
		}
	}

	leadingPrompt := info.LeadingSystemPrompt(clientPromptIndex >= 0)
	if leadingPrompt == "" {
		return
	}

	if clientPromptIndex < 0 {
		systemMessage := dto.Message{
			Role:    systemRole,
			Content: leadingPrompt,
		}
		request.Messages = append([]dto.Message{systemMessage}, request.Messages...)
		return
	}

	common.SetContextKey(c, constant.ContextKeySystemPromptOverride, true)
	message := request.Messages[clientPromptIndex]
	if message.IsStringContent() {
		request.Messages[clientPromptIndex].SetStringContent(leadingPrompt + "\n" + message.StringContent())
		return
	}
	contents := append([]dto.MediaContent{
		{
			Type: dto.ContentTypeText,
			Text: leadingPrompt,
		},
	}, message.ParseContent()...)
	request.Messages[clientPromptIndex].Content = contents
}

// applyResponsesInstructionsIfNeeded prepends the configured system prompt
// layers to the `instructions` field of an OpenAI Responses request. Most
// adaptors do not compose system prompts for this endpoint, so the handler
// applies them centrally; RelayInfo.LeadingSystemPrompt is guarded against
// reapplying, which keeps an adaptor that also composes them from injecting a
// second time.
func applyResponsesInstructionsIfNeeded(c *gin.Context, info *relaycommon.RelayInfo, request *dto.OpenAIResponsesRequest) {
	if info == nil || request == nil {
		return
	}

	existing, mergeable := responsesInstructionsText(request.Instructions)
	if !mergeable {
		return
	}
	leadingPrompt := info.LeadingSystemPrompt(existing != "")
	if leadingPrompt == "" {
		return
	}
	if existing != "" {
		common.SetContextKey(c, constant.ContextKeySystemPromptOverride, true)
		leadingPrompt += "\n" + existing
	}
	instructions, err := common.Marshal(leadingPrompt)
	if err != nil {
		return
	}
	request.Instructions = instructions
}

// responsesInstructionsText reports whether instructions can accept a text
// prompt. Missing and null values are mergeable; other non-string JSON values
// are preserved so request validation or the upstream can reject them.
func responsesInstructionsText(instructions json.RawMessage) (string, bool) {
	if len(instructions) == 0 {
		return "", true
	}
	var text *string
	if err := common.Unmarshal(instructions, &text); err != nil {
		return "", false
	}
	if text == nil {
		return "", true
	}
	return strings.TrimSpace(*text), true
}
