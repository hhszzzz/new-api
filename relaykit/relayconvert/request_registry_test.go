package relayconvert

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	oairesponses "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/oai_responses"
	sharedgemini "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/gemini"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStructuredOutputAndGenerationParametersAcrossProtocols(t *testing.T) {
	const schema = `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`
	for _, source := range []Protocol{ProtocolChat, ProtocolResponses, ProtocolMessages, ProtocolGemini} {
		for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude, types.RelayFormatGemini} {
			if ProtocolForFormat(target) == source {
				continue
			}
			t.Run(string(source)+"_to_"+string(target), func(t *testing.T) {
				var request any
				switch source {
				case ProtocolChat:
					request = &dto.GeneralOpenAIRequest{Model: "test", MaxTokens: kitutil.GetPointer(uint(64)), Messages: []dto.Message{{Role: "user", Content: "question"}}, ResponseFormat: &dto.ResponseFormat{Type: "json_schema", JsonSchema: []byte(`{"name":"answer","strict":true,"schema":` + schema + `}`)}}
				case ProtocolResponses:
					request = &dto.OpenAIResponsesRequest{Model: "test", MaxOutputTokens: kitutil.GetPointer(uint(64)), Input: []byte(`"question"`), Text: []byte(`{"format":{"type":"json_schema","name":"answer","strict":true,"schema":` + schema + `}}`)}
				case ProtocolMessages:
					request = &dto.ClaudeRequest{Model: "test", MaxTokens: kitutil.GetPointer(uint(64)), Messages: []dto.ClaudeMessage{{Role: "user", Content: "question"}}, OutputConfig: []byte(`{"format":{"type":"json_schema","schema":` + schema + `}}`)}
				case ProtocolGemini:
					request = &dto.GeminiChatRequest{Contents: []dto.GeminiChatContent{{Role: "user", Parts: []dto.GeminiPart{{Text: "question"}}}}, GenerationConfig: dto.GeminiChatGenerationConfig{MaxOutputTokens: kitutil.GetPointer(uint(64)), ResponseMimeType: "application/json", ResponseJsonSchema: []byte(schema)}}
				}
				session := NewConversionSession(&convmeta.Values{ChannelMetaAttached: true, UpstreamModelName: "test"})
				defer session.Close()
				result, err := session.Request(nil, target, request)
				require.NoError(t, err)
				raw, err := kitutil.Marshal(result.Value)
				require.NoError(t, err)
				assert.Contains(t, string(raw), `"additionalProperties":false`)
				assert.Contains(t, string(raw), `"required":["answer"]`)
			})
		}
	}
	for _, field := range []string{"output_config", "output_format"} {
		var request dto.ClaudeRequest
		format := `{"type":"json_schema","schema":` + schema + `}`
		if field == "output_config" {
			format = `{"format":` + format + `}`
		}
		require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","service_tier":"standard_only","`+field+`":`+format+`}`), &request))
		result, err := NewConversionSession(nil).Request(nil, types.RelayFormatOpenAIResponses, &request)
		require.NoError(t, err)
		assert.Equal(t, "default", result.Value.(*dto.OpenAIResponsesRequest).ServiceTier)
	}
	for _, value := range []int{0, 20} {
		request := &dto.ClaudeRequest{Model: "test", TopK: &value, MaxTokens: kitutil.GetPointer(uint(64)), Messages: []dto.ClaudeMessage{{Role: "user", Content: "question"}}}
		gemini, err := NewConversionSession(nil).Request(nil, types.RelayFormatGemini, request)
		require.NoError(t, err)
		assert.Equal(t, float64(value), *gemini.Value.(*dto.GeminiChatRequest).GenerationConfig.TopK)
		messages, err := NewConversionSession(&convmeta.Values{ChannelMetaAttached: true, UpstreamModelName: "test"}).Request(nil, types.RelayFormatClaude, gemini.Value)
		require.NoError(t, err)
		assert.Equal(t, value, *messages.Value.(*dto.ClaudeRequest).TopK)
	}
	request := &dto.GeneralOpenAIRequest{Model: "test", LogProbs: kitutil.GetPointer(true), TopLogProbs: kitutil.GetPointer(0), Verbosity: []byte(`"low"`)}
	converted, err := NewConversionSession(nil).Request(nil, types.RelayFormatOpenAIResponses, request)
	require.NoError(t, err)
	responses := converted.Value.(*dto.OpenAIResponsesRequest)
	assert.JSONEq(t, `["message.output_text.logprobs"]`, string(responses.Include))
	assert.JSONEq(t, `{"verbosity":"low"}`, string(responses.Text))
	back, err := NewConversionSession(nil).Request(nil, types.RelayFormatOpenAI, responses)
	require.NoError(t, err)
	chat := back.Value.(*dto.GeneralOpenAIRequest)
	assert.True(t, *chat.LogProbs)
	assert.Zero(t, *chat.TopLogProbs)
	assert.JSONEq(t, `"low"`, string(chat.Verbosity))
}

