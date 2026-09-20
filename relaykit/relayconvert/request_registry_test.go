package relayconvert

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	sharedgemini "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/gemini"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtocolCatalogPreservesOperationAndTransportBoundaries(t *testing.T) {
	for _, tc := range []struct {
		from      Protocol
		operation Operation
		transport Transport
		target    Protocol
	}{
		{ProtocolResponses, OperationCompact, TransportSSE, ProtocolResponses},
		{ProtocolChat, OperationGenerate, TransportWebSocket, ProtocolChat},
		{ProtocolResponses, OperationGenerate, TransportWebSocket, ProtocolMessages},
		{ProtocolMessages, OperationCompletions, TransportHTTP, ProtocolMessages},
	} {
		_, err := PlanConversions(tc.from, tc.operation, tc.transport, []Protocol{tc.target}, RequestFeatureSet{}, "safe")
		require.Error(t, err)
	}
	plans, err := PlanConversions(ProtocolResponses, OperationCompact, TransportHTTP, []Protocol{ProtocolMessages, ProtocolResponses}, RequestFeatureSet{}, "safe")
	require.NoError(t, err)
	require.Len(t, plans, 2)
	assert.Equal(t, ProtocolResponses, plans[0].UpstreamProtocol)
	assert.Equal(t, CompactionNative, plans[0].CompactionMode)
	assert.Equal(t, ProtocolMessages, plans[1].UpstreamProtocol)
	assert.Equal(t, CompactionSummary, plans[1].CompactionMode)
	_, err = PlanConversions(ProtocolChat, OperationGenerate, TransportHTTP, []Protocol{ProtocolChat}, RequestFeatureSet{}, "unrecognized")
	require.Error(t, err)
}

func TestProtocolPlansRejectRequiredSemanticLoss(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to Protocol
		body     string
		contains string
	}{
		{"chat stop", ProtocolChat, ProtocolResponses, `{"stop":["END"]}`, "stop sequences"},
		{"chat candidates", ProtocolChat, ProtocolMessages, `{"n":2}`, "multiple output candidates"},
		{"chat output format", ProtocolChat, ProtocolMessages, `{"response_format":{"type":"json_object"}}`, "output format"},
		{"chat audio", ProtocolChat, ProtocolMessages, `{"messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"AA==","format":"wav"}}]}]}`, "input_audio"},
		{"chat unknown content", ProtocolChat, ProtocolGemini, `{"messages":[{"role":"user","content":[{"type":"future_content","value":"required"}]}]}`, "future_content"},
		{"chat unknown field", ProtocolChat, ProtocolResponses, `{"future_execution_constraint":true}`, "future_execution_constraint"},
		{"gemini audio", ProtocolGemini, ProtocolChat, `{"contents":[{"parts":[{"inlineData":{"mimeType":"audio/wav","data":"AA=="}}]}]}`, "media:audio/wav"},
		{"gemini video", ProtocolGemini, ProtocolMessages, `{"contents":[{"parts":[{"fileData":{"mimeType":"video/mp4","fileUri":"https://example.test/a.mp4"}}]}]}`, "media:video/mp4"},
		{"gemini signature", ProtocolGemini, ProtocolResponses, `{"contents":[{"parts":[{"text":"thought","thoughtSignature":"opaque"}]}]}`, "provider-bound state"},
		{"gemini schema", ProtocolGemini, ProtocolChat, `{"generationConfig":{"responseMimeType":"application/json","responseSchema":{"type":"OBJECT","required":["answer"]}}}`, "generationConfig.response"},
		{"gemini top k", ProtocolGemini, ProtocolMessages, `{"generationConfig":{"topK":0}}`, "top_k"},
		{"gemini stop truncation", ProtocolGemini, ProtocolChat, `{"generationConfig":{"stopSequences":["a","b","c","d","e"]}}`, "stop sequence count"},
		{"gemini unknown content", ProtocolGemini, ProtocolChat, `{"contents":[{"parts":[{"executableCode":{"code":"print(1)"}}]}]}`, "executableCode"},
		{"gemini cached history", ProtocolGemini, ProtocolChat, `{"cachedContent":"cachedContents/123"}`, "cachedContent"},
		{"gemini text after call", ProtocolGemini, ProtocolChat, `{"contents":[{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{}}},{"text":"after"}]}]}`, "content_after_function_call"},
		{"gemini conflicting part payloads", ProtocolGemini, ProtocolChat, `{"contents":[{"role":"model","parts":[{"text":"before","functionCall":{"name":"lookup","args":{}}}]}]}`, "multiple_part_payloads"},
		{"gemini text before result", ProtocolGemini, ProtocolResponses, `{"contents":[{"role":"user","parts":[{"text":"before"},{"functionResponse":{"name":"lookup","response":{}}}]}]}`, "content_before_function_response"},
		{"gemini system media", ProtocolGemini, ProtocolMessages, `{"systemInstruction":{"parts":[{"inlineData":{"mimeType":"image/png","data":"AA=="}}]}}`, "system:inlineData"},
		{"responses output constraint", ProtocolResponses, ProtocolMessages, `{"text":{"format":{"type":"json_schema","schema":{"type":"object"}}}}`, "output format"},
		{"responses opaque state", ProtocolResponses, ProtocolChat, `{"input":[{"type":"reasoning","encrypted_content":"opaque"}]}`, "provider-bound state"},
		{"responses search include", ProtocolResponses, ProtocolMessages, `{"include":["web_search_call.action.sources"]}`, "include"},
		{"responses reasoning include chat", ProtocolResponses, ProtocolChat, `{"include":["reasoning.encrypted_content"]}`, "include"},
		{"gemini schema pattern", ProtocolChat, ProtocolGemini, `{"response_format":{"type":"json_schema","json_schema":{"schema":{"type":"string","pattern":"^OK$"}}}}`, "pattern"},
		{"gemini exclusive schema", ProtocolResponses, ProtocolGemini, `{"text":{"format":{"type":"json_schema","schema":{"oneOf":[{"type":"integer"},{"type":"number"}]}}}}`, "oneOf"},
		{"messages native format", ProtocolMessages, ProtocolChat, `{"output_config":{"format":{"type":"json_schema","schema":{"type":"object"}}}}`, "output_config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			features, err := ExtractRequestFeatureSet(tc.from, []byte(tc.body))
			require.NoError(t, err)
			for _, policy := range []string{"lossless", "safe"} {
				_, err := PlanConversions(tc.from, OperationGenerate, TransportHTTP, []Protocol{tc.to}, features, policy)
				require.ErrorContains(t, err, tc.contains)
			}
			plans, err := PlanConversions(tc.from, OperationGenerate, TransportHTTP, []Protocol{tc.to, tc.from}, features, "safe")
			require.NoError(t, err)
			require.Len(t, plans, 1)
			assert.Equal(t, tc.from, plans[0].UpstreamProtocol)
		})
	}
}

