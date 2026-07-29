package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertClaudeResponseToResponsesPreservesContentBlockOrder(t *testing.T) {
	c := WithProtocolBridgeContext(context.Background())
	response := &dto.ClaudeResponse{
		Id:    "msg_1",
		Model: "claude-test",
		Content: []dto.ClaudeMediaMessage{
			{Type: "text", Text: respPtr("before")},
			{Type: "tool_use", Id: "toolu_1", Name: "lookup", Input: map[string]any{"q": "x"}},
			{Type: "thinking", Thinking: respPtr("inspect")},
			{Type: "text", Text: respPtr("after")},
			{Type: "tool_use", Id: "toolu_2", Name: "fetch", Input: map[string]any{"id": 2}},
		},
	}

	result, err := ConvertResponse(c, nil, types.RelayFormatOpenAIResponses, response)
	require.NoError(t, err)
	converted := result.Value.(*dto.OpenAIResponsesResponse)

	require.Len(t, converted.Output, 5)
	assert.Equal(t, "message", converted.Output[0].Type)
	assert.Equal(t, "before", converted.Output[0].Content[0].Text)
	assert.Equal(t, "function_call", converted.Output[1].Type)
	assert.Equal(t, "toolu_1", converted.Output[1].CallId)
	assert.Equal(t, "reasoning", converted.Output[2].Type)
	require.Len(t, converted.Output[2].Summary, 1)
	assert.Equal(t, "summary_text", converted.Output[2].Summary[0].Type)
	assert.Equal(t, "inspect", converted.Output[2].Summary[0].Text)
	assert.Empty(t, converted.Output[2].Content)
	assert.Equal(t, "message", converted.Output[3].Type)
	assert.Equal(t, "after", converted.Output[3].Content[0].Text)
	assert.Equal(t, "function_call", converted.Output[4].Type)
	assert.Equal(t, "toolu_2", converted.Output[4].CallId)
}

func TestConvertResponsesResponseToClaudePreservesOutputItemOrder(t *testing.T) {
	c := WithProtocolBridgeContext(context.Background())
	result, err := ConvertResponse(c, &convmeta.Values{
		ChannelMetaAttached: true,
		ChannelID:           17,
		Options:             &convmeta.Options{ProviderStateSecret: "bridge-secret"},
	}, types.RelayFormatClaude, &dto.OpenAIResponsesResponse{
		ID:     "resp_1",
		Model:  "gpt-test",
		Status: []byte(`"completed"`),
		Output: []dto.ResponsesOutput{
			{Type: "message", Role: "assistant", Content: []dto.ResponsesOutputContent{{Type: "output_text", Text: "before"}}},
			{Type: "function_call", ID: "fc_1", CallId: "call_1", Name: "lookup", Arguments: []byte(`{"q":"x"}`)},
			{Type: "reasoning", ID: "rs_1", Summary: []dto.ResponsesReasoningSummaryPart{{Type: "summary_text", Text: "inspect"}}, EncryptedContent: "provider-secret"},
			{Type: "message", Role: "assistant", Content: []dto.ResponsesOutputContent{{Type: "output_text", Text: "after"}}},
			{Type: "function_call", ID: "fc_2", CallId: "call_2", Name: "fetch", Arguments: []byte(`{"id":2}`)},
		},
		Usage: &dto.Usage{InputTokens: 4, OutputTokens: 7, TotalTokens: 11},
	})
	require.NoError(t, err)
	converted := result.Value.(*dto.ClaudeResponse)

	require.Len(t, converted.Content, 5)
	assert.Equal(t, "text", converted.Content[0].Type)
	assert.Equal(t, "before", converted.Content[0].GetText())
	assert.Equal(t, "tool_use", converted.Content[1].Type)
	assert.Equal(t, "call_1", converted.Content[1].Id)
	assert.Equal(t, "thinking", converted.Content[2].Type)
	require.NotNil(t, converted.Content[2].Thinking)
	assert.Equal(t, "inspect", *converted.Content[2].Thinking)
	assert.Empty(t, converted.Content[2].Signature)
	assert.Equal(t, "text", converted.Content[3].Type)
	assert.Equal(t, "after", converted.Content[3].GetText())
	assert.Equal(t, "tool_use", converted.Content[4].Type)
	assert.Equal(t, "call_2", converted.Content[4].Id)
	assert.Equal(t, "tool_use", converted.StopReason)
	assert.Equal(t, 11, result.Usage.TotalTokens)
}