func TestGatewayContextEditsAndTuningLoss(t *testing.T) {
	request := &dto.ClaudeRequest{Model: "test", TopK: kitutil.GetPointer(0), Speed: []byte(`"fast"`), MaxTokens: kitutil.GetPointer(uint(64)), ContextManagement: []byte(`{"edits":[{"type":"clear_thinking_20251015","keep":{"type":"thinking_turns","value":1}},{"type":"clear_tool_uses_20250919","trigger":{"type":"tool_uses","value":1},"keep":{"type":"tool_uses","value":1},"exclude_tools":["protected"]}]}`)}
	request.Messages = []dto.ClaudeMessage{
		{Role: "user", Content: "question"},
		{Role: "assistant", Content: []dto.ClaudeMediaMessage{{Type: "thinking", Thinking: kitutil.GetPointer("old thought")}, {Type: "tool_use", Id: "old", Name: "read", Input: map[string]any{"path": "a"}}}},
		{Role: "user", Content: []dto.ClaudeMediaMessage{{Type: "tool_result", ToolUseId: "old", Content: "old secret result"}}},
		{Role: "assistant", Content: []dto.ClaudeMediaMessage{{Type: "tool_use", Id: "protected", Name: "protected", Input: map[string]any{}}}},
		{Role: "user", Content: []dto.ClaudeMediaMessage{{Type: "tool_result", ToolUseId: "protected", Content: "keep protected"}}},
		{Role: "assistant", Content: []dto.ClaudeMediaMessage{{Type: "thinking", Thinking: kitutil.GetPointer("recent thought")}, {Type: "tool_use", Id: "recent", Name: "read", Input: map[string]any{}}}},
		{Role: "user", Content: []dto.ClaudeMediaMessage{{Type: "tool_result", ToolUseId: "recent", Content: "keep recent"}}},
	}
	original, err := kitutil.Marshal(request)
	require.NoError(t, err)
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
		for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyLossy} {
			session := NewConversionSession(&convmeta.Values{Options: &convmeta.Options{ToolLossPolicy: policy, AllowDirectiveDrop: policy == types.ConversionLossPolicyLossy}})
			result, err := session.Request(nil, target, request)
			if policy != types.ConversionLossPolicyLossy {
				require.Error(t, err)
				continue
			}
			require.NoError(t, err)
			raw, err := kitutil.Marshal(result.Value)
			require.NoError(t, err)
			assert.NotContains(t, string(raw), "old secret result")
			assert.Contains(t, string(raw), "keep protected")
			assert.Contains(t, string(raw), "keep recent")
			assert.NotContains(t, string(raw), "context_management\":")
			assert.Contains(t, result.Diagnostics, types.ConversionDiagnostic{Code: "context_management_emulated", LossClass: types.ConversionLossTuning, Path: "context_management", Severity: types.ConversionDiagnosticWarning, From: types.RelayFormatClaude, To: target, Message: "gateway cleared 1 tool results and 1 thinking turns; input-token thresholds and savings use estimates, upstream usage remains authoritative"})
		}
	}
	after, err := kitutil.Marshal(request)
	require.NoError(t, err)
	assert.Equal(t, string(original), string(after))
	copy := *request
	copy.ContextManagement = []byte(`{"edits":[{"type":"unknown_edit"}]}`)
	result, err := NewConversionSession(&convmeta.Values{Options: &convmeta.Options{ToolLossPolicy: types.ConversionLossPolicyLossy, AllowDirectiveDrop: true}}).Request(nil, types.RelayFormatOpenAIResponses, &copy)
	require.NoError(t, err)
	assert.Equal(t, "context_management_omitted", result.Diagnostics[0].Code)
}