func TestConversionPresentationMetadataPolicy(t *testing.T) {
	const body = `{"model":"test","input":"hello","max_output_tokens":64,"metadata":{"label":"example"},"client_metadata":{"label":"example"},"user":"example"}`
	features, err := ExtractRequestFeatureSet(ProtocolResponses, []byte(body))
	require.NoError(t, err)
	_, err = PlanConversions(ProtocolResponses, OperationGenerate, TransportHTTP, []Protocol{ProtocolMessages}, features, "lossless")
	require.Error(t, err)
	plans, err := PlanConversions(ProtocolResponses, OperationGenerate, TransportHTTP, []Protocol{ProtocolMessages}, features, "safe")
	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.ElementsMatch(t, []string{"metadata", "client_metadata", "user"}, plans[0].Losses)
	var request dto.OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(body), &request))
	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict} {
		session := NewConversionSession(&convmeta.Values{Options: &convmeta.Options{ToolLossPolicy: policy}})
		defer session.Close()
		result, err := session.Request(nil, types.RelayFormatClaude, &request)
		if policy == types.ConversionLossPolicyStrict {
			var loss *types.ConversionLossError
			require.ErrorAs(t, err, &loss)
			continue
		}
		require.NoError(t, err)
		require.Len(t, result.Diagnostics, 3)
		for _, diagnostic := range result.Diagnostics {
			assert.Equal(t, "omitted_presentation_metadata", diagnostic.Code)
			assert.Equal(t, types.ConversionLossPresentation, diagnostic.LossClass)
		}
	}
}