func TestLookupBuiltinResponseConverters(t *testing.T) {
	tests := []struct {
		lookupID       string
		id             string
		from           types.RelayFormat
		to             types.RelayFormat
		quality        ResponseConverterQuality
		stepConverters []string
	}{
		{lookupID: ResponseConverterOAIChatToOAIResponses, id: ConverterOpenAIChatToOpenAIResponses, from: types.RelayFormatOpenAI, to: types.RelayFormatOpenAIResponses, quality: ResponseConverterQualityGood},
		{lookupID: ResponseConverterOAIResponsesToOAIChat, id: ConverterOpenAIResponsesToOpenAIChat, from: types.RelayFormatOpenAIResponses, to: types.RelayFormatOpenAI, quality: ResponseConverterQualityGood},
		{lookupID: ResponseConverterOAIChatToClaudeMessages, id: ConverterOpenAIChatToClaudeMessages, from: types.RelayFormatOpenAI, to: types.RelayFormatClaude, quality: ResponseConverterQualityFair},
		{lookupID: ResponseConverterOAIChatToGeminiChat, id: ConverterOpenAIChatToGeminiContent, from: types.RelayFormatOpenAI, to: types.RelayFormatGemini, quality: ResponseConverterQualityFair},
		{lookupID: ResponseConverterClaudeMessagesToOAIChat, id: ConverterClaudeMessagesToOpenAIChat, from: types.RelayFormatClaude, to: types.RelayFormatOpenAI, quality: ResponseConverterQualityFair},
		{lookupID: ResponseConverterGeminiChatToOAIChat, id: ConverterGeminiContentToOpenAIChat, from: types.RelayFormatGemini, to: types.RelayFormatOpenAI, quality: ResponseConverterQualityFair},
		{
			lookupID: responseConverterClaudeToGemini,
			id:       requestConverterClaudeToGemini,
			from:     types.RelayFormatClaude,
			to:       types.RelayFormatGemini,
			quality:  ResponseConverterQualityDiscouraged,
			stepConverters: []string{
				ConverterClaudeMessagesToOpenAIChat,
				ConverterOpenAIChatToGeminiContent,
			},
		},
		{
			lookupID: responseConverterClaudeToResponses,
			id:       requestConverterClaudeToResponses,
			from:     types.RelayFormatClaude,
			to:       types.RelayFormatOpenAIResponses,
			quality:  ResponseConverterQualityFair,
			stepConverters: []string{
				ConverterClaudeMessagesToOpenAIChat,
				ConverterOpenAIChatToOpenAIResponses,
			},
		},
		{
			lookupID: responseConverterGeminiToClaude,
			id:       requestConverterGeminiToClaude,
			from:     types.RelayFormatGemini,
			to:       types.RelayFormatClaude,
			quality:  ResponseConverterQualityDiscouraged,
		},
		{
			lookupID: responseConverterGeminiToResponses,
			id:       requestConverterGeminiToResponses,
			from:     types.RelayFormatGemini,
			to:       types.RelayFormatOpenAIResponses,
			quality:  ResponseConverterQualityFair,
			stepConverters: []string{
				ConverterGeminiContentToOpenAIChat,
				ConverterOpenAIChatToOpenAIResponses,
			},
		},
		{
			lookupID: responseConverterResponsesToClaude,
			id:       requestConverterResponsesToClaude,
			from:     types.RelayFormatOpenAIResponses,
			to:       types.RelayFormatClaude,
			quality:  ResponseConverterQualityFair,
			stepConverters: []string{
				ConverterOpenAIResponsesToOpenAIChat,
				ConverterOpenAIChatToClaudeMessages,
			},
		},
		{
			lookupID: responseConverterResponsesToGemini,
			id:       ConverterOpenAIResponsesToGemini,
			from:     types.RelayFormatOpenAIResponses,
			to:       types.RelayFormatGemini,
			quality:  ResponseConverterQualityFair,
			stepConverters: []string{
				ConverterOpenAIResponsesToOpenAIChat,
				ConverterOpenAIChatToGeminiContent,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.lookupID, func(t *testing.T) {
			spec, ok := LookupResponseConverter(tt.lookupID)
			require.True(t, ok)
			assert.Equal(t, tt.id, spec.ID)
			assert.Equal(t, tt.from, spec.From)
			assert.Equal(t, tt.to, spec.To)
			assert.Equal(t, tt.quality, spec.Quality)
			assert.Equal(t, tt.stepConverters, spec.StepConverters)
			if len(tt.stepConverters) == 0 {
				assert.NotNil(t, spec.Convert)
			} else {
				assert.Nil(t, spec.Convert)
			}
		})
	}

	_, ok := LookupResponseConverter("missing")
	assert.False(t, ok)
}

func TestConvertResponseRejectsNilAndUnsupportedRoute(t *testing.T) {
	_, err := ConvertResponse(nil, nil, types.RelayFormatOpenAI, (*dto.OpenAITextResponse)(nil))
	require.Error(t, err)

	_, err = ConvertResponse(nil, nil, types.RelayFormatEmbedding, &dto.OpenAITextResponse{})
	require.Error(t, err)
}

func TestConvertResponseDirectConverters(t *testing.T) {
	chat := textRegistryChatResponse()
	info := &convmeta.Values{ChannelMetaAttached: true, UpstreamModelName: "gemini-test"}

	toResponses, err := ConvertResponse(nil, info, types.RelayFormatOpenAIResponses, chat)
	require.NoError(t, err)
	assert.Equal(t, ConverterOpenAIChatToOpenAIResponses, toResponses.Converter)
	assert.Equal(t, ResponseConverterQualityGood, toResponses.Quality)
	assert.Equal(t, types.RelayFormatOpenAI, toResponses.From)
	assert.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), toResponses.To)
	assert.Equal(t, []ResponseStep{{Converter: ConverterOpenAIChatToOpenAIResponses, From: types.RelayFormatOpenAI, To: types.RelayFormatOpenAIResponses}}, toResponses.Steps)
	require.IsType(t, &dto.OpenAIResponsesResponse{}, toResponses.Value)
	assert.Equal(t, 9, toResponses.Usage.TotalTokens)
	require.NotNil(t, toResponses.Usage.BillingUsage)
	require.NotNil(t, toResponses.Usage.BillingUsage.OpenAIUsage)
	assert.Equal(t, dto.BillingUsageSourceOAIChat, toResponses.Usage.BillingUsage.Source)
	assert.Equal(t, 4, toResponses.Usage.BillingUsage.OpenAIUsage.PromptTokens)

	responses := &dto.OpenAIResponsesResponse{
		ID:        "resp_1",
		CreatedAt: 123,
		Model:     "gpt-test",
		Status:    []byte(`"completed"`),
		Output: []dto.ResponsesOutput{
			{
				Type: "message",
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "hello"},
				},
			},
		},
		Usage: &dto.Usage{InputTokens: 4, OutputTokens: 6, TotalTokens: 10},
	}
	toChat, err := ConvertResponse(nil, info, types.RelayFormatOpenAI, responses)
	require.NoError(t, err)
	assert.Equal(t, ConverterOpenAIResponsesToOpenAIChat, toChat.Converter)
	assert.Equal(t, ResponseConverterQualityGood, toChat.Quality)
	require.IsType(t, &dto.OpenAITextResponse{}, toChat.Value)
	assert.Equal(t, 10, toChat.Usage.TotalTokens)
	require.NotNil(t, toChat.Usage.BillingUsage)
	require.NotNil(t, toChat.Usage.BillingUsage.OpenAIUsage)
	assert.Equal(t, dto.BillingUsageSourceOAIResponses, toChat.Usage.BillingUsage.Source)
	assert.Equal(t, 4, toChat.Usage.BillingUsage.OpenAIUsage.InputTokens)

	toClaude, err := ConvertResponse(nil, info, types.RelayFormatClaude, chat)
	require.NoError(t, err)
	assert.Equal(t, ConverterOpenAIChatToClaudeMessages, toClaude.Converter)
	assert.Equal(t, ResponseConverterQualityFair, toClaude.Quality)
	require.IsType(t, &dto.ClaudeResponse{}, toClaude.Value)
	assert.Equal(t, 9, toClaude.Usage.TotalTokens)
	require.NotNil(t, toClaude.Usage.BillingUsage)
	require.NotNil(t, toClaude.Usage.BillingUsage.OpenAIUsage)
	claudeValue := toClaude.Value.(*dto.ClaudeResponse)
	require.NotNil(t, claudeValue.Usage)
	require.NotNil(t, claudeValue.Usage.BillingUsage)
	require.NotNil(t, claudeValue.Usage.BillingUsage.OpenAIUsage)

	toGemini, err := ConvertResponse(nil, info, types.RelayFormatGemini, chat)
	require.NoError(t, err)
	assert.Equal(t, ConverterOpenAIChatToGeminiContent, toGemini.Converter)
	assert.Equal(t, ResponseConverterQualityFair, toGemini.Quality)
	require.IsType(t, &dto.GeminiChatResponse{}, toGemini.Value)
	assert.Equal(t, 9, toGemini.Usage.TotalTokens)
	require.NotNil(t, toGemini.Usage.BillingUsage)
	require.NotNil(t, toGemini.Usage.BillingUsage.OpenAIUsage)
	geminiValue := toGemini.Value.(*dto.GeminiChatResponse)
	require.NotNil(t, geminiValue.UsageMetadata.BillingUsage)
	require.NotNil(t, geminiValue.UsageMetadata.BillingUsage.OpenAIUsage)
}

