package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlphaSearchHelperOpenAIRequiresExplicitCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()

	tests := []struct {
		name              string
		settingsJSON      string
		wantUpstreamCalls int
		wantStatusCode    int
	}{
		{
			name:              "disabled by default",
			settingsJSON:      `{}`,
			wantUpstreamCalls: 0,
			wantStatusCode:    http.StatusInternalServerError,
		},
		{
			name:              "enabled explicitly",
			settingsJSON:      `{"allow_alpha_search":true}`,
			wantUpstreamCalls: 1,
			wantStatusCode:    http.StatusBadRequest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstreamCalls := 0
			var upstreamPath string
			var upstreamBody string
			var upstreamReadErr error
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls++
				upstreamPath = r.URL.Path
				body, err := io.ReadAll(r.Body)
				upstreamReadErr = err
				upstreamBody = string(body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"captured","type":"invalid_request_error"}}`))
			}))
			defer upstream.Close()

			var otherSettings dto.ChannelOtherSettings
			require.NoError(t, common.Unmarshal([]byte(test.settingsJSON), &otherSettings))

			rawBody := `{"model":"gpt-5-codex","query":"test"}`
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(rawBody))
			ctx.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "gpt-5-codex")
			common.SetContextKey(ctx, constant.ContextKeyChannelId, 1)
			common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
			common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(ctx, constant.ContextKeyChannelKey, "test-key")
			common.SetContextKey(ctx, constant.ContextKeyChannelOtherSetting, otherSettings)

			request := &dto.AlphaSearchRequest{
				Model:   "gpt-5-codex",
				RawBody: []byte(rawBody),
			}
			info, err := relaycommon.GenRelayInfo(ctx, types.RelayFormatOpenAIAlphaSearch, request, nil)
			require.NoError(t, err)

			relayErr := AlphaSearchHelper(ctx, info)

			require.NotNil(t, relayErr)
			require.NoError(t, upstreamReadErr)
			assert.Equal(t, test.wantStatusCode, relayErr.StatusCode)
			assert.Equal(t, test.wantUpstreamCalls, upstreamCalls)
			if test.wantUpstreamCalls == 1 {
				assert.Equal(t, "/v1/alpha/search", upstreamPath)
				assert.JSONEq(t, rawBody, upstreamBody)
			}
		})
	}
}

func TestBuildAlphaSearchRequestBodyPreservesUnknownFields(t *testing.T) {
	raw := []byte(`{
		"id":"req_1",
		"model":"gpt-5.1",
		"input":[{"role":"user","content":"hi"}],
		"commands":{"search_query":[{"q":"weather","recency":1}]},
		"settings":{"locale":"en"},
		"future_field":{"nested":true}
	}`)

	out, err := buildAlphaSearchRequestBody(raw, "gpt-5.1", "gpt-5.1-mapped")
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, common.Unmarshal(out, &body))
	assert.Equal(t, "gpt-5.1-mapped", body["model"])
	assert.Equal(t, "req_1", body["id"])
	require.Contains(t, body, "commands")
	require.Contains(t, body, "settings")
	require.Contains(t, body, "future_field")
	require.Contains(t, body, "input")

	commands, ok := body["commands"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, commands, "search_query")

	future, ok := body["future_field"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, future["nested"])
}

func TestBuildAlphaSearchRequestBodyNoMappingKeepsRawBytes(t *testing.T) {
	raw := []byte(`{"model":"gpt-5.1","commands":{"search_query":[{"q":"x"}]},"future_field":1}`)
	out, err := buildAlphaSearchRequestBody(raw, "gpt-5.1", "gpt-5.1")
	require.NoError(t, err)
	assert.Equal(t, raw, out)
}