func TestStopEmulationDrainsResponsesAndPreservesUsage(t *testing.T) {
	const output = "visible<END>hidden billable output"
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
		for _, streaming := range []bool{false, true} {
			t.Run(string(target)+"_"+map[bool]string{false: "full", true: "stream"}[streaming], func(t *testing.T) {
				var request any
				switch target {
				case types.RelayFormatOpenAI:
					request = &dto.GeneralOpenAIRequest{Model: "test", Stop: []string{"<END>"}}
				case types.RelayFormatClaude:
					request = &dto.ClaudeRequest{Model: "test", StopSequences: []string{"<END>"}}
				case types.RelayFormatGemini:
					request = &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{StopSequences: []string{"<END>"}}}
				}
				_, err := NewConversionSession(nil).Request(nil, types.RelayFormatOpenAIResponses, request)
				require.ErrorContains(t, err, "stop")
				session := NewConversionSession(&convmeta.Values{ChannelMetaAttached: true, UpstreamModelName: "test", Options: &convmeta.Options{ToolLossPolicy: types.ConversionLossPolicyLossy, AllowDirectiveDrop: true}})
				defer session.Close()
				converted, err := session.Request(nil, types.RelayFormatOpenAIResponses, request)
				require.NoError(t, err)
				assert.Empty(t, converted.Value.(*dto.OpenAIResponsesRequest).Stop)
				upstream := &dto.OpenAIResponsesResponse{ID: "resp_test", Model: "test", Status: []byte(`"completed"`), Output: []dto.ResponsesOutput{{Type: "message", ID: "msg_test", Role: "assistant", Content: []dto.ResponsesOutputContent{{Type: "output_text", Text: output}}}}, Usage: &dto.Usage{InputTokens: 17, OutputTokens: 31, TotalTokens: 48}}
				var results []ResponseResult
				var usage *dto.Usage
				if !streaming {
					result, err := session.Response(nil, target, upstream)
					require.NoError(t, err)
					results = []ResponseResult{*result}
					usage = result.Usage
				} else {
					state, err := session.StreamState(types.RelayFormatOpenAIResponses, target, ResponseStreamOptions{IncludeUsage: true})
					require.NoError(t, err)
					events := []*dto.ResponsesStreamResponse{
						{Type: "response.created", Response: &dto.OpenAIResponsesResponse{ID: "resp_test", Model: "test", Status: []byte(`"in_progress"`)}},
						{Type: "response.output_item.added", OutputIndex: kitutil.GetPointer(0), Item: &dto.ResponsesOutput{Type: "message", ID: "msg_test", Role: "assistant"}},
						{Type: "response.output_text.delta", ItemID: "msg_test", OutputIndex: kitutil.GetPointer(0), ContentIndex: kitutil.GetPointer(0), Delta: "visible<EN"},
						{Type: "response.output_text.delta", ItemID: "msg_test", OutputIndex: kitutil.GetPointer(0), ContentIndex: kitutil.GetPointer(0), Delta: "D>hidden billable output"},
						{Type: "response.output_text.done", ItemID: "msg_test", OutputIndex: kitutil.GetPointer(0), ContentIndex: kitutil.GetPointer(0), Text: kitutil.GetPointer(output)},
						{Type: "response.output_item.done", OutputIndex: kitutil.GetPointer(0), Item: &upstream.Output[0]},
						{Type: "response.completed", Response: upstream},
					}
					for _, event := range events {
						chunks, err := session.Stream(nil, state, event)
						require.NoError(t, err)
						results = append(results, chunks...)
					}
					chunks, err := session.Finish(nil, state)
					require.NoError(t, err)
					results = append(results, chunks...)
					usage = state.Usage()
				}
				require.NotNil(t, usage)
				assert.Equal(t, 31, usage.CompletionTokens)
				assert.Equal(t, 17, usage.PromptTokens)
				require.NotNil(t, usage.BillingUsage)
				var visible strings.Builder
				for _, result := range results {
					raw, err := kitutil.Marshal(result.Value)
					require.NoError(t, err)
					assert.NotContains(t, string(raw), "hidden")
					assert.NotContains(t, string(raw), "<END>")
					switch response := result.Value.(type) {
					case *dto.OpenAITextResponse:
						visible.WriteString(response.Choices[0].Message.StringContent())
						assert.Equal(t, "stop", response.Choices[0].FinishReason)
					case *dto.ChatCompletionsStreamResponse:
						for _, choice := range response.Choices {
							visible.WriteString(choice.Delta.GetContentString())
							if choice.FinishReason != nil {
								assert.Equal(t, "stop", *choice.FinishReason)
							}
						}
					case *dto.ClaudeResponse:
						if streaming {
							if response.Delta != nil {
								visible.WriteString(response.Delta.GetText())
								if response.Type == "message_delta" {
									require.NotNil(t, response.Delta.StopSequence)
									assert.Equal(t, "<END>", *response.Delta.StopSequence)
								}
							}
						} else {
							for _, block := range response.Content {
								visible.WriteString(block.GetText())
							}
							assert.Equal(t, "stop_sequence", response.StopReason)
						}
					case *dto.GeminiChatResponse:
						for _, candidate := range response.Candidates {
							for _, part := range candidate.Content.Parts {
								if !part.Thought {
									visible.WriteString(part.Text)
								}
							}
						}
					}
				}
				assert.Equal(t, "visible", visible.String())
				assert.Equal(t, output, upstream.Output[0].Content[0].Text)
			})
		}
	}
}