func TestConvertResponseMultiHopConverters(t *testing.T) {
	responses := textRegistryResponsesResponse()

	toClaude, err := ConvertResponse(nil, &convmeta.Values{}, types.RelayFormatClaude, responses)
	require.NoError(t, err)
	assert.Equal(t, requestConverterResponsesToClaude, toClaude.Converter)
	assert.Equal(t, ResponseConverterQualityFair, toClaude.Quality)
	assert.Equal(t, []ResponseStep{
		{Converter: ConverterOpenAIResponsesToOpenAIChat, From: types.RelayFormatOpenAIResponses, To: types.RelayFormatOpenAI},
		{Converter: ConverterOpenAIChatToClaudeMessages, From: types.RelayFormatOpenAI, To: types.RelayFormatClaude},
	}, toClaude.Steps)
	require.IsType(t, &dto.ClaudeResponse{}, toClaude.Value)
	claudeValue := toClaude.Value.(*dto.ClaudeResponse)
	require.Len(t, claudeValue.Content, 2)
	assert.Equal(t, "text", claudeValue.Content[0].Type)
	assert.Equal(t, "tool_use", claudeValue.Content[1].Type)
	assert.Equal(t, "lookup", claudeValue.Content[1].Name)
	assert.Equal(t, map[string]interface{}{"q": "x"}, claudeValue.Content[1].Input)
	assert.Equal(t, 11, toClaude.Usage.TotalTokens)

	toGemini, err := ConvertResponse(nil, &convmeta.Values{ChannelMetaAttached: true, UpstreamModelName: "gemini-test"}, types.RelayFormatGemini, responses)
	require.NoError(t, err)
	assert.Equal(t, ConverterOpenAIResponsesToGemini, toGemini.Converter)
	assert.Equal(t, ResponseConverterQualityFair, toGemini.Quality)
	assert.Equal(t, []ResponseStep{
		{Converter: ConverterOpenAIResponsesToOpenAIChat, From: types.RelayFormatOpenAIResponses, To: types.RelayFormatOpenAI},
		{Converter: ConverterOpenAIChatToGeminiContent, From: types.RelayFormatOpenAI, To: types.RelayFormatGemini},
	}, toGemini.Steps)
	require.IsType(t, &dto.GeminiChatResponse{}, toGemini.Value)
	geminiValue := toGemini.Value.(*dto.GeminiChatResponse)
	require.Len(t, geminiValue.Candidates, 1)
	require.Len(t, geminiValue.Candidates[0].Content.Parts, 2)
	assert.Equal(t, "hello", geminiValue.Candidates[0].Content.Parts[0].Text)
	require.NotNil(t, geminiValue.Candidates[0].Content.Parts[1].FunctionCall)
	assert.Equal(t, "lookup", geminiValue.Candidates[0].Content.Parts[1].FunctionCall.FunctionName)
	assert.Equal(t, map[string]interface{}{"q": "x"}, geminiValue.Candidates[0].Content.Parts[1].FunctionCall.Arguments)
	assert.Equal(t, 11, toGemini.Usage.TotalTokens)
}

func TestConvertResponseByIDExecutesMultiHopAndChecksSource(t *testing.T) {
	responses := textRegistryResponsesResponse()

	result, err := ConvertResponseByID(nil, nil, responseConverterResponsesToGemini, responses)
	require.NoError(t, err)
	assert.Equal(t, ConverterOpenAIResponsesToGemini, result.Converter)
	assert.Equal(t, []ResponseStep{
		{Converter: ConverterOpenAIResponsesToOpenAIChat, From: types.RelayFormatOpenAIResponses, To: types.RelayFormatOpenAI},
		{Converter: ConverterOpenAIChatToGeminiContent, From: types.RelayFormatOpenAI, To: types.RelayFormatGemini},
	}, result.Steps)

	_, err = ConvertResponseByID(nil, nil, responseConverterResponsesToGemini, textRegistryChatResponse())
	require.Error(t, err)
}

