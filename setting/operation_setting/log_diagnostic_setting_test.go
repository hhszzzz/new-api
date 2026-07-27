package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeLogDiagnosticSettingPublishesImmutableSnapshot(t *testing.T) {
	target := GetLogDiagnosticSetting()
	original := *GetLogDiagnosticSettingSnapshot()
	t.Cleanup(func() {
		*target = original
		NormalizeLogDiagnosticSetting()
	})

	*target = LogDiagnosticSetting{
		RecordIP:      true,
		RecordHeaders: true,
		ExtraHeaders:  []string{" X-Trace ", "x-trace", "x-client"},
	}
	NormalizeLogDiagnosticSetting()
	previous := GetLogDiagnosticSettingSnapshot()
	require.NotNil(t, previous)
	assert.True(t, previous.RecordIP)
	assert.Equal(t, []string{"x-client", "x-trace"}, previous.ExtraHeaders)

	target.RecordIP = false
	target.ExtraHeaders[0] = "x-mutated"
	assert.True(t, previous.RecordIP)
	assert.Equal(t, []string{"x-client", "x-trace"}, previous.ExtraHeaders)

	NormalizeLogDiagnosticSetting()
	current := GetLogDiagnosticSettingSnapshot()
	assert.False(t, current.RecordIP)
	assert.Equal(t, []string{"x-mutated", "x-trace"}, current.ExtraHeaders)
}