func TestLogprobsSurviveChatResponsesRoundTrip(t *testing.T) {
	probabilities := []dto.TokenLogprob{{Token: "hi", Logprob: -0.25, Bytes: []int{104, 105}, TopLogprobs: []dto.TokenLogprob{{Token: "hi", Logprob: -0.25, Bytes: []int{104, 105}}}}}
	var chatProbabilities any = dto.ChatLogprobs{Content: probabilities}
	chat := &dto.OpenAITextResponse{Id: "chat_1", Model: "test", Choices: []dto.OpenAITextResponseChoice{{Message: dto.Message{Role: "assistant", Content: "hi"}, FinishReason: "stop", Logprobs: &chatProbabilities}}}
	converted, err := ConvertResponse(nil, nil, types.RelayFormatOpenAIResponses, chat)
	require.NoError(t, err)
	responses := converted.Value.(*dto.OpenAIResponsesResponse)
	require.Len(t, responses.Output, 1)
	assert.Equal(t, probabilities, responses.Output[0].Content[0].Logprobs)
	back, err := ConvertResponse(nil, nil, types.RelayFormatOpenAI, responses)
	require.NoError(t, err)
	assert.Equal(t, chat.Choices[0].Logprobs, back.Value.(*dto.OpenAITextResponse).Choices[0].Logprobs)
	session := NewConversionSession(nil)
	defer session.Close()
	state, err := session.StreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{})
	require.NoError(t, err)
	var events []ResponseResult
	for _, chunk := range []*dto.ChatCompletionsStreamResponse{
		{Id: "chat_1", Model: "test", Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: kitutil.GetPointer("hi")}, Logprobs: &chatProbabilities}}},
		{Id: "chat_1", Model: "test", Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: kitutil.GetPointer("stop")}}},
	} {
		result, err := session.Stream(nil, state, chunk)
		require.NoError(t, err)
		events = append(events, result...)
	}
	final, err := session.Finish(nil, state)
	require.NoError(t, err)
	events = append(events, final...)
	decoder := NewConversionSession(nil)
	defer decoder.Close()
	decodeState, err := decoder.StreamState(types.RelayFormatOpenAIResponses, types.RelayFormatOpenAI, ResponseStreamOptions{})
	require.NoError(t, err)
	var roundtrip []dto.TokenLogprob
	for _, result := range events {
		payload := result.Value.(ChatToResponsesStreamEvent).Payload
		event := &payload
		if event.Type == "response.output_text.delta" {
			assert.Equal(t, probabilities, event.Logprobs)
		}
		if event.Type == "response.completed" {
			assert.Equal(t, probabilities, event.Response.Output[0].Content[0].Logprobs)
		}
		chunks, err := decoder.Stream(nil, decodeState, event)
		require.NoError(t, err)
		for _, chunk := range chunks {
			response, ok := chunk.Value.(*dto.ChatCompletionsStreamResponse)
			if !ok {
				value := chunk.Value.(dto.ChatCompletionsStreamResponse)
				response = &value
			}
			for _, choice := range response.Choices {
				if choice.Logprobs != nil {
					decoded, err := kitutil.Any2Type[dto.ChatLogprobs](*choice.Logprobs)
					require.NoError(t, err)
					roundtrip = append(roundtrip, decoded.Content...)
				}
			}
		}
	}
	assert.Equal(t, probabilities, roundtrip, "delta and completed snapshots must not duplicate logprobs")
}