func TestConvertResponseProviderToOAIChatUsage(t *testing.T) {
	claude := &dto.ClaudeResponse{
		Id:         "msg_1",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-test",
		StopReason: "end_turn",
		Content: []dto.ClaudeMediaMessage{
			{Type: "tool_use", Id: "toolu_1", Name: "lookup", Input: map[string]interface{}{"q": "x"}},
		},
		Usage: &dto.ClaudeUsage{
			InputTokens:              10,
			CacheReadInputTokens:     3,
			CacheCreationInputTokens: 4,
			OutputTokens:             5,
			CacheCreation: &dto.ClaudeCacheCreationUsage{
				Ephemeral5mInputTokens: 1,
				Ephemeral1hInputTokens: 3,
			},
		},
	}
	toChat, err := ConvertResponse(nil, nil, types.RelayFormatOpenAI, claude)
	require.NoError(t, err)
	assert.Equal(t, ConverterClaudeMessagesToOpenAIChat, toChat.Converter)
	require.IsType(t, &dto.OpenAITextResponse{}, toChat.Value)
	assert.Equal(t, 17, toChat.Usage.PromptTokens)
	assert.Equal(t, 5, toChat.Usage.CompletionTokens)
	assert.Equal(t, 22, toChat.Usage.TotalTokens)
	assert.Equal(t, 3, toChat.Usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 4, toChat.Usage.PromptTokensDetails.CachedCreationTokens)
	assert.Equal(t, 4, toChat.Usage.PromptTokensDetails.CacheWriteTokens)
	require.NotNil(t, toChat.Usage.BillingUsage)
	require.NotNil(t, toChat.Usage.BillingUsage.ClaudeUsage)
	assert.Equal(t, dto.BillingUsageSourceClaudeMessages, toChat.Usage.BillingUsage.Source)
	assert.Equal(t, dto.BillingUsageSemanticAnthropic, toChat.Usage.BillingUsage.Semantic)
	assert.Equal(t, 10, toChat.Usage.BillingUsage.ClaudeUsage.InputTokens)
	assert.Equal(t, 3, toChat.Usage.BillingUsage.ClaudeUsage.CacheReadInputTokens)
	assert.Equal(t, 4, toChat.Usage.BillingUsage.ClaudeUsage.CacheCreationInputTokens)
	assert.Equal(t, 5, toChat.Usage.BillingUsage.ClaudeUsage.OutputTokens)
	chatValue := toChat.Value.(*dto.OpenAITextResponse)
	require.Len(t, chatValue.Choices, 1)
	require.Len(t, chatValue.Choices[0].Message.ParseToolCalls(), 1)
	assert.JSONEq(t, `{"q":"x"}`, chatValue.Choices[0].Message.ParseToolCalls()[0].Function.Arguments)

	gemini := &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Parts: []dto.GeminiPart{
						{Text: "hello"},
						{FunctionCall: &dto.FunctionCall{FunctionName: "lookup", Arguments: map[string]interface{}{"q": "x"}}},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        7,
			ToolUsePromptTokenCount: 2,
			CandidatesTokenCount:    5,
			ThoughtsTokenCount:      3,
			TotalTokenCount:         17,
			CachedContentTokenCount: 4,
			PromptTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "TEXT", TokenCount: 5},
				{Modality: "IMAGE", TokenCount: 1},
			},
			ToolUsePromptTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "AUDIO", TokenCount: 3},
			},
			CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "TEXT", TokenCount: 4},
				{Modality: "IMAGE", TokenCount: 1},
			},
		},
	}
	toChat, err = ConvertResponse(nil, &convmeta.Values{ChannelMetaAttached: true, UpstreamModelName: "gemini-test"}, types.RelayFormatOpenAI, gemini)
	require.NoError(t, err)
	assert.Equal(t, ConverterGeminiContentToOpenAIChat, toChat.Converter)
	require.IsType(t, &dto.OpenAITextResponse{}, toChat.Value)
	assert.Equal(t, 9, toChat.Usage.PromptTokens)
	assert.Equal(t, 8, toChat.Usage.CompletionTokens)
	assert.Equal(t, 17, toChat.Usage.TotalTokens)
	assert.Equal(t, 3, toChat.Usage.CompletionTokenDetails.ReasoningTokens)
	assert.Equal(t, 4, toChat.Usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 5, toChat.Usage.PromptTokensDetails.TextTokens)
	assert.Equal(t, 3, toChat.Usage.PromptTokensDetails.AudioTokens)
	assert.Equal(t, 1, toChat.Usage.PromptTokensDetails.ImageTokens)
	assert.Equal(t, 4, toChat.Usage.CompletionTokenDetails.TextTokens)
	assert.Equal(t, 1, toChat.Usage.CompletionTokenDetails.ImageTokens)
	require.NotNil(t, toChat.Usage.BillingUsage)
	require.NotNil(t, toChat.Usage.BillingUsage.GeminiUsageMetadata)
	assert.Equal(t, dto.BillingUsageSourceGeminiChat, toChat.Usage.BillingUsage.Source)
	assert.Equal(t, dto.BillingUsageSemanticGemini, toChat.Usage.BillingUsage.Semantic)
	assert.Equal(t, 7, toChat.Usage.BillingUsage.GeminiUsageMetadata.PromptTokenCount)
	assert.Equal(t, 2, toChat.Usage.BillingUsage.GeminiUsageMetadata.ToolUsePromptTokenCount)
	assert.Equal(t, 17, toChat.Usage.BillingUsage.GeminiUsageMetadata.TotalTokenCount)
}

func TestConvertResponseMapsGeminiBlockedPromptToClaudeRefusal(t *testing.T) {
	blockReason := "SAFETY"
	result, err := ConvertResponse(nil, &convmeta.Values{ChannelMetaAttached: true, UpstreamModelName: "gemini-test"}, types.RelayFormatClaude, &dto.GeminiChatResponse{
		PromptFeedback: &dto.GeminiChatPromptFeedback{BlockReason: &blockReason},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount: 4,
			TotalTokenCount:  4,
		},
		HasUsageMetadata: true,
	})

	require.NoError(t, err)
	response, ok := result.Value.(*dto.ClaudeResponse)
	require.True(t, ok)
	assert.Equal(t, "refusal", response.StopReason)
	require.Len(t, response.Content, 1)
	assert.Equal(t, "text", response.Content[0].Type)
	assert.Equal(t, "Request blocked by Gemini safety filters: SAFETY", response.Content[0].GetText())
	require.NotNil(t, response.Usage)
	assert.Equal(t, 4, response.Usage.InputTokens)
	assert.Zero(t, response.Usage.OutputTokens)
}

func TestConvertGeminiResponseToClaudeMatchesCCSwitchVisibleParts(t *testing.T) {
	stop := "STOP"
	result, err := ConvertResponse(nil, &convmeta.Values{ChannelMetaAttached: true, UpstreamModelName: "gemini-test"}, types.RelayFormatClaude, &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				FinishReason: &stop,
				Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{
					{Thought: true, Text: "private chain of thought"},
					{Text: "before"},
					{InlineData: &dto.GeminiInlineData{MimeType: "image/png", Data: "aGVsbG8="}},
					{FunctionCall: &dto.FunctionCall{ID: "call_1", FunctionName: "lookup", Arguments: map[string]interface{}{"q": "x"}}},
					{Text: "after"},
				}},
			},
			{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{Text: "second candidate"}}}},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, requestConverterGeminiToClaude, result.Converter)
	assert.Equal(t, []ResponseStep{{Converter: requestConverterGeminiToClaude, From: types.RelayFormatGemini, To: types.RelayFormatClaude}}, result.Steps)
	response := result.Value.(*dto.ClaudeResponse)
	require.Len(t, response.Content, 3)
	assert.Equal(t, "text", response.Content[0].Type)
	assert.Equal(t, "before", response.Content[0].GetText())
	assert.Equal(t, "tool_use", response.Content[1].Type)
	assert.Equal(t, "call_1", response.Content[1].Id)
	assert.Equal(t, "lookup", response.Content[1].Name)
	assert.Equal(t, map[string]interface{}{"q": "x"}, response.Content[1].Input)
	assert.Equal(t, "text", response.Content[2].Type)
	assert.Equal(t, "after", response.Content[2].GetText())
	assert.Equal(t, "tool_use", response.StopReason)
}

