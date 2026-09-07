package model_setting

import (
	"bytes"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtocolBridgePolicyValidate(t *testing.T) {
	assert.Equal(t, 4*1024*1024, DefaultProtocolBridgeMaxStateBytes)
	assert.Equal(t, 128*1024*1024, MaxProtocolBridgeMaxStateBytes)

	valid := ProtocolBridgePolicy{
		StateTTLSeconds: DefaultProtocolBridgeStateTTLSeconds,
		MaxStateTurns:   DefaultProtocolBridgeMaxStateTurns,
		MaxStateBytes:   DefaultProtocolBridgeMaxStateBytes,
	}
	require.NoError(t, valid.Validate())

	maximum := valid
	maximum.MaxStateBytes = MaxProtocolBridgeMaxStateBytes
	require.NoError(t, maximum.Validate())

	tests := []struct {
		name   string
		mutate func(*ProtocolBridgePolicy)
		want   string
	}{
		{name: "TTL too small", mutate: func(policy *ProtocolBridgePolicy) { policy.StateTTLSeconds = MinProtocolBridgeStateTTLSeconds - 1 }, want: "state_ttl_seconds"},
		{name: "TTL too large", mutate: func(policy *ProtocolBridgePolicy) { policy.StateTTLSeconds = MaxProtocolBridgeStateTTLSeconds + 1 }, want: "state_ttl_seconds"},
		{name: "turns too small", mutate: func(policy *ProtocolBridgePolicy) { policy.MaxStateTurns = MinProtocolBridgeMaxStateTurns - 1 }, want: "max_state_turns"},
		{name: "turns too large", mutate: func(policy *ProtocolBridgePolicy) { policy.MaxStateTurns = MaxProtocolBridgeMaxStateTurns + 1 }, want: "max_state_turns"},
		{name: "bytes too small", mutate: func(policy *ProtocolBridgePolicy) { policy.MaxStateBytes = MinProtocolBridgeMaxStateBytes - 1 }, want: "max_state_bytes"},
		{name: "bytes too large", mutate: func(policy *ProtocolBridgePolicy) { policy.MaxStateBytes = MaxProtocolBridgeMaxStateBytes + 1 }, want: "max_state_bytes"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := valid
			test.mutate(&policy)
			err := policy.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.want)
		})
	}
}

func TestShouldPreserveThinkingSuffixExactAndRegex(t *testing.T) {
	settings := GetGlobalSettings()
	original := append([]string(nil), settings.ThinkingModelBlacklist...)
	t.Cleanup(func() { settings.ThinkingModelBlacklist = original })

	assert.True(t, ShouldPreserveThinkingSuffix("kimi-k2-thinking"))
	assert.True(t, ShouldPreserveThinkingSuffix("moonshotai/kimi-k2-thinking"))
	assert.False(t, ShouldPreserveThinkingSuffix("m@sha256:abc"))

	settings.ThinkingModelBlacklist = []string{
		"kimi-k2-thinking",
		"re:[",
		"re:",
		"re:.*@sha256:.*",
	}

	var logged bytes.Buffer
	previous := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logged
	t.Cleanup(func() { gin.DefaultErrorWriter = previous })

	assert.True(t, ShouldPreserveThinkingSuffix("kimi-k2-thinking"))
	assert.True(t, ShouldPreserveThinkingSuffix("m@sha256:abc"))
	assert.False(t, ShouldPreserveThinkingSuffix("m@sha256"))
	assert.False(t, ShouldPreserveThinkingSuffix("qwen3-max@thinking:on"))
	require.Contains(t, logged.String(), `invalid thinking_model_blacklist regex "re:["`)
	require.Contains(t, logged.String(), `invalid thinking_model_blacklist regex "re:"`)

	settings.ThinkingModelBlacklist = []string{"re:^beta@"}
	assert.False(t, ShouldPreserveThinkingSuffix("m@sha256:abc"))
	assert.True(t, ShouldPreserveThinkingSuffix("beta@sha256:abc"))
	assert.False(t, ShouldPreserveThinkingSuffix("alpha@sha256:abc"))
}