func TestStopEmulationFlushesUnmatchedPrefixAndHidesToolCalls(t *testing.T) {
	for _, test := range []struct {
		chunks []string
		want   string
	}{
		{[]string{"text<EN"}, "text<EN"},
		{[]string{"中", "文<", "END>", "later"}, "中文"},
		{[]string{"text<EN", "X"}, "text<ENX"},
	} {
		stop, err := newStopEmulator(ProtocolChat, []byte(`{"stop":["<END>"]}`))
		require.NoError(t, err)
		var visible strings.Builder
		for _, chunk := range test.chunks {
			visible.WriteString(stop.text(chunk, false))
		}
		visible.WriteString(stop.text("", true))
		assert.Equal(t, test.want, visible.String())
	}
	stop, err := newStopEmulator(ProtocolChat, []byte(`{"stop":["END"]}`))
	require.NoError(t, err)
	var probabilities any = dto.ChatLogprobs{Content: []dto.TokenLogprob{{Token: "safe", Bytes: []int{115, 97, 102, 101}}, {Token: "ENDsecret", Bytes: []int{69, 78, 68, 115, 101, 99, 114, 101, 116}}}}
	results, err := stop.filter(&dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: kitutil.GetPointer("safeENDsecret")}, Logprobs: &probabilities}}}, true)
	require.NoError(t, err)
	raw, err := kitutil.Marshal(results)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "safe")
	assert.NotContains(t, string(raw), "secret")
	results, err = stop.filter(&dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{ID: "do-not-run"}}}, FinishReason: kitutil.GetPointer("tool_calls")}}}, true)
	require.NoError(t, err)
	response := results[0].(*dto.ChatCompletionsStreamResponse)
	assert.Empty(t, response.Choices[0].Delta.ToolCalls)
	assert.Equal(t, "stop", *response.Choices[0].FinishReason)

	t.Run("missing earlier logprobs cannot expose stopped tokens", func(t *testing.T) {
		stop, err := newStopEmulator(ProtocolChat, []byte(`{"stop":["END"]}`))
		require.NoError(t, err)
		_, err = stop.filter(&dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: kitutil.GetPointer("earlier visible output")}}}}, true)
		require.NoError(t, err)
		var probabilities any = dto.ChatLogprobs{Content: []dto.TokenLogprob{{Token: "ENDhidden"}}}
		results, err := stop.filter(&dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: kitutil.GetPointer("ENDhidden")}, Logprobs: &probabilities}}}, true)
		require.NoError(t, err)
		raw, err := kitutil.Marshal(results)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), "hidden")
	})
	t.Run("pending Gemini text precedes a tool call", func(t *testing.T) {
		stop, err := newStopEmulator(ProtocolGemini, []byte(`{"generationConfig":{"stopSequences":["END"]}}`))
		require.NoError(t, err)
		assert.Equal(t, "safe", stop.text("safeE", false))
		input := &dto.GeminiChatResponse{}
		require.NoError(t, kitutil.Unmarshal([]byte(`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"read","args":{}}}]}}]}`), input))
		results, err := stop.filter(input, true)
		require.NoError(t, err)
		parts := results[0].(*dto.GeminiChatResponse).Candidates[0].Content.Parts
		require.Len(t, parts, 2)
		assert.Equal(t, "E", parts[0].Text)
		assert.NotNil(t, parts[1].FunctionCall)
	})
}