func TestConvertGeminiResponseToClaudeDoesNotInventSafetyText(t *testing.T) {
	safety := "SAFETY"
	result, err := ConvertResponse(nil, nil, types.RelayFormatClaude, &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{FinishReason: &safety}},
	})

	require.NoError(t, err)
	response := result.Value.(*dto.ClaudeResponse)
	assert.Equal(t, "refusal", response.StopReason)
	assert.Empty(t, response.Content)
}

func TestConvertGeminiStreamToClaudeSkipsThoughtsAndDefersTools(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatGemini, types.RelayFormatClaude, ResponseStreamOptions{
		ID:    "msg_1",
		Model: "public-model",
	})
	require.NoError(t, err)

	first, err := ConvertStreamResponseChunk(nil, nil, state, &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{
			{Thought: true, Text: "private"},
			{Text: "Hel"},
			{FunctionCall: &dto.FunctionCall{ID: "call_1", FunctionName: "lookup", Arguments: map[string]interface{}{"q": "x"}}},
		}}}},
	})
	require.NoError(t, err)
	assertClaudeStreamHasTextDelta(t, first, "Hel")
	assertClaudeStreamLacksType(t, first, "thinking_delta")
	assertClaudeStreamLacksType(t, first, "input_json_delta")

	stop := "STOP"
	second, err := ConvertStreamResponseChunk(nil, nil, state, &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			FinishReason: &stop,
			Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{
				{Thought: true, Text: "private reasoning"},
				{Text: "Hello"},
				{FunctionCall: &dto.FunctionCall{ID: "call_1", FunctionName: "lookup", Arguments: map[string]interface{}{"q": "x"}}},
			}},
		}},
		UsageMetadata:    dto.GeminiUsageMetadata{PromptTokenCount: 2, CandidatesTokenCount: 3, TotalTokenCount: 5},
		HasUsageMetadata: true,
	})
	require.NoError(t, err)
	assertClaudeStreamHasTextDelta(t, second, "lo")
	assertClaudeStreamLacksType(t, second, "thinking_delta")
	assertClaudeStreamHasType(t, second, "input_json_delta")
	assertClaudeStreamHasType(t, second, "message_stop")

	finalized, err := FinalizeStreamResponse(nil, nil, state)
	require.NoError(t, err)
	assert.Empty(t, finalized)
}

func assertClaudeStreamHasTextDelta(t *testing.T, results []ResponseResult, expected string) {
	t.Helper()
	for _, result := range results {
		response, ok := result.Value.(*dto.ClaudeResponse)
		if ok && response.Delta != nil && response.Delta.Text != nil && *response.Delta.Text == expected {
			return
		}
	}
	t.Fatalf("Claude stream did not contain text delta %q", expected)
}

func assertClaudeStreamHasType(t *testing.T, results []ResponseResult, eventType string) {
	t.Helper()
	for _, result := range results {
		response, ok := result.Value.(*dto.ClaudeResponse)
		if !ok {
			continue
		}
		if response.Type == eventType || response.Delta != nil && response.Delta.Type == eventType {
			return
		}
	}
	t.Fatalf("Claude stream did not contain type %q", eventType)
}

func assertClaudeStreamLacksType(t *testing.T, results []ResponseResult, eventType string) {
	t.Helper()
	for _, result := range results {
		response, ok := result.Value.(*dto.ClaudeResponse)
		if !ok {
			continue
		}
		if response.Type == eventType || response.Delta != nil && response.Delta.Type == eventType {
			t.Fatalf("Claude stream unexpectedly contained type %q", eventType)
		}
	}
}

func TestConvertResponsePreservesBillingUsageAcrossChatResponsesBridge(t *testing.T) {
	chat := textRegistryChatResponse()
	chat.Usage.BillingUsage = dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{
		InputTokens:              10,
		CacheReadInputTokens:     3,
		CacheCreationInputTokens: 4,
		OutputTokens:             5,
	})

	toResponses, err := ConvertResponse(nil, nil, types.RelayFormatOpenAIResponses, chat)
	require.NoError(t, err)
	require.NotNil(t, toResponses.Usage.BillingUsage)
	require.NotNil(t, toResponses.Usage.BillingUsage.ClaudeUsage)
	assert.Equal(t, 10, toResponses.Usage.BillingUsage.ClaudeUsage.InputTokens)

	responsesValue := toResponses.Value.(*dto.OpenAIResponsesResponse)
	toChat, err := ConvertResponse(nil, nil, types.RelayFormatOpenAI, responsesValue)
	require.NoError(t, err)
	require.NotNil(t, toChat.Usage.BillingUsage)
	require.NotNil(t, toChat.Usage.BillingUsage.ClaudeUsage)
	assert.Equal(t, 4, toChat.Usage.BillingUsage.ClaudeUsage.CacheCreationInputTokens)
}

func TestConvertResponseUsesBillingUsageWhenRestoringNativeTargets(t *testing.T) {
	chat := textRegistryChatResponse()
	chat.Usage.BillingUsage = dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{
		InputTokens:              10,
		CacheReadInputTokens:     3,
		CacheCreationInputTokens: 4,
		OutputTokens:             5,
	})

	toClaude, err := ConvertResponse(nil, nil, types.RelayFormatClaude, chat)
	require.NoError(t, err)
	claudeValue := toClaude.Value.(*dto.ClaudeResponse)
	require.NotNil(t, claudeValue.Usage)
	assert.Equal(t, 10, claudeValue.Usage.InputTokens)
	assert.Equal(t, 3, claudeValue.Usage.CacheReadInputTokens)
	assert.Equal(t, 4, claudeValue.Usage.CacheCreationInputTokens)
	assert.Equal(t, 5, claudeValue.Usage.OutputTokens)

	chat.Usage.BillingUsage = dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{
		PromptTokenCount:        7,
		ToolUsePromptTokenCount: 2,
		CandidatesTokenCount:    5,
		ThoughtsTokenCount:      3,
		TotalTokenCount:         17,
	})

	toGemini, err := ConvertResponse(nil, nil, types.RelayFormatGemini, chat)
	require.NoError(t, err)
	geminiValue := toGemini.Value.(*dto.GeminiChatResponse)
	assert.Equal(t, 7, geminiValue.UsageMetadata.PromptTokenCount)
	assert.Equal(t, 2, geminiValue.UsageMetadata.ToolUsePromptTokenCount)
	assert.Equal(t, 5, geminiValue.UsageMetadata.CandidatesTokenCount)
	assert.Equal(t, 3, geminiValue.UsageMetadata.ThoughtsTokenCount)
	assert.Equal(t, 17, geminiValue.UsageMetadata.TotalTokenCount)
}

