package toolconv

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func geminiCodeExecutionRequest(t *testing.T) *dto.GeminiChatRequest {
	t.Helper()
	tools, err := kitutil.Marshal([]map[string]any{{"codeExecution": map[string]any{}}})
	require.NoError(t, err)
	return &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{Role: "user", Parts: []dto.GeminiPart{{Text: "run this"}}},
		},
		Tools: tools,
	}
}

func hasDiagnosticCode(diagnostics []types.ConversionDiagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func TestDefaultPolicyRejectsHostedExecutionRemoval(t *testing.T) {
	t.Parallel()

	_, set, err := ExtractRequest(types.RelayFormatGemini, geminiCodeExecutionRequest(t))
	require.NoError(t, err)
	target := &dto.GeneralOpenAIRequest{
		Model:    "gpt-4o",
		Messages: []dto.Message{{Role: "user", Content: "run this"}},
	}

	_, diagnostics, err := AttachRequest(types.RelayFormatOpenAI, target, set, &convmeta.Options{})
	var loss *types.ConversionLossError
	require.ErrorAs(t, err, &loss)
	assert.True(t, hasDiagnosticCode(diagnostics, "unsupported_hosted_tool"))
	assert.Equal(t, types.ConversionLossPolicySafe, (&convmeta.Options{}).EffectiveToolLossPolicy())
}

func TestResponsePhaseNeverRejectsEvenUnderStrictPolicy(t *testing.T) {
	t.Parallel()

	text := "hello"
	resp := &dto.ClaudeResponse{
		Type:       "message",
		Role:       "assistant",
		StopReason: "pause_turn",
		Content: []dto.ClaudeMediaMessage{
			{Type: "redacted_thinking", Data: "secret"},
			{Type: "text", Text: &text},
		},
	}
	diagnostics := InspectResponse(types.RelayFormatClaude, types.RelayFormatOpenAI, resp)
	require.True(t, hasDiagnosticCode(diagnostics, "continuation_state_lost"))
	require.Error(t, types.RejectConversionLoss(types.ConversionLossPolicyStrict, diagnostics))

	_, hosted, err := ExtractHostedResponse(types.RelayFormatClaude, resp)
	require.NoError(t, err)
	out, _, err := AttachHostedResponse(
		types.RelayFormatOpenAI,
		&dto.OpenAITextResponse{},
		hosted,
		&convmeta.Options{ToolLossPolicy: types.ConversionLossPolicyStrict},
	)
	require.NoError(t, err)
	require.NotNil(t, out)
}

func TestSafePolicyRejectsRequestPhaseHostedToolLoss(t *testing.T) {
	t.Parallel()

	_, set, err := ExtractRequest(types.RelayFormatGemini, geminiCodeExecutionRequest(t))
	require.NoError(t, err)
	target := &dto.GeneralOpenAIRequest{
		Model:    "gpt-4o",
		Messages: []dto.Message{{Role: "user", Content: "run this"}},
	}

	_, diagnostics, err := AttachRequest(
		types.RelayFormatOpenAI,
		target,
		set,
		&convmeta.Options{ToolLossPolicy: types.ConversionLossPolicySafe},
	)
	require.Error(t, err)
	var loss *types.ConversionLossError
	require.ErrorAs(t, err, &loss)
	require.NotEmpty(t, loss.Diagnostics)
	assert.True(t, hasDiagnosticCode(loss.Diagnostics, "unsupported_hosted_tool"))
	assert.True(t, hasDiagnosticCode(diagnostics, "unsupported_hosted_tool"))
}

func TestRejectConversionLossFollowsLossClassTier(t *testing.T) {
	t.Parallel()

	losses := []types.ConversionDiagnostic{
		{Code: "presentation", LossClass: types.ConversionLossPresentation, Severity: types.ConversionDiagnosticWarning},
		{Code: "tuning", LossClass: types.ConversionLossTuning, Severity: types.ConversionDiagnosticWarning},
		// A producer that marks a loss as an error refuses it outright; no tier
		// below allow may tolerate it because of its class.
		{Code: "refused-tuning", LossClass: types.ConversionLossTuning, Severity: types.ConversionDiagnosticError},
		{Code: "semantic", LossClass: types.ConversionLossSemantic, Severity: types.ConversionDiagnosticError},
	}
	for _, tc := range []struct {
		policy   types.ConversionLossPolicy
		rejected []string
	}{
		{policy: types.ConversionLossPolicyStrict, rejected: []string{"presentation", "tuning", "refused-tuning", "semantic"}},
		{policy: types.ConversionLossPolicySafe, rejected: []string{"tuning", "refused-tuning", "semantic"}},
		{policy: types.ConversionLossPolicyLossy, rejected: []string{"refused-tuning", "semantic"}},
		{policy: types.ConversionLossPolicyAllow, rejected: nil},
	} {
		t.Run(string(tc.policy), func(t *testing.T) {
			t.Parallel()

			err := types.RejectConversionLoss(tc.policy, losses)
			if len(tc.rejected) == 0 {
				require.NoError(t, err)
				return
			}
			var loss *types.ConversionLossError
			require.ErrorAs(t, err, &loss)
			rejected := make([]string, 0, len(loss.Diagnostics))
			for _, diagnostic := range loss.Diagnostics {
				rejected = append(rejected, diagnostic.Code)
			}
			assert.Equal(t, tc.rejected, rejected)
		})
	}
}

func claudeWebSearchRequest(t *testing.T, tool string) *dto.ClaudeRequest {
	t.Helper()
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{
		"model":"claude-test",
		"max_tokens":64,
		"tools":[`+tool+`],
		"messages":[{"role":"user","content":"hello"}]
	}`), &request))
	return &request
}

// A Claude web_search max_uses cap has no Chat Completions equivalent, so it is
// execution tuning: only a channel that opted into lossy conversion accepts the
// drop, and access constraints stay fatal on that channel.
func TestChatConversionOfSearchLimitFollowsTheLossyOptIn(t *testing.T) {
	t.Parallel()

	chatTarget := func() *dto.GeneralOpenAIRequest {
		return &dto.GeneralOpenAIRequest{
			Model:    "chat-model",
			Messages: []dto.Message{{Role: "user", Content: "hello"}},
		}
	}
	const cappedSearch = `{"type":"web_search_20250305","name":"web_search","max_uses":8}`

	t.Run("safe policy rejects the search cap", func(t *testing.T) {
		t.Parallel()

		_, set, err := ExtractRequest(types.RelayFormatClaude, claudeWebSearchRequest(t, cappedSearch))
		require.NoError(t, err)

		_, _, err = AttachRequest(
			types.RelayFormatOpenAI,
			chatTarget(),
			set,
			&convmeta.Options{ToolLossPolicy: types.ConversionLossPolicySafe},
		)
		var loss *types.ConversionLossError
		require.ErrorAs(t, err, &loss)
		require.Len(t, loss.Diagnostics, 1)
		assert.Equal(t, "unsupported_search_limit", loss.Diagnostics[0].Code)
		assert.Equal(t, types.ConversionLossTuning, loss.Diagnostics[0].LossClass)
	})

	t.Run("lossy policy converts and reports the dropped cap", func(t *testing.T) {
		t.Parallel()

		_, set, err := ExtractRequest(types.RelayFormatClaude, claudeWebSearchRequest(t, cappedSearch))
		require.NoError(t, err)

		converted, diagnostics, err := AttachRequest(
			types.RelayFormatOpenAI,
			chatTarget(),
			set,
			&convmeta.Options{ToolLossPolicy: types.ConversionLossPolicyLossy},
		)
		require.NoError(t, err)
		chatRequest, ok := converted.(*dto.GeneralOpenAIRequest)
		require.True(t, ok)
		require.NotNil(t, chatRequest.WebSearchOptions)

		var dropped *types.ConversionDiagnostic
		for index := range diagnostics {
			if diagnostics[index].Code == "unsupported_search_limit" {
				dropped = &diagnostics[index]
			}
		}
		require.NotNil(t, dropped)
		assert.Equal(t, types.ConversionLossTuning, dropped.LossClass)
		assert.Equal(t, types.ConversionDiagnosticWarning, dropped.Severity)
	})

	t.Run("lossy policy still rejects widened source access", func(t *testing.T) {
		t.Parallel()

		_, set, err := ExtractRequest(types.RelayFormatClaude, claudeWebSearchRequest(t, `{
			"type":"web_search_20250305","name":"web_search","max_uses":8,"allowed_domains":["example.com"]
		}`))
		require.NoError(t, err)

		_, diagnostics, err := AttachRequest(
			types.RelayFormatOpenAI,
			chatTarget(),
			set,
			&convmeta.Options{ToolLossPolicy: types.ConversionLossPolicyLossy},
		)
		var loss *types.ConversionLossError
		require.ErrorAs(t, err, &loss)
		assert.True(t, hasDiagnosticCode(loss.Diagnostics, "unsupported_domain_filter"))
		assert.True(t, hasDiagnosticCode(diagnostics, "unsupported_domain_filter"))
	})
}