func TestConversionPreservesGeminiContentBeforeToolCall(t *testing.T) {
	var request dto.GeminiChatRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"contents":[{"role":"model","parts":[{"text":"Inspect this image"},{"inlineData":{"mimeType":"image/png","data":"AA=="}},{"functionCall":{"id":"call_1","name":"lookup","args":{"q":"image"}}}]}]}`), &request))
	session := NewConversionSession(nil)
	defer session.Close()
	result, err := session.Request(nil, types.RelayFormatOpenAI, &request)
	require.NoError(t, err)
	converted := result.Value.(*dto.GeneralOpenAIRequest)
	require.Len(t, converted.Messages, 1)
	content := converted.Messages[0].ParseContent()
	require.Len(t, content, 2)
	assert.Equal(t, "Inspect this image", content[0].Text)
	assert.Equal(t, "image_url", content[1].Type)
	calls := converted.Messages[0].ParseToolCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "call_1", calls[0].ID)
	assert.Equal(t, "lookup", calls[0].Function.Name)
}

func TestResponsesReasoningIncludeUsesMessagesState(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{Model: "test", Input: []byte(`"hello"`), MaxOutputTokens: kitutil.GetPointer(uint(64)), Include: []byte(`["reasoning.encrypted_content"]`)}
	session := NewConversionSession(nil)
	defer session.Close()
	result, err := session.Request(nil, types.RelayFormatClaude, request)
	require.NoError(t, err)
	require.IsType(t, &dto.ClaudeRequest{}, result.Value)
	assert.Empty(t, result.Diagnostics)
}

func TestConversionSessionResolvesForcedToolsAfterReasoning(t *testing.T) {
	for _, model := range []string{"claude-sonnet-5", "claude-fable-5"} {
		for _, protocol := range []Protocol{ProtocolChat, ProtocolResponses} {
			t.Run(model+"/"+string(protocol), func(t *testing.T) {
				var request any
				if protocol == ProtocolChat {
					request = &dto.GeneralOpenAIRequest{Model: model, MaxTokens: kitutil.GetPointer(uint(4096)), Messages: []dto.Message{{Role: "user", Content: "hello"}}, Tools: []dto.ToolCallRequest{{Type: "function", Function: dto.FunctionRequest{Name: "lookup", Parameters: map[string]any{"type": "object"}}}}, ToolChoice: "required"}
				} else {
					request = &dto.OpenAIResponsesRequest{Model: model, MaxOutputTokens: kitutil.GetPointer(uint(4096)), Input: []byte(`"hello"`), Tools: []byte(`[{"type":"function","name":"lookup","parameters":{"type":"object"}}]`), ToolChoice: []byte(`"required"`)}
				}
				session := NewConversionSession(nil)
				defer session.Close()
				result, err := session.Request(nil, types.RelayFormatClaude, request)
				if model == "claude-fable-5" {
					require.ErrorContains(t, err, "cannot honor a forced tool_choice")
					return
				}
				require.NoError(t, err)
				converted := result.Value.(*dto.ClaudeRequest)
				require.NotNil(t, converted.Thinking)
				assert.Equal(t, "disabled", converted.Thinking.Type)
			})
		}
	}
}

func TestConversionSessionRejectsLossBeforeMediaResolution(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Model: "test", MaxTokens: kitutil.GetPointer(uint(64)), ResponseFormat: &dto.ResponseFormat{Type: "json_object"}, Messages: []dto.Message{{Role: "user", Content: "hello"}}}
	session := NewConversionSession(nil)
	defer session.Close()
	result, err := session.Request(nil, types.RelayFormatClaude, request)
	var loss *types.ConversionLossError
	require.ErrorAs(t, err, &loss)
	require.NotNil(t, result)
	assert.Nil(t, result.Value)
	assert.Contains(t, loss.Error(), "output format")
}

func TestProtocolConversionsPreserveOutputSchemaConstraints(t *testing.T) {
	const schema = `{"type":"object","additionalProperties":false,"required":["answer"],"properties":{"answer":{"type":"integer","minimum":0}}}`
	for _, source := range []Protocol{ProtocolChat, ProtocolResponses} {
		t.Run(string(source), func(t *testing.T) {
			var request any
			if source == ProtocolChat {
				request = &dto.GeneralOpenAIRequest{Model: "test", Messages: []dto.Message{{Role: "user", Content: "hello"}}, ResponseFormat: &dto.ResponseFormat{Type: "json_schema", JsonSchema: []byte(`{"name":"answer","strict":true,"schema":` + schema + `}`)}}
			} else {
				request = &dto.OpenAIResponsesRequest{Model: "test", Input: []byte(`"hello"`), Text: []byte(`{"format":{"type":"json_schema","name":"answer","strict":true,"schema":` + schema + `}}`)}
			}
			result, err := NewConversionSession(nil).Request(nil, types.RelayFormatGemini, request)
			require.NoError(t, err)
			converted := result.Value.(*dto.GeminiChatRequest)
			assert.Equal(t, "application/json", converted.GenerationConfig.ResponseMimeType)
			assert.JSONEq(t, schema, string(converted.GenerationConfig.ResponseJsonSchema))
			assert.Nil(t, converted.GenerationConfig.ResponseSchema)
		})
	}
}

func TestRequestConverterRegistryListsSupportedTextConverters(t *testing.T) {
	tests := []struct {
		converter      string
		from           types.RelayFormat
		to             types.RelayFormat
		quality        RequestConverterQuality
		stepConverters []string
		advancedCustom bool
	}{
		{converter: ConverterClaudeMessagesToOpenAIChat, from: types.RelayFormatClaude, to: types.RelayFormatOpenAI, quality: RequestConverterQualityFair, advancedCustom: true},
		{converter: ConverterGeminiContentToOpenAIChat, from: types.RelayFormatGemini, to: types.RelayFormatOpenAI, quality: RequestConverterQualityFair, advancedCustom: true},
		{converter: ConverterOpenAIChatToClaudeMessages, from: types.RelayFormatOpenAI, to: types.RelayFormatClaude, quality: RequestConverterQualityFair, advancedCustom: true},
		{converter: ConverterOpenAIChatToGeminiContent, from: types.RelayFormatOpenAI, to: types.RelayFormatGemini, quality: RequestConverterQualityFair, advancedCustom: true},
		{converter: ConverterOpenAIChatToOpenAIResponses, from: types.RelayFormatOpenAI, to: types.RelayFormatOpenAIResponses, quality: RequestConverterQualityGood, advancedCustom: true},
		{converter: ConverterOpenAIResponsesToOpenAIChat, from: types.RelayFormatOpenAIResponses, to: types.RelayFormatOpenAI, quality: RequestConverterQualityGood, advancedCustom: true},
		{
			converter: requestConverterClaudeToGemini,
			from:      types.RelayFormatClaude,
			to:        types.RelayFormatGemini,
			quality:   RequestConverterQualityDiscouraged,
		},
		{
			converter:      requestConverterClaudeToResponses,
			from:           types.RelayFormatClaude,
			to:             types.RelayFormatOpenAIResponses,
			quality:        RequestConverterQualityFair,
			advancedCustom: true,
		},
		{
			converter: requestConverterGeminiToClaude,
			from:      types.RelayFormatGemini,
			to:        types.RelayFormatClaude,
			quality:   RequestConverterQualityDiscouraged,
			stepConverters: []string{
				ConverterGeminiContentToOpenAIChat,
				ConverterOpenAIChatToClaudeMessages,
			},
		},
		{
			converter: requestConverterGeminiToResponses,
			from:      types.RelayFormatGemini,
			to:        types.RelayFormatOpenAIResponses,
			quality:   RequestConverterQualityFair,
			stepConverters: []string{
				ConverterGeminiContentToOpenAIChat,
				ConverterOpenAIChatToOpenAIResponses,
			},
		},
		{
			converter:      requestConverterResponsesToClaude,
			from:           types.RelayFormatOpenAIResponses,
			to:             types.RelayFormatClaude,
			quality:        RequestConverterQualityFair,
			advancedCustom: true,
		},
		{
			converter:      ConverterOpenAIResponsesToGemini,
			from:           types.RelayFormatOpenAIResponses,
			to:             types.RelayFormatGemini,
			quality:        RequestConverterQualityFair,
			advancedCustom: true,
		},
	}

	require.Len(t, requestConverters, len(tests))

	for _, tt := range tests {
		t.Run(tt.converter, func(t *testing.T) {
			spec, ok := LookupRequestConverter(tt.converter)

			require.True(t, ok)
			assert.Equal(t, tt.converter, spec.ID)
			assert.Equal(t, tt.from, spec.From)
			assert.Equal(t, tt.to, spec.To)
			assert.Equal(t, tt.quality, spec.Quality)
			assert.Equal(t, tt.stepConverters, spec.StepConverters)
			if len(tt.stepConverters) == 0 {
				assert.NotNil(t, spec.Convert)
			} else {
				assert.Nil(t, spec.Convert)
			}
			target, err := ResolveTarget(CanonicalPath(ProtocolForFormat(tt.from), "test"), string(ProtocolForFormat(tt.to)), "")
			require.NoError(t, err)
			assert.Equal(t, ProtocolForFormat(tt.to), target)
		})
	}
}

func TestConvertRequestToTargetRecordsConversionChain(t *testing.T) {
	info := &convmeta.Values{
		ConversionChain: []types.RelayFormat{types.RelayFormatOpenAI},
	}
	req := &dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		Messages: []dto.Message{
			{Role: "user", Content: "hello"},
		},
	}

	result, err := ConvertRequest(nil, info, types.RelayFormatOpenAIResponses, req)

	require.NoError(t, err)
	require.IsType(t, &dto.OpenAIResponsesRequest{}, result.Value)
	assert.Equal(t, types.RelayFormatOpenAI, result.From)
	assert.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), result.To)
	assert.Equal(t, ConverterOpenAIChatToOpenAIResponses, result.Converter)
	assert.Equal(t, RequestConverterQualityGood, result.Quality)
	assert.Equal(t, []RequestStep{
		{
			Converter: ConverterOpenAIChatToOpenAIResponses,
			From:      types.RelayFormatOpenAI,
			To:        types.RelayFormatOpenAIResponses,
		},
	}, result.Steps)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses}, info.ConversionChain)
}

func TestConvertRequestClaudeToResponsesUsesDirectConverter(t *testing.T) {
	info := &convmeta.Values{
		ConversionChain: []types.RelayFormat{types.RelayFormatClaude},
	}
	req := &dto.ClaudeRequest{
		Model: "claude-test",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
	}

	result, err := ConvertRequest(nil, info, types.RelayFormatOpenAIResponses, req)

	require.NoError(t, err)
	require.IsType(t, &dto.OpenAIResponsesRequest{}, result.Value)
	assert.Equal(t, types.RelayFormat(types.RelayFormatClaude), result.From)
	assert.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), result.To)
	assert.Equal(t, requestConverterClaudeToResponses, result.Converter)
	assert.Equal(t, RequestConverterQualityFair, result.Quality)
	assert.Equal(t, []RequestStep{
		{
			Converter: requestConverterClaudeToResponses,
			From:      types.RelayFormatClaude,
			To:        types.RelayFormatOpenAIResponses,
		},
	}, result.Steps)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses}, info.ConversionChain)
}

func TestConvertRequestClaudeToChatResolvesToolResultNames(t *testing.T) {
	req := &dto.ClaudeRequest{
		Model: "claude-test",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: []dto.ClaudeMediaMessage{
				{Type: "tool_result", ToolUseId: "call_1", Content: "before call"},
			}},
			{Role: "assistant", Content: []dto.ClaudeMediaMessage{
				{Type: "tool_use", Id: "call_1", Name: "first", Input: map[string]any{}},
			}},
			{Role: "user", Content: []dto.ClaudeMediaMessage{
				{Type: "tool_result", ToolUseId: "call_1", Content: "after call"},
				{Type: "tool_result", ToolUseId: "missing", Content: "unknown"},
				{Type: "tool_result", ToolUseId: "call_1", Name: "explicit", Content: "named"},
			}},
			{Role: "assistant", Content: []dto.ClaudeMediaMessage{
				{Type: "tool_use", Id: "call_1", Name: "later", Input: map[string]any{}},
			}},
		},
	}

	result, err := ConvertRequestByID(nil, nil, ConverterClaudeMessagesToOpenAIChat, req)
	require.NoError(t, err)
	chatReq, ok := result.Value.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Len(t, chatReq.Messages, 6)
	for _, tt := range []struct {
		index int
		id    string
		name  string
	}{
		{0, "call_1", "first"},
		{2, "call_1", "first"},
		{3, "missing", ""},
		{4, "call_1", "explicit"},
	} {
		message := chatReq.Messages[tt.index]
		assert.Equal(t, "tool", message.Role)
		assert.Equal(t, tt.id, message.ToolCallId)
		require.NotNil(t, message.Name)
		assert.Equal(t, tt.name, *message.Name)
	}
	assert.Equal(t, "assistant", chatReq.Messages[1].Role)
	assert.Equal(t, "assistant", chatReq.Messages[5].Role)
}

func TestConvertRequestClaudeToResponsesPreservesMixedBlockOrder(t *testing.T) {
	info := &convmeta.Values{ConversionChain: []types.RelayFormat{types.RelayFormatClaude}}
	stream := true
	strict := true
	maxTokens := uint(4096)
	req := &dto.ClaudeRequest{
		Model:     "gpt-test",
		System:    []dto.ClaudeMediaMessage{{Type: "text", Text: kitutil.GetPointer("system ")}, {Type: "text", Text: kitutil.GetPointer("rules")}},
		MaxTokens: &maxTokens,
		Stream:    &stream,
		Tools: []dto.Tool{{
			Name:        "lookup",
			Description: "Look up a value",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
			Strict:      &strict,
		}},
		ToolChoice: dto.ClaudeToolChoice{Type: "tool", Name: "lookup", DisableParallelToolUse: true},
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: []dto.ClaudeMediaMessage{{Type: "text", Text: kitutil.GetPointer("question")}}},
			{Role: "assistant", Content: []dto.ClaudeMediaMessage{
				{Type: "text", Text: kitutil.GetPointer("before")},
				{Type: "tool_use", Id: "call_1", Name: "lookup", Input: map[string]any{"q": "x"}},
				{Type: "text", Text: kitutil.GetPointer("after")},
			}},
			{Role: "user", Content: []dto.ClaudeMediaMessage{
				{Type: "tool_result", ToolUseId: "call_1", Content: "result"},
				{Type: "text", Text: kitutil.GetPointer("continue")},
			}},
		},
	}

	result, err := ConvertRequest(nil, info, types.RelayFormatOpenAIResponses, req)
	require.NoError(t, err)
	responsesReq := result.Value.(*dto.OpenAIResponsesRequest)
	assert.Equal(t, "gpt-test", responsesReq.Model)
	assert.Equal(t, maxTokens, *responsesReq.MaxOutputTokens)
	assert.True(t, *responsesReq.Stream)
	assert.JSONEq(t, `"system \n\nrules"`, string(responsesReq.Instructions))
	assert.JSONEq(t, `[{"type":"function","name":"lookup","description":"Look up a value","parameters":{"type":"object","properties":{"q":{"type":"string"}}},"strict":true}]`, string(responsesReq.Tools))
	assert.JSONEq(t, `{"type":"function","name":"lookup"}`, string(responsesReq.ToolChoice))
	assert.JSONEq(t, `false`, string(responsesReq.ParallelToolCalls))

	var input []map[string]any
	require.NoError(t, kitutil.Unmarshal(responsesReq.Input, &input))
	require.Len(t, input, 6)
	assert.Equal(t, "user", input[0]["role"])
	assert.Equal(t, "question", inputContentText(t, input[0]))
	assert.Equal(t, "assistant", input[1]["role"])
	assert.Equal(t, "before", inputContentText(t, input[1]))
	assert.Equal(t, "function_call", input[2]["type"])
	assert.Equal(t, "call_1", input[2]["call_id"])
	assert.Equal(t, "lookup", input[2]["name"])
	assert.JSONEq(t, `{"q":"x"}`, input[2]["arguments"].(string))
	assert.Equal(t, "assistant", input[3]["role"])
	assert.Equal(t, "after", inputContentText(t, input[3]))
	assert.Equal(t, "function_call_output", input[4]["type"])
	assert.Equal(t, "result", input[4]["output"])
	assert.Equal(t, "user", input[5]["role"])
	assert.Equal(t, "continue", inputContentText(t, input[5]))
}

func TestConvertRequestClaudeToResponsesRejectsIncompatibleContextManagement(t *testing.T) {
	req := &dto.ClaudeRequest{
		Model: "gpt-test",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
		ContextManagement: mustRawMessage(t, map[string]any{
			"edits": []map[string]any{{"type": "clear_tool_uses_20250919"}},
		}),
	}

	_, err := ConvertRequest(nil, nil, types.RelayFormatOpenAIResponses, req)
	require.ErrorContains(t, err, "context_management")
}

func TestConvertRequestClaudeAdaptiveThinkingPreservesEffort(t *testing.T) {
	tests := []struct {
		name         string
		outputConfig []byte
		wantEffort   string
	}{
		{name: "adaptive default", wantEffort: "high"},
		{name: "explicit low", outputConfig: mustRawMessage(t, map[string]any{"effort": "low"}), wantEffort: "low"},
		{name: "explicit xhigh", outputConfig: mustRawMessage(t, map[string]any{"effort": "xhigh"}), wantEffort: "xhigh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &convmeta.Values{
				OriginModelName: "gpt-5.6-sol",
				ConversionChain: []types.RelayFormat{types.RelayFormatClaude},
			}
			req := &dto.ClaudeRequest{
				Model:        "gpt-5.6-sol",
				OutputConfig: tt.outputConfig,
				Thinking:     &dto.Thinking{Type: "adaptive", Display: "summarized"},
				Messages: []dto.ClaudeMessage{
					{Role: "user", Content: "hello"},
				},
			}

			result, err := ConvertRequest(nil, info, types.RelayFormatOpenAIResponses, req)

			require.NoError(t, err)
			responsesReq, ok := result.Value.(*dto.OpenAIResponsesRequest)
			require.True(t, ok)
			require.NotNil(t, responsesReq.Reasoning)
			assert.Equal(t, tt.wantEffort, responsesReq.Reasoning.Effort)
			assert.Equal(t, "detailed", responsesReq.Reasoning.Summary)
			assert.Equal(t, tt.wantEffort, info.GetReasoningEffort())
		})
	}
}

func TestGeminiThinkingLevelCaseInsensitiveAcrossPaths(t *testing.T) {
	newRequest := func(level string) *dto.GeminiChatRequest {
		return &dto.GeminiChatRequest{
			Contents: []dto.GeminiChatContent{{Role: "user", Parts: []dto.GeminiPart{{Text: "hello"}}}},
			GenerationConfig: dto.GeminiChatGenerationConfig{
				ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingLevel: level},
			},
		}
	}

	t.Run("native passthrough records canonical effort without rewriting wire value", func(t *testing.T) {
		info := &convmeta.Values{OriginModelName: "gemini-3.7-flash", UpstreamModelName: "gemini-3.7-flash"}
		req := newRequest(" MEDIUM ")
		require.NoError(t, ApplyGeminiThinkingConfigChecked(req, info))
		assert.Equal(t, "medium", info.GetReasoningEffort())
		assert.Equal(t, " MEDIUM ", req.GenerationConfig.ThinkingConfig.ThinkingLevel)
	})

	t.Run("native passthrough keeps unknown level as sent", func(t *testing.T) {
		info := &convmeta.Values{OriginModelName: "gemini-3.7-flash", UpstreamModelName: "gemini-3.7-flash"}
		req := newRequest("ULTRA")
		require.NoError(t, ApplyGeminiThinkingConfigChecked(req, info))
		assert.Equal(t, "ULTRA", info.GetReasoningEffort())
	})

	t.Run("suffix state canonicalizes uppercase level against normalized effort", func(t *testing.T) {
		info := &convmeta.Values{
			OriginModelName:     "gemini-3.7-flash-thinking-medium",
			UpstreamModelName:   "gemini-3.7-flash",
			ChannelMetaAttached: true,
			ReasoningConversion: &dto.ReasoningConversionState{Mode: "enabled", Effort: "medium"},
		}
		req := newRequest("MEDIUM")
		require.NoError(t, ApplyGeminiThinkingConfigChecked(req, info))
		assert.Equal(t, "medium", info.GetReasoningEffort())
		assert.Equal(t, "medium", req.GenerationConfig.ThinkingConfig.ThinkingLevel)
	})

	t.Run("gemini to openai conversion accepts uppercase level", func(t *testing.T) {
		info := &convmeta.Values{
			OriginModelName:   "gemini-3.7-flash",
			UpstreamModelName: "gemini-3.7-flash",
			ConversionChain:   []types.RelayFormat{types.RelayFormatGemini},
		}
		result, err := ConvertRequest(nil, info, types.RelayFormatOpenAI, newRequest("MEDIUM"))
		require.NoError(t, err)
		openaiReq, ok := result.Value.(*dto.GeneralOpenAIRequest)
		require.True(t, ok)
		assert.Equal(t, "medium", openaiReq.ReasoningEffort)
		assert.Equal(t, "medium", info.GetReasoningEffort())
	})

	t.Run("gemini to openai conversion adjusts unsupported level with a diagnostic", func(t *testing.T) {
		info := &convmeta.Values{
			OriginModelName:   "gemini-3-pro-preview",
			UpstreamModelName: "gemini-3-pro-preview",
			ConversionChain:   []types.RelayFormat{types.RelayFormatGemini},
		}
		result, err := ConvertRequest(nil, info, types.RelayFormatOpenAI, newRequest("MINIMAL"))
		require.NoError(t, err)
		openaiReq, ok := result.Value.(*dto.GeneralOpenAIRequest)
		require.True(t, ok)
		assert.Equal(t, "low", openaiReq.ReasoningEffort)
		assert.Equal(t, "low", info.GetReasoningEffort())
		codes := make([]string, 0, len(result.Diagnostics))
		for _, diagnostic := range result.Diagnostics {
			codes = append(codes, diagnostic.Code)
		}
		assert.Contains(t, codes, "gemini_level_adjusted")
	})
}

func TestConvertRequestViaExecutesExplicitPath(t *testing.T) {
	info := &convmeta.Values{
		ConversionChain: []types.RelayFormat{types.RelayFormatOpenAI},
	}
	req := &dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		Messages: []dto.Message{
			{Role: "user", Content: "hello"},
		},
	}

	result, err := ConvertRequestVia(nil, info, req, types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses)

	require.NoError(t, err)
	require.IsType(t, &dto.OpenAIResponsesRequest{}, result.Value)
	assert.Equal(t, []RequestStep{
		{
			Converter: ConverterOpenAIChatToOpenAIResponses,
			From:      types.RelayFormatOpenAI,
			To:        types.RelayFormatOpenAIResponses,
		},
	}, result.Steps)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses}, info.ConversionChain)
}

func TestConvertRequestResponsesToGeminiLowersCustomToolsAndHistory(t *testing.T) {
	info := &convmeta.Values{
		ConversionChain:     []types.RelayFormat{types.RelayFormatOpenAIResponses},
		ChannelMetaAttached: true,
		UpstreamModelName:   "gemini-test",
	}
	req := &dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"role":    "user",
				"content": "next turn",
			},
			{
				"type":    "reasoning",
				"summary": []map[string]any{{"type": "summary_text", "text": "inspect"}},
			},
			{
				"type":    "custom_tool_call",
				"call_id": "call_custom",
				"name":    "apply_patch",
				"input":   "patch body",
			},
			{
				"type":    "custom_tool_call_output",
				"call_id": "call_custom",
				"output":  "ok",
			},
			{
				"type":    "function_call_output",
				"call_id": "call_orphan",
				"output":  "orphaned output",
			},
		}),
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "custom", "name": "apply_patch", "description": "apply a patch"},
		}),
	}

	result, err := ConvertRequest(nil, info, types.RelayFormatGemini, req)

	require.NoError(t, err)
	geminiReq, ok := result.Value.(*dto.GeminiChatRequest)
	require.True(t, ok)

	tools := geminiReq.GetTools()
	require.Len(t, tools, 1)
	functions, err := kitutil.Any2Type[[]dto.FunctionRequest](tools[0].FunctionDeclarations)
	require.NoError(t, err)
	require.Len(t, functions, 1)
	assert.Equal(t, "apply_patch", functions[0].Name)

	require.Len(t, geminiReq.Contents, 3)
	assert.Equal(t, "user", geminiReq.Contents[0].Role)
	assert.Equal(t, "next turn", geminiReq.Contents[0].Parts[0].Text)
	assert.Equal(t, "model", geminiReq.Contents[1].Role)
	functionCall := geminiReq.Contents[1].Parts[0].FunctionCall
	require.NotNil(t, functionCall)
	assert.Equal(t, "apply_patch", functionCall.FunctionName)
	assert.Equal(t, map[string]any{"input": "patch body"}, functionCall.Arguments)
	assert.Equal(t, "user", geminiReq.Contents[2].Role)
	functionResponse := geminiReq.Contents[2].Parts[0].FunctionResponse
	require.NotNil(t, functionResponse)
	assert.Equal(t, "apply_patch", functionResponse.Name)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatGemini}, info.ConversionChain)
}

func TestConvertRequestResponsesToGeminiUsesDirectConverter(t *testing.T) {
	info := &convmeta.Values{
		Options:             &convmeta.Options{Gemini: convmeta.GeminiOptions{FunctionCallThoughtSignatureEnabled: true}},
		ConversionChain:     []types.RelayFormat{types.RelayFormatOpenAIResponses},
		ChannelMetaAttached: true,
		UpstreamModelName:   "gemini-test",
	}
	maxOutputTokens := uint(256)
	req := &dto.OpenAIResponsesRequest{
		Model:           "gemini-test",
		Instructions:    mustRawMessage(t, "system rules"),
		MaxOutputTokens: &maxOutputTokens,
		Input: mustRawMessage(t, []map[string]any{
			{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "I will call."},
				},
			},
			{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "lookup",
				"arguments": map[string]any{"q": "x"},
			},
			{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  map[string]any{"ok": true},
			},
		}),
		Tools: mustRawMessage(t, []map[string]any{
			{
				"type":        "function",
				"name":        "lookup",
				"description": "Lookup data",
				"parameters": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"propertyNames":        map[string]any{"pattern": "^[a-z]+$"},
					"properties": map[string]any{
						"q": map[string]any{
							"type":             "string",
							"exclusiveMinimum": 0,
						},
						"filters": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type":                 "object",
								"additionalProperties": true,
								"properties": map[string]any{
									"name": map[string]any{"type": "string"},
								},
							},
						},
					},
				},
			},
		}),
		Text: mustRawMessage(t, map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   "answer",
				"schema": map[string]any{"type": "object"},
			},
		}),
	}

	result, err := ConvertRequest(nil, info, types.RelayFormatGemini, req)

	require.NoError(t, err)
	geminiReq, ok := result.Value.(*dto.GeminiChatRequest)
	require.True(t, ok)
	assert.Equal(t, ConverterOpenAIResponsesToGemini, result.Converter)
	assert.Equal(t, []RequestStep{
		{
			Converter: ConverterOpenAIResponsesToGemini,
			From:      types.RelayFormatOpenAIResponses,
			To:        types.RelayFormatGemini,
		},
	}, result.Steps)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatGemini}, info.ConversionChain)

	require.NotNil(t, geminiReq.SystemInstructions)
	require.Len(t, geminiReq.SystemInstructions.Parts, 1)
	assert.Equal(t, "system rules", geminiReq.SystemInstructions.Parts[0].Text)
	assert.Equal(t, "application/json", geminiReq.GenerationConfig.ResponseMimeType)
	assert.Equal(t, maxOutputTokens, *geminiReq.GenerationConfig.MaxOutputTokens)

	tools := geminiReq.GetTools()
	require.Len(t, tools, 1)
	functions, err := kitutil.Any2Type[[]dto.FunctionRequest](tools[0].FunctionDeclarations)
	require.NoError(t, err)
	require.Len(t, functions, 1)
	assert.Equal(t, "lookup", functions[0].Name)
	assert.Nil(t, functions[0].Parameters)
	params, ok := functions[0].ParametersJsonSchema.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "object", params["type"])
	assert.Equal(t, false, params["additionalProperties"])
	assert.Contains(t, params, "propertyNames")
	properties, ok := params["properties"].(map[string]any)
	require.True(t, ok)
	queryParam, ok := properties["q"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", queryParam["type"])
	assert.Equal(t, float64(0), queryParam["exclusiveMinimum"])
	filterParam, ok := properties["filters"].(map[string]any)
	require.True(t, ok)
	filterItems, ok := filterParam["items"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, filterItems["additionalProperties"])

	require.Len(t, geminiReq.Contents, 2)
	assert.Equal(t, "model", geminiReq.Contents[0].Role)
	require.Len(t, geminiReq.Contents[0].Parts, 2)
	functionCall := geminiReq.Contents[0].Parts[0].FunctionCall
	require.NotNil(t, functionCall)
	assert.Equal(t, "lookup", functionCall.FunctionName)
	assert.Equal(t, map[string]any{"q": "x"}, functionCall.Arguments)
	var thoughtSignature string
	require.NoError(t, kitutil.Unmarshal(geminiReq.Contents[0].Parts[0].ThoughtSignature, &thoughtSignature))
	assert.Equal(t, sharedgemini.ThoughtSignatureBypassValue, thoughtSignature)
	assert.Equal(t, "I will call.", geminiReq.Contents[0].Parts[1].Text)

	assert.Equal(t, "user", geminiReq.Contents[1].Role)
	require.Len(t, geminiReq.Contents[1].Parts, 1)
	functionResponse := geminiReq.Contents[1].Parts[0].FunctionResponse
	require.NotNil(t, functionResponse)
	assert.Equal(t, "lookup", functionResponse.Name)
	assert.Equal(t, true, functionResponse.Response["ok"])
	assert.Empty(t, geminiReq.Contents[1].Parts[0].ThoughtSignature)
}

func TestConvertRequestResponsesToGeminiSkipsThoughtSignatureWhenDisabled(t *testing.T) {
	info := &convmeta.Values{
		Options:             &convmeta.Options{Gemini: convmeta.GeminiOptions{FunctionCallThoughtSignatureEnabled: false}},
		ConversionChain:     []types.RelayFormat{types.RelayFormatOpenAIResponses},
		ChannelMetaAttached: true,
		UpstreamModelName:   "gemini-test",
	}
	req := &dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "lookup",
				"arguments": map[string]any{"q": "x"},
			},
			{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  map[string]any{"ok": true},
			},
		}),
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
		}),
	}

	result, err := ConvertRequest(nil, info, types.RelayFormatGemini, req)

	require.NoError(t, err)
	geminiReq, ok := result.Value.(*dto.GeminiChatRequest)
	require.True(t, ok)
	require.Len(t, geminiReq.Contents, 2)
	require.Len(t, geminiReq.Contents[0].Parts, 1)
	require.NotNil(t, geminiReq.Contents[0].Parts[0].FunctionCall)
	assert.Empty(t, geminiReq.Contents[0].Parts[0].ThoughtSignature)
}

func TestConvertRequestOpenAIChatToGeminiAddsThoughtSignatureForAdvancedCustom(t *testing.T) {
	assistantMessage := dto.Message{Role: "assistant", Content: ""}
	assistantMessage.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:   "call_1",
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      "lookup",
				Arguments: `{"q":"x"}`,
			},
		},
	})
	info := &convmeta.Values{
		Options:             &convmeta.Options{Gemini: convmeta.GeminiOptions{FunctionCallThoughtSignatureEnabled: true}},
		ConversionChain:     []types.RelayFormat{types.RelayFormatOpenAI},
		ChannelMetaAttached: true,
		ChannelType:         58, // advanced-custom in the host
		UpstreamModelName:   "gemini-test",
	}
	req := &dto.GeneralOpenAIRequest{
		Model: "gemini-test",
		Messages: []dto.Message{
			{Role: "user", Content: "hi"},
			assistantMessage,
			{Role: "tool", ToolCallId: "call_1", Content: `{"ok":true}`},
		},
		Tools: []dto.ToolCallRequest{
			{
				Type: "function",
				Function: dto.FunctionRequest{
					Name:       "lookup",
					Parameters: map[string]any{"type": "object"},
				},
			},
		},
	}

	result, err := ConvertRequest(nil, info, types.RelayFormatGemini, req)

	require.NoError(t, err)
	geminiReq, ok := result.Value.(*dto.GeminiChatRequest)
	require.True(t, ok)
	require.Len(t, geminiReq.Contents, 3)
	assert.Equal(t, "model", geminiReq.Contents[1].Role)
	require.Len(t, geminiReq.Contents[1].Parts, 1)
	require.NotNil(t, geminiReq.Contents[1].Parts[0].FunctionCall)
	var thoughtSignature string
	require.NoError(t, kitutil.Unmarshal(geminiReq.Contents[1].Parts[0].ThoughtSignature, &thoughtSignature))
	assert.Equal(t, sharedgemini.ThoughtSignatureBypassValue, thoughtSignature)
}

func TestConvertRequestResponsesToClaudeUsesDirectConverter(t *testing.T) {
	info := &convmeta.Values{
		ConversionChain: []types.RelayFormat{types.RelayFormatOpenAIResponses},
	}
	stream := true
	parallelToolCalls := false
	maxOutputTokens := uint(512)
	req := &dto.OpenAIResponsesRequest{
		Model:             "claude-test",
		Instructions:      mustRawMessage(t, "system rules"),
		Stream:            &stream,
		MaxOutputTokens:   &maxOutputTokens,
		ParallelToolCalls: mustRawMessage(t, parallelToolCalls),
		Reasoning:         &dto.Reasoning{Effort: "medium"},
		Input: mustRawMessage(t, []map[string]any{
			{
				"role":    "user",
				"content": "question",
			},
			{
				"type":    "reasoning",
				"summary": []map[string]any{{"type": "summary_text", "text": "inspect inputs"}},
			},
			{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "I will call."},
				},
			},
			{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "lookup",
				"arguments": map[string]any{"q": "x"},
			},
			{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  map[string]any{"ok": true},
			},
		}),
		Tools: mustRawMessage(t, []map[string]any{
			{
				"type":        "function",
				"name":        "lookup",
				"description": "Lookup data",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"q": map[string]any{"type": "string"},
					},
				},
			},
		}),
	}

	result, err := ConvertRequest(nil, info, types.RelayFormatClaude, req)

	require.NoError(t, err)
	claudeReq, ok := result.Value.(*dto.ClaudeRequest)
	require.True(t, ok)
	assert.Equal(t, requestConverterResponsesToClaude, result.Converter)
	assert.Equal(t, []RequestStep{
		{
			Converter: requestConverterResponsesToClaude,
			From:      types.RelayFormatOpenAIResponses,
			To:        types.RelayFormatClaude,
		},
	}, result.Steps)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatClaude}, info.ConversionChain)

	system, err := kitutil.Any2Type[[]dto.ClaudeMediaMessage](claudeReq.System)
	require.NoError(t, err)
	require.Len(t, system, 1)
	assert.Equal(t, "system rules", system[0].GetText())
	require.NotNil(t, claudeReq.Stream)
	assert.True(t, *claudeReq.Stream)
	assert.Equal(t, maxOutputTokens, *claudeReq.MaxTokens)
	require.NotNil(t, claudeReq.Thinking)
	assert.Equal(t, "disabled", claudeReq.Thinking.Type)

	tools, err := kitutil.Any2Type[[]*dto.Tool](claudeReq.Tools)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "lookup", tools[0].Name)

	require.Len(t, claudeReq.Messages, 3)
	assert.Equal(t, "user", claudeReq.Messages[0].Role)
	userParts, err := claudeReq.Messages[0].ParseContent()
	require.NoError(t, err)
	require.Len(t, userParts, 1)
	assert.Equal(t, "question", userParts[0].GetText())

	assert.Equal(t, "assistant", claudeReq.Messages[1].Role)
	assistantParts, err := claudeReq.Messages[1].ParseContent()
	require.NoError(t, err)
	require.Len(t, assistantParts, 2)
	assert.Equal(t, "I will call.", assistantParts[0].GetText())
	assert.Equal(t, "tool_use", assistantParts[1].Type)
	assert.Equal(t, "call_1", assistantParts[1].Id)
	assert.Equal(t, "lookup", assistantParts[1].Name)
	assert.Equal(t, map[string]any{"q": "x"}, assistantParts[1].Input)
	for _, part := range assistantParts {
		assert.NotEqual(t, "thinking", part.Type)
		assert.Empty(t, part.Signature)
	}
	encodedClaudeRequest, err := kitutil.Marshal(claudeReq)
	require.NoError(t, err)
	assert.NotContains(t, string(encodedClaudeRequest), "encrypted_content")
	assert.NotContains(t, string(encodedClaudeRequest), "opaque-openai-state")

	assert.Equal(t, "user", claudeReq.Messages[2].Role)
	toolResultParts, err := claudeReq.Messages[2].ParseContent()
	require.NoError(t, err)
	require.Len(t, toolResultParts, 1)
	assert.Equal(t, "tool_result", toolResultParts[0].Type)
	assert.Equal(t, "call_1", toolResultParts[0].ToolUseId)
	toolResultJSON, ok := toolResultParts[0].Content.(string)
	require.True(t, ok)
	assert.JSONEq(t, `{"ok":true}`, toolResultJSON)
}

func TestConvertRequestViaResponsesToGeminiStillUsesDirectSteps(t *testing.T) {
	info := &convmeta.Values{
		ConversionChain:     []types.RelayFormat{types.RelayFormatOpenAIResponses},
		ChannelMetaAttached: true,
		UpstreamModelName:   "gemini-test",
	}
	req := &dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"role":    "user",
				"content": "hello",
			},
		}),
	}

	result, err := ConvertRequestVia(nil, info, req, types.RelayFormatOpenAI, types.RelayFormatGemini)

	require.NoError(t, err)
	require.IsType(t, &dto.GeminiChatRequest{}, result.Value)
	assert.Equal(t, ConverterOpenAIResponsesToOpenAIChat+","+ConverterOpenAIChatToGeminiContent, result.Converter)
	assert.Equal(t, []RequestStep{
		{
			Converter: ConverterOpenAIResponsesToOpenAIChat,
			From:      types.RelayFormatOpenAIResponses,
			To:        types.RelayFormatOpenAI,
		},
		{
			Converter: ConverterOpenAIChatToGeminiContent,
			From:      types.RelayFormatOpenAI,
			To:        types.RelayFormatGemini,
		},
	}, result.Steps)
}

func TestConvertRequestByIDDeduplicatesConversionChain(t *testing.T) {
	info := &convmeta.Values{
		ConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses},
	}
	req := &dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		Messages: []dto.Message{
			{Role: "user", Content: "hello"},
		},
	}

	result, err := ConvertRequestByID(nil, info, ConverterOpenAIChatToOpenAIResponses, req)

	require.NoError(t, err)
	require.IsType(t, &dto.OpenAIResponsesRequest{}, result.Value)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses}, info.ConversionChain)
}

func TestConvertRequestByIDExecutesDirectClaudeToResponsesConverter(t *testing.T) {
	info := &convmeta.Values{
		ConversionChain: []types.RelayFormat{types.RelayFormatClaude},
	}
	req := &dto.ClaudeRequest{
		Model: "claude-test",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
	}

	result, err := ConvertRequestByID(nil, info, requestConverterClaudeToResponses, req)

	require.NoError(t, err)
	require.IsType(t, &dto.OpenAIResponsesRequest{}, result.Value)
	assert.Equal(t, requestConverterClaudeToResponses, result.Converter)
	assert.Equal(t, RequestConverterQualityFair, result.Quality)
	assert.Equal(t, []RequestStep{
		{
			Converter: requestConverterClaudeToResponses,
			From:      types.RelayFormatClaude,
			To:        types.RelayFormatOpenAIResponses,
		},
	}, result.Steps)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses}, info.ConversionChain)
}

func TestConvertRequestRejectsUnsupportedConverterAndNilRequest(t *testing.T) {
	_, err := ConvertRequestByID(nil, &convmeta.Values{}, "missing_converter", &dto.GeneralOpenAIRequest{Model: "gpt-test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")

	_, err = ConvertRequest(nil, &convmeta.Values{}, types.RelayFormatOpenAIResponses, (*dto.GeneralOpenAIRequest)(nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestConvertRequestByIDRejectsWrongSourceFormat(t *testing.T) {
	_, err := ConvertRequestByID(
		nil,
		&convmeta.Values{},
		ConverterOpenAIChatToOpenAIResponses,
		&dto.ClaudeRequest{Model: "claude-test"},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expects openai request")
}

func TestConvertRequestRejectsUnregisteredExplicitPath(t *testing.T) {
	_, err := ConvertRequest(
		nil,
		&convmeta.Values{},
		types.RelayFormatEmbedding,
		&dto.ClaudeRequest{Model: "claude-test"},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "from claude to embedding is not registered")
}

func TestConvertRequestClaudeToGeminiKeepsToolResultMediaWithFunctionResponse(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"gemini-3.6-flash",
		"messages":[
			{"role":"assistant","content":[{
				"type":"tool_use",
				"id":"call_image",
				"name":"inspect",
				"input":{}
			}]},
			{"role":"user","content":[{
				"type":"tool_result",
				"tool_use_id":"call_image",
				"content":[
					{"type":"text","text":"caption"},
					{"type":"image","source":{"type":"base64","media_type":"image/png","data":"GEMINI_TOOL_IMAGE_SENTINEL"}}
				]
			}]}
		]
	}`), &request))

	result, err := ConvertRequest(nil, &convmeta.Values{
		ChannelMetaAttached: true,
		UpstreamModelName:   "gemini-3.6-flash",
	}, types.RelayFormatGemini, &request)
	require.NoError(t, err)
	converted := result.Value.(*dto.GeminiChatRequest)
	require.Len(t, converted.Contents, 2)
	require.Len(t, converted.Contents[1].Parts, 1)
	require.NotNil(t, converted.Contents[1].Parts[0].FunctionResponse)
	functionResponse := converted.Contents[1].Parts[0].FunctionResponse
	assert.Contains(t, functionResponse.Response["content"], "tool result media attached")
	var mediaParts []dto.GeminiPart
	require.NoError(t, kitutil.Unmarshal(functionResponse.Parts, &mediaParts))
	require.Len(t, mediaParts, 1)
	require.NotNil(t, mediaParts[0].InlineData)
	assert.Equal(t, "image/png", mediaParts[0].InlineData.MimeType)
	assert.Equal(t, "aGVsbG8=", mediaParts[0].InlineData.Data)
}

