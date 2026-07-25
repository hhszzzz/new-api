package ollama

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOllamaChatHandlerNonStreamToolCalls(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "compact json per-line parse path",
			raw:  `{"model":"llama3.1","created_at":"2026-05-27T12:00:00Z","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"get_weather","arguments":{"city":"Paris","days":0}}}]},"done":true,"done_reason":"stop","prompt_eval_count":5,"eval_count":7}`,
		},
		{
			name: "pretty json fallback parse path",
			raw: `{
  "model": "llama3.1",
  "created_at": "2026-05-27T12:00:00Z",
  "message": {
    "role": "assistant",
    "content": "",
    "tool_calls": [
      {
        "function": {
          "name": "get_weather",
          "arguments": {
            "city": "Paris",
            "days": 0
          }
        }
      }
    ]
  },
  "done": true,
  "done_reason": "stop",
  "prompt_eval_count": 5,
  "eval_count": 7
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(tt.raw)),
			}

			usage, apiErr := ollamaChatHandler(c, &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "fallback-model"},
			}, resp)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, 12, usage.TotalTokens)

			var out dto.OpenAITextResponse
			require.NoError(t, common.Unmarshal(w.Body.Bytes(), &out))
			require.Len(t, out.Choices, 1)
			assert.Equal(t, constant.FinishReasonToolCalls, out.Choices[0].FinishReason)

			var toolCalls []dto.ToolCallResponse
			require.NoError(t, common.Unmarshal(out.Choices[0].Message.ToolCalls, &toolCalls))
			require.Len(t, toolCalls, 1)
			assert.NotEmpty(t, toolCalls[0].ID)
			assert.Equal(t, "function", toolCalls[0].Type)
			assert.Equal(t, "get_weather", toolCalls[0].Function.Name)
			assert.Nil(t, toolCalls[0].Index)

			var args map[string]any
			require.NoError(t, common.Unmarshal([]byte(toolCalls[0].Function.Arguments), &args))
			assert.Equal(t, "Paris", args["city"])
			assert.Equal(t, float64(0), args["days"])
		})
	}
}

func TestOllamaHandlersHideUserModelRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		publicModel = "gpt-5.4"
		targetModel = "gpt-5.5"
		finalModel  = "provider/gpt-5.5"
	)
	newInfo := func() *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			OriginModelName:      publicModel,
			UserModelRouteId:     7,
			RouteTargetModelName: targetModel,
			ChannelMeta: &relaycommon.ChannelMeta{
				UpstreamModelName: finalModel,
			},
		}
	}

	t.Run("non-stream", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		response := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"model":"provider/gpt-5.5","created_at":"2026-05-27T12:00:00Z","message":{"role":"assistant","content":"done"},"done":true,"prompt_eval_count":2,"eval_count":3}`,
			)),
		}

		usage, apiErr := ollamaChatHandler(context, newInfo(), response)

		require.Nil(t, apiErr)
		require.Equal(t, 5, usage.TotalTokens)
		var output dto.OpenAITextResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &output))
		assert.Equal(t, publicModel, output.Model)
		assert.NotContains(t, recorder.Body.String(), targetModel)
		assert.NotContains(t, recorder.Body.String(), finalModel)
	})

	t.Run("stream", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		body := strings.Join([]string{
			`{"model":"provider/gpt-5.5","created_at":"2026-05-27T12:00:00Z","message":{"role":"assistant","content":"done"},"done":false}`,
			`{"model":"provider/gpt-5.5","created_at":"2026-05-27T12:00:01Z","done":true,"prompt_eval_count":2,"eval_count":3}`,
		}, "\n")
		response := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}

		usage, apiErr := ollamaStreamHandler(context, newInfo(), response)

		require.Nil(t, apiErr)
		require.Equal(t, 5, usage.TotalTokens)
		assert.Contains(t, recorder.Body.String(), `"model":"`+publicModel+`"`)
		assert.NotContains(t, recorder.Body.String(), targetModel)
		assert.NotContains(t, recorder.Body.String(), finalModel)
	})
}