func TestConvertStreamResponseDirectConverters(t *testing.T) {
	info := &convmeta.Values{
		ClaudeConvertInfo: &convmeta.ClaudeConvertInfo{
			LastMessagesType: convmeta.LastMessageTypeNone,
		},
	}
	info.SendResponseCount = 1
	finishReason := "stop"
	result, err := ConvertStreamResponse(nil, info, types.RelayFormatClaude, &dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_1",
		Model: "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				FinishReason: &finishReason,
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Content: respPtr("hello"),
				},
			},
		},
		Usage: &dto.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5},
	})
	require.NoError(t, err)
	assert.True(t, result.Stream)
	assert.Equal(t, ConverterOpenAIChatToClaudeMessages, result.Converter)
	require.IsType(t, []*dto.ClaudeResponse{}, result.Value)
	assert.Equal(t, 5, result.Usage.TotalTokens)

	result, err = ConvertStreamResponse(nil, &convmeta.Values{ChannelMetaAttached: true, UpstreamModelName: "gemini-test"}, types.RelayFormatOpenAI, &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{Text: "hello"}}}}},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     1,
			CandidatesTokenCount: 2,
			TotalTokenCount:      3,
		},
	})
	require.NoError(t, err)
	assert.True(t, result.Stream)
	assert.Equal(t, ConverterGeminiContentToOpenAIChat, result.Converter)
	require.IsType(t, &dto.ChatCompletionsStreamResponse{}, result.Value)
	assert.Equal(t, 3, result.Usage.TotalTokens)
}

func TestConvertStreamResponseStatefulDirectConverters(t *testing.T) {
	chatState, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{
		ID:    "resp_1",
		Model: "gpt-test",
	})
	require.NoError(t, err)
	chatResults, err := ConvertStreamResponseChunk(nil, nil, chatState, &dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_1",
		Model: "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: respPtr("hello")}},
		},
		Usage: &dto.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5},
	})
	require.NoError(t, err)
	require.NotEmpty(t, chatResults)
	assert.Equal(t, ConverterOpenAIChatToOpenAIResponses, chatResults[0].Converter)
	assert.Equal(t, []ResponseStep{{Converter: ConverterOpenAIChatToOpenAIResponses, From: types.RelayFormatOpenAI, To: types.RelayFormatOpenAIResponses}}, chatResults[0].Steps)
	assert.Equal(t, 5, chatState.Usage().TotalTokens)

	finalResults, err := FinalizeStreamResponse(nil, nil, chatState)
	require.NoError(t, err)
	require.NotEmpty(t, finalResults)
	lastEvent, ok := finalResults[len(finalResults)-1].Value.(ChatToResponsesStreamEvent)
	require.True(t, ok)
	assert.Equal(t, "response.incomplete", lastEvent.Type)
	require.NotNil(t, lastEvent.Payload.Response)
	require.NotNil(t, lastEvent.Payload.Response.IncompleteDetails)
	assert.Equal(t, "max_output_tokens", lastEvent.Payload.Response.IncompleteDetails.Reason)

	responsesState, err := NewResponseStreamState(types.RelayFormatOpenAIResponses, types.RelayFormatOpenAI, ResponseStreamOptions{
		ID:    "chatcmpl_1",
		Model: "gpt-test",
	})
	require.NoError(t, err)
	responsesResults, err := ConvertStreamResponseChunk(nil, nil, responsesState, &dto.ResponsesStreamResponse{
		Type:  "response.output_text.delta",
		Delta: "hello",
	})
	require.NoError(t, err)
	require.NotEmpty(t, responsesResults)
	assert.Equal(t, ConverterOpenAIResponsesToOpenAIChat, responsesResults[0].Converter)
	assert.Equal(t, []ResponseStep{{Converter: ConverterOpenAIResponsesToOpenAIChat, From: types.RelayFormatOpenAIResponses, To: types.RelayFormatOpenAI}}, responsesResults[0].Steps)
	require.IsType(t, dto.ChatCompletionsStreamResponse{}, responsesResults[len(responsesResults)-1].Value)
}

func TestConvertStreamResponseStatefulMultiHopResponsesToClaude(t *testing.T) {
	info := &convmeta.Values{
		ClaudeConvertInfo: &convmeta.ClaudeConvertInfo{
			LastMessagesType: convmeta.LastMessageTypeNone,
		},
	}
	state, err := NewResponseStreamState(types.RelayFormatOpenAIResponses, types.RelayFormatClaude, ResponseStreamOptions{
		ID:    "chatcmpl_1",
		Model: "gpt-test",
	})
	require.NoError(t, err)

	results, err := ConvertStreamResponseChunk(nil, info, state, &dto.ResponsesStreamResponse{
		Type:  "response.output_text.delta",
		Delta: "hello",
	})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Equal(t, requestConverterResponsesToClaude, results[0].Converter)
	assert.Equal(t, []ResponseStep{
		{Converter: ConverterOpenAIResponsesToOpenAIChat, From: types.RelayFormatOpenAIResponses, To: types.RelayFormatOpenAI},
		{Converter: ConverterOpenAIChatToClaudeMessages, From: types.RelayFormatOpenAI, To: types.RelayFormatClaude},
	}, results[0].Steps)

	var sawTextDelta bool
	for _, result := range results {
		claudeResponse, ok := result.Value.(*dto.ClaudeResponse)
		if !ok || claudeResponse == nil {
			continue
		}
		if claudeResponse.Type == "content_block_delta" && claudeResponse.Delta != nil && claudeResponse.Delta.Text != nil && *claudeResponse.Delta.Text == "hello" {
			sawTextDelta = true
		}
	}
	assert.True(t, sawTextDelta)

	state.SetUsage(&dto.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5})
	_, err = FinalizeStreamResponse(nil, info, state)
	require.NoError(t, err)
	assert.Equal(t, 5, state.Usage().TotalTokens)
}