func TestConvertRequestClaudeToGeminiKeepsRemoteToolImageInLegacyResponse(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"gemini-3.6-flash",
		"messages":[
			{"role":"assistant","content":[{
				"type":"tool_use",
				"id":"call_remote",
				"name":"inspect",
				"input":{}
			}]},
			{"role":"user","content":[{
				"type":"tool_result",
				"tool_use_id":"call_remote",
				"content":[{
					"type":"image",
					"source":{"type":"url","url":"https://example.test/tool-image.png"}
				}]
			}]}
		]
	}`), &request))

	result, err := ConvertRequest(nil, &convmeta.Values{
		ChannelMetaAttached: true,
		UpstreamModelName:   "gemini-3.6-flash",
	}, types.RelayFormatGemini, &request)
	require.NoError(t, err)
	converted := result.Value.(*dto.GeminiChatRequest)
	require.Len(t, converted.Contents, 2)
	require.Len(t, converted.Contents[1].Parts, 1)
	functionResponse := converted.Contents[1].Parts[0].FunctionResponse
	require.NotNil(t, functionResponse)
	assert.Empty(t, functionResponse.Parts)
	content, ok := functionResponse.Response["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	image, ok := content[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "https://example.test/tool-image.png", image["source"].(map[string]any)["url"])
}

func TestConvertRequestClaudeToGemini2KeepsToolMediaAdjacentToFunctionResponse(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"gemini-2.5-pro",
		"messages":[
			{"role":"assistant","content":[{
				"type":"tool_use",
				"id":"call_image",
				"name":"inspect",
				"input":{}
			}]},
			{"role":"user","content":[{
				"type":"tool_result",
				"tool_use_id":"call_image",
				"content":[{
					"type":"image",
					"source":{"type":"base64","media_type":"image/png","data":"GEMINI_2_IMAGE_SENTINEL"}
				}]
			}]}
		]
	}`), &request))

	result, err := ConvertRequest(nil, &convmeta.Values{
		ChannelMetaAttached: true,
		UpstreamModelName:   "gemini-2.5-pro",
	}, types.RelayFormatGemini, &request)
	require.NoError(t, err)
	converted := result.Value.(*dto.GeminiChatRequest)
	require.Len(t, converted.Contents, 2)
	require.Len(t, converted.Contents[1].Parts, 3)
	functionResponse := converted.Contents[1].Parts[0].FunctionResponse
	require.NotNil(t, functionResponse)
	assert.Contains(t, functionResponse.Response["content"], "tool result media attached")
	assert.Equal(t, "[new-api: media output of tool call call_image]", converted.Contents[1].Parts[1].Text)
	require.NotNil(t, converted.Contents[1].Parts[2].InlineData)
	assert.Equal(t, "image/png", converted.Contents[1].Parts[2].InlineData.MimeType)
	assert.Equal(t, "aGVsbG8=", converted.Contents[1].Parts[2].InlineData.Data)
}

func mustRawMessage(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := kitutil.Marshal(value)
	require.NoError(t, err)
	return raw
}

func inputContentText(t *testing.T, item map[string]any) string {
	t.Helper()
	content, ok := item["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	part, ok := content[0].(map[string]any)
	require.True(t, ok)
	text, ok := part["text"].(string)
	require.True(t, ok)
	return text
}
