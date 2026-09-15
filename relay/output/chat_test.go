package output

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestChatWriterPreservesChoicesAndExtensions(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, force := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%t/force=%t", stream, force), func(t *testing.T) {
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				writer := &ChatWriter{ResponseWriter: ctx.Writer, ForceFormat: force, ThinkingToContent: true}
				field, status := "message", http.StatusOK
				if stream {
					field, status = "delta", 0
				}
				body := fmt.Sprintf(`{"id":"r","object":"chat.completion","model":"test","future_extension":{"keep":true},"choices":[{"index":0,"%s":{"role":"assistant","reasoning_content":"reason-0","content":"answer-0"},"finish_reason":"stop"},{"index":1,"%s":{"role":"assistant","reasoning_content":"reason-1","content":"answer-1"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}}`, field, field)
				require.NoError(t, writer.WriteMessage(Message{Data: []byte(body), Status: status}))
				payload := strings.TrimSpace(strings.TrimPrefix(recorder.Body.String(), "data: "))
				for index := range 2 {
					assert.Equal(t, fmt.Sprintf("<think>\nreason-%d\n</think>\nanswer-%d", index, index), gjson.Get(payload, fmt.Sprintf("choices.%d.%s.content", index, field)).String())
					assert.False(t, gjson.Get(payload, fmt.Sprintf("choices.%d.%s.reasoning_content", index, field)).Exists())
				}
				assert.Equal(t, int64(14), gjson.Get(payload, "usage.total_tokens").Int())
				assert.Equal(t, !force, gjson.Get(payload, "future_extension.keep").Exists())
				if !stream {
					assert.Equal(t, fmt.Sprint(len(recorder.Body.Bytes())), recorder.Header().Get("Content-Length"))
				}
			})
		}
	}
}

func TestChatWriterKeepsThinkingAndTerminalStateSeparate(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	writer := &ChatWriter{ResponseWriter: ctx.Writer, ThinkingToContent: true}
	for _, data := range []string{
		`{"choices":[{"index":0,"delta":{"reasoning_content":"first"},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{"reasoning_content":" second","content":"answer"},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"total_tokens":9}}`,
		`[DONE]`,
	} {
		require.NoError(t, writer.WriteMessage(Message{Data: []byte(data)}))
	}
	frames := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n\n")
	require.Len(t, frames, 4)
	assert.Equal(t, "<think>\nfirst", gjson.Get(strings.TrimPrefix(frames[0], "data: "), "choices.0.delta.content").String())
	assert.Equal(t, " second\n</think>\nanswer", gjson.Get(strings.TrimPrefix(frames[1], "data: "), "choices.0.delta.content").String())
	assert.Equal(t, "stop", gjson.Get(strings.TrimPrefix(frames[2], "data: "), "choices.0.finish_reason").String())
	assert.Equal(t, int64(9), gjson.Get(strings.TrimPrefix(frames[2], "data: "), "usage.total_tokens").Int())
	assert.Equal(t, "data: [DONE]", frames[3])

	errorRecorder := httptest.NewRecorder()
	errorCtx, _ := gin.CreateTestContext(errorRecorder)
	errorWriter := &ChatWriter{ResponseWriter: errorCtx.Writer, ForceFormat: true, ThinkingToContent: true}
	errorPayload := `{"error":{"message":"upstream failed","type":"upstream_error"}}`
	require.NoError(t, errorWriter.WriteMessage(Message{Data: []byte(errorPayload)}))
	assert.Contains(t, errorRecorder.Body.String(), errorPayload)
}