func TestConvertResponseResponsesToClaudeKeepsToolSearchAmongMixedOutput(t *testing.T) {
	result, err := ConvertResponse(WithProtocolBridgeContext(context.Background()), nil, types.RelayFormatClaude, &dto.OpenAIResponsesResponse{
		ID:     "resp_tool_search",
		Model:  "gpt-test",
		Status: []byte(`"completed"`),
		Output: []dto.ResponsesOutput{
			{
				Type: "message",
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "I will search."},
				},
			},
			{
				Type:      "tool_search_call",
				ID:        "ts_1",
				CallId:    "call_search_1",
				Arguments: []byte(`{"query":"mail"}`),
			},
		},
	})
	require.NoError(t, err)

	response, ok := result.Value.(*dto.ClaudeResponse)
	require.True(t, ok)
	require.Len(t, response.Content, 2)
	assert.Equal(t, "text", response.Content[0].Type)
	assert.Equal(t, "I will search.", response.Content[0].GetText())
	assert.Equal(t, "tool_use", response.Content[1].Type)
	assert.Equal(t, "call_search_1", response.Content[1].Id)
	assert.Equal(t, "tool_search", response.Content[1].Name)
	assert.Equal(t, map[string]any{"query": "mail"}, response.Content[1].Input)
}

func TestConvertResponseResponsesToClaudeIgnoresNamelessToolWithoutShiftingValidTool(t *testing.T) {
	result, err := ConvertResponse(WithProtocolBridgeContext(context.Background()), nil, types.RelayFormatClaude, &dto.OpenAIResponsesResponse{
		ID:     "resp_tools",
		Model:  "gpt-test",
		Status: []byte(`"completed"`),
		Output: []dto.ResponsesOutput{
			{Type: "function_call", ID: "fc_bad", CallId: "call_bad", Arguments: []byte(`{}`)},
			{
				Type: "message",
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "Using the valid tool."},
				},
			},
			{Type: "function_call", ID: "fc_good", CallId: "call_good", Name: "lookup", Arguments: []byte(`{"q":"x"}`)},
		},
	})
	require.NoError(t, err)

	response, ok := result.Value.(*dto.ClaudeResponse)
	require.True(t, ok)
	require.Len(t, response.Content, 2)
	assert.Equal(t, "text", response.Content[0].Type)
	assert.Equal(t, "Using the valid tool.", response.Content[0].GetText())
	assert.Equal(t, "tool_use", response.Content[1].Type)
	assert.Equal(t, "call_good", response.Content[1].Id)
	assert.Equal(t, "lookup", response.Content[1].Name)
}

func TestResponsesToClaudeReasoningDoesNotExposeProviderState(t *testing.T) {
	meta := &convmeta.Values{
		ChannelMetaAttached: true,
		ChannelID:           17,
		Options:             &convmeta.Options{ProviderStateSecret: "bridge-secret"},
	}
	responseResult, err := ConvertResponse(WithProtocolBridgeContext(context.Background()), meta, types.RelayFormatClaude, &dto.OpenAIResponsesResponse{
		ID:     "resp_1",
		Model:  "gpt-test",
		Status: []byte(`"completed"`),
		Output: []dto.ResponsesOutput{
			{
				Type:             "reasoning",
				ID:               "rs_1",
				Summary:          []dto.ResponsesReasoningSummaryPart{{Type: "summary_text", Text: "inspect inputs"}},
				EncryptedContent: "provider-secret",
			},
			{Type: "function_call", ID: "fc_1", CallId: "call_1", Name: "lookup", Arguments: []byte(`{"q":"x"}`)},
		},
	})
	require.NoError(t, err)
	claudeResponse, ok := responseResult.Value.(*dto.ClaudeResponse)
	require.True(t, ok)
	require.Len(t, claudeResponse.Content, 2)
	assert.Equal(t, "thinking", claudeResponse.Content[0].Type)
	assert.Equal(t, "inspect inputs", *claudeResponse.Content[0].Thinking)
	assert.Empty(t, claudeResponse.Content[0].Signature)
	assert.Equal(t, "tool_use", claudeResponse.Content[1].Type)
	encodedResponse, err := kitutil.Marshal(claudeResponse)
	require.NoError(t, err)
	assert.NotContains(t, string(encodedResponse), "provider-secret")
	assert.NotContains(t, string(encodedResponse), "signature")

	nextRequest := &dto.ClaudeRequest{
		Model: "gpt-test",
		Messages: []dto.ClaudeMessage{
			{Role: "assistant", Content: claudeResponse.Content},
			{Role: "user", Content: []dto.ClaudeMediaMessage{{Type: "tool_result", ToolUseId: "call_1", Content: "done"}}},
		},
		Tools: []dto.Tool{{Name: "lookup", InputSchema: map[string]any{"type": "object"}}},
	}
	requestResult, err := ConvertRequest(WithProtocolBridgeContext(context.Background()), meta, types.RelayFormatOpenAIResponses, nextRequest)
	require.NoError(t, err)
	converted, ok := requestResult.Value.(*dto.OpenAIResponsesRequest)
	require.True(t, ok)
	var input []map[string]any
	require.NoError(t, kitutil.Unmarshal(converted.Input, &input))
	require.NotEmpty(t, input)
	for _, item := range input {
		assert.NotEqual(t, "reasoning", item["type"])
		assert.NotEqual(t, "provider-secret", item["encrypted_content"])
	}
}

func TestResponsesToClaudeStreamClosesReasoningWithoutSignatureBeforeToolBlock(t *testing.T) {
	meta := &convmeta.Values{
		ChannelMetaAttached: true,
		ChannelID:           17,
		Options:             &convmeta.Options{ProviderStateSecret: "bridge-secret"},
	}
	state, err := NewResponseStreamState(types.RelayFormatOpenAIResponses, types.RelayFormatClaude, ResponseStreamOptions{Model: "gpt-test"})
	require.NoError(t, err)

	events := []*dto.ResponsesStreamResponse{
		{Type: "response.created", Response: &dto.OpenAIResponsesResponse{ID: "resp_1", Model: "gpt-test"}},
		{Type: "response.reasoning_summary_text.delta", Delta: "inspect inputs"},
		{Type: "response.output_item.done", OutputIndex: respPtr(0), Item: &dto.ResponsesOutput{
			Type:             "reasoning",
			ID:               "rs_1",
			Summary:          []dto.ResponsesReasoningSummaryPart{{Type: "summary_text", Text: "inspect inputs"}},
			EncryptedContent: "provider-secret",
		}},
		{Type: "response.output_item.added", OutputIndex: respPtr(1), Item: &dto.ResponsesOutput{
			Type:      "function_call",
			ID:        "fc_1",
			CallId:    "call_1",
			Name:      "lookup",
			Arguments: []byte(`{}`),
		}},
	}

	convertedEvents := make([]*dto.ClaudeResponse, 0)
	for _, event := range events {
		results, convertErr := ConvertStreamResponseChunk(context.Background(), meta, state, event)
		require.NoError(t, convertErr)
		for _, result := range results {
			if converted, ok := result.Value.(*dto.ClaudeResponse); ok && converted != nil {
				convertedEvents = append(convertedEvents, converted)
			}
		}
	}

	thinkingStopIndex := -1
	toolStartIndex := -1
	for index, event := range convertedEvents {
		if event.Type == "content_block_delta" && event.Delta != nil && event.Delta.Type == "signature_delta" {
			t.Fatalf("Responses provider state leaked as Anthropic signature_delta: %#v", event.Delta)
		}
		if event.Type == "content_block_start" && event.ContentBlock != nil && event.ContentBlock.Type == "redacted_thinking" {
			t.Fatalf("Responses provider state leaked as Anthropic redacted_thinking: %#v", event.ContentBlock)
		}
		if event.Type == "content_block_stop" && thinkingStopIndex == -1 {
			thinkingStopIndex = index
		}
		if event.Type == "content_block_start" && event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
			toolStartIndex = index
		}
	}
	require.NotEqual(t, -1, thinkingStopIndex)
	require.NotEqual(t, -1, toolStartIndex)
	assert.Less(t, thinkingStopIndex, toolStartIndex)
}