func TestContextEditThresholdsAndDefaults(t *testing.T) {
	for _, test := range []struct {
		name, edit      string
		estimate        int
		cleared, inputs bool
	}{
		{"below default threshold", `{"type":"clear_tool_uses_20250919","keep":{"type":"tool_uses","value":0}}`, 100000, false, false},
		{"above threshold", `{"type":"clear_tool_uses_20250919","keep":{"type":"tool_uses","value":0}}`, 100001, true, false},
		{"minimum savings not met", `{"type":"clear_tool_uses_20250919","keep":{"type":"tool_uses","value":0},"clear_at_least":{"type":"input_tokens","value":99999}}`, 100001, false, false},
		{"clear input and result", `{"type":"clear_tool_uses_20250919","keep":{"type":"tool_uses","value":0},"clear_at_least":{"type":"input_tokens","value":10},"clear_tool_inputs":true}`, 100001, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := &dto.ClaudeRequest{Model: "claude-sonnet-4-6", ContextManagement: []byte(`{"edits":[` + test.edit + `]}`), Messages: []dto.ClaudeMessage{
				{Role: "assistant", Content: []dto.ClaudeMediaMessage{{Type: "tool_use", Id: "call_1", Name: "read", Input: map[string]any{"path": "input-kept"}}}},
				{Role: "user", Content: []dto.ClaudeMediaMessage{{Type: "tool_result", ToolUseId: "call_1", Content: strings.Repeat("result-kept ", 100)}}},
			}}
			edited, _, err := applyMessagesContextManagement(request, &convmeta.Values{EstimatePromptTokens: test.estimate})
			require.NoError(t, err)
			raw, err := kitutil.Marshal(edited)
			require.NoError(t, err)
			assert.Equal(t, !test.cleared, strings.Contains(string(raw), "result-kept"))
			assert.Equal(t, !test.inputs, strings.Contains(string(raw), "input-kept"))
		})
	}
	for _, model := range []string{"claude-opus-4.5", "anthropic/claude-sonnet-4.6", "claude-sonnet-4-7", "claude-opus-5"} {
		keep, err := defaultThinkingKeep(model)
		require.NoError(t, err)
		assert.Equal(t, "all", keep, model)
	}
	_, err := defaultThinkingKeep("unknown-route")
	require.Error(t, err)
}

func TestResponsesLogprobsSurviveMissingTerminalOutput(t *testing.T) {
	probabilities := []dto.TokenLogprob{{Token: "hello", Logprob: -0.1}}
	accumulator := oairesponses.NewResponsesBufferedAccumulator()
	accumulator.ProcessEvent(&dto.ResponsesStreamResponse{Type: "response.output_text.delta", OutputIndex: kitutil.GetPointer(0), ContentIndex: kitutil.GetPointer(0), Delta: "hello", Logprobs: probabilities})
	terminal := &dto.OpenAIResponsesResponse{}
	accumulator.SupplementResponseOutput(terminal)
	require.Len(t, terminal.Output, 1)
	assert.Equal(t, probabilities, terminal.Output[0].Content[0].Logprobs)
	state := oairesponses.NewResponsesToChatStreamState("test", false)
	chunks, err := oairesponses.ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{Type: "response.output_item.done", OutputIndex: kitutil.GetPointer(0), Item: &terminal.Output[0]}, state)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	require.NotNil(t, chunks[len(chunks)-1].Choices[0].Logprobs)
	parsed, err := kitutil.Any2Type[dto.ChatLogprobs](*chunks[len(chunks)-1].Choices[0].Logprobs)
	require.NoError(t, err)
	assert.Equal(t, probabilities, parsed.Content)
}

func TestStructuredOutputNeverSilentlyWeakensConstraints(t *testing.T) {
	for _, constraint := range []string{`"minimum":1`, `"maxLength":10`, `"oneOf":[{"type":"string"}]`, `"$ref":"https://example.com/schema"`, `"pattern":"(?=secret)"`} {
		request := &dto.GeneralOpenAIRequest{Model: "test", MaxTokens: kitutil.GetPointer(uint(64)), ResponseFormat: &dto.ResponseFormat{Type: "json_schema", JsonSchema: []byte(`{"name":"answer","schema":{"type":"object","additionalProperties":false,"properties":{"answer":{"type":"string",` + constraint + `}},"required":["answer"]}}`)}}
		_, err := NewConversionSession(nil).Request(nil, types.RelayFormatClaude, request)
		require.Error(t, err)
	}
	request := &dto.ClaudeRequest{Model: "test", OutputConfig: []byte(`{"format":{"type":"json_schema","schema":{"type":"object","additionalProperties":false,"properties":{"optional":{"type":"string"}}}}}`)}
	_, err := NewConversionSession(nil).Request(nil, types.RelayFormatOpenAI, request)
	require.ErrorContains(t, err, "optional property")
	request.OutputConfig = []byte(`{"format":{"type":"json_schema","schema":{"type":"object","additionalProperties":false,"properties":{},"$defs":{"loop":{"$ref":"#/$defs/loop"}},"$ref":"#/$defs/loop"}}}`)
	_, err = NewConversionSession(nil).Request(nil, types.RelayFormatOpenAI, request)
	require.ErrorContains(t, err, "recursive")
}

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
		{"gemini schema", ProtocolGemini, ProtocolChat, `{"generationConfig":{"responseMimeType":"application/json","responseSchema":{"type":"OBJECT","required":["answer"]}}}`, "additionalProperties"},
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