func TestResponseUsageMatrixChatAndResponsesDetails(t *testing.T) {
	chat := textRegistryChatResponse()
	chat.Usage = dto.Usage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      20,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         3,
			CachedCreationTokens: 2,
			CacheWriteTokens:     6,
			TextTokens:           4,
			AudioTokens:          1,
			ImageTokens:          5,
		},
		CompletionTokenDetails: dto.OutputTokenDetails{
			ReasoningTokens: 2,
			TextTokens:      2,
			AudioTokens:     1,
			ImageTokens:     2,
		},
	}
	result, err := ConvertResponse(nil, nil, types.RelayFormatOpenAIResponses, chat)
	require.NoError(t, err)
	assert.Equal(t, 10, result.Usage.InputTokens)
	assert.Equal(t, 5, result.Usage.OutputTokens)
	assert.Equal(t, 20, result.Usage.TotalTokens)
	require.NotNil(t, result.Usage.InputTokensDetails)
	assert.Equal(t, 3, result.Usage.InputTokensDetails.CachedTokens)
	assert.Equal(t, 2, result.Usage.InputTokensDetails.CachedCreationTokens)
	assert.Equal(t, 6, result.Usage.InputTokensDetails.CacheWriteTokens)
	assert.Equal(t, 4, result.Usage.InputTokensDetails.TextTokens)
	assert.Equal(t, 1, result.Usage.InputTokensDetails.AudioTokens)
	assert.Equal(t, 5, result.Usage.InputTokensDetails.ImageTokens)
	assert.Equal(t, 2, result.Usage.CompletionTokenDetails.ReasoningTokens)
	assert.Equal(t, 2, result.Usage.CompletionTokenDetails.TextTokens)
	assert.Equal(t, 1, result.Usage.CompletionTokenDetails.AudioTokens)
	assert.Equal(t, 2, result.Usage.CompletionTokenDetails.ImageTokens)

	responses := &dto.OpenAIResponsesResponse{
		ID:        "resp_1",
		Status:    []byte(`"completed"`),
		Model:     "gpt-test",
		Output:    []dto.ResponsesOutput{},
		CreatedAt: 123,
		Usage: &dto.Usage{
			InputTokens:  12,
			OutputTokens: 8,
			TotalTokens:  21,
			InputTokensDetails: &dto.InputTokenDetails{
				CachedTokens:         4,
				CachedCreationTokens: 1,
				CacheWriteTokens:     7,
				TextTokens:           5,
				AudioTokens:          2,
				ImageTokens:          1,
			},
			CompletionTokenDetails: dto.OutputTokenDetails{
				ReasoningTokens: 3,
				TextTokens:      4,
				AudioTokens:     1,
				ImageTokens:     3,
			},
		},
	}
	result, err = ConvertResponse(nil, nil, types.RelayFormatOpenAI, responses)
	require.NoError(t, err)
	assert.Equal(t, 12, result.Usage.PromptTokens)
	assert.Equal(t, 8, result.Usage.CompletionTokens)
	assert.Equal(t, 21, result.Usage.TotalTokens)
	assert.Equal(t, 4, result.Usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 1, result.Usage.PromptTokensDetails.CachedCreationTokens)
	assert.Equal(t, 7, result.Usage.PromptTokensDetails.CacheWriteTokens)
	assert.Equal(t, 5, result.Usage.PromptTokensDetails.TextTokens)
	assert.Equal(t, 2, result.Usage.PromptTokensDetails.AudioTokens)
	assert.Equal(t, 1, result.Usage.PromptTokensDetails.ImageTokens)
	assert.Equal(t, 3, result.Usage.CompletionTokenDetails.ReasoningTokens)
	assert.Equal(t, 4, result.Usage.CompletionTokenDetails.TextTokens)
	assert.Equal(t, 1, result.Usage.CompletionTokenDetails.AudioTokens)
	assert.Equal(t, 3, result.Usage.CompletionTokenDetails.ImageTokens)
}

func textRegistryChatResponse() *dto.OpenAITextResponse {
	msg := dto.Message{
		Role:    "assistant",
		Content: "hello",
	}
	msg.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:   "call_1",
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      "lookup",
				Arguments: `{"q":"x"}`,
			},
		},
	})
	return &dto.OpenAITextResponse{
		Id:      "chatcmpl_1",
		Model:   "gpt-test",
		Created: 123,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: "tool_calls",
			},
		},
		Usage: dto.Usage{PromptTokens: 4, CompletionTokens: 5, TotalTokens: 9},
	}
}

func textRegistryResponsesResponse() *dto.OpenAIResponsesResponse {
	return &dto.OpenAIResponsesResponse{
		ID:        "resp_1",
		CreatedAt: 123,
		Model:     "gpt-test",
		Status:    []byte(`"completed"`),
		Output: []dto.ResponsesOutput{
			{
				Type: "message",
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "hello"},
				},
			},
			{
				Type:      "function_call",
				ID:        "call_1",
				CallId:    "call_1",
				Name:      "lookup",
				Arguments: []byte(`{"q":"x"}`),
			},
		},
		Usage: &dto.Usage{InputTokens: 4, OutputTokens: 7, TotalTokens: 11},
	}
}

func respPtr[T any](value T) *T {
	return &value
}
