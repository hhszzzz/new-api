package setting

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateModelRadarSettings(t *testing.T) {
	models := make(map[string]ModelRadarModelOverride)
	for i := 0; i < 257; i++ {
		models[fmt.Sprintf("model-%d", i)] = ModelRadarModelOverride{}
	}
	oversized, err := common.Marshal(ModelRadarSettings{Models: models})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"empty", "", true},
		{"partial", `{}`, true},
		{"valid", `{"default_vendor":"all","show_degradation_alerts":false,"models":{"k3":{"display_name":"Kimi K3","vendor":"moonshot","hidden":true}}}`, true},
		{"trimmed names", `{"models":{" k3 ":{"display_name":" kimi-k3 "}}}`, true},
		{"too many models", string(oversized), false},
		{"invalid default vendor", `{"default_vendor":"OpenAI"}`, false},
		{"invalid vendor", `{"models":{"k3":{"vendor":"bad vendor"}}}`, false},
		{"reserved vendor", `{"models":{"k3":{"vendor":"all"}}}`, false},
		{"long vendor", `{"default_vendor":"` + strings.Repeat("a", 33) + `"}`, false},
		{"long model", `{"models":{"` + strings.Repeat("a", 129) + `":{}}}`, false},
		{"long display", `{"models":{"k3":{"display_name":"` + strings.Repeat("a", 129) + `"}}}`, false},
		{"empty model", `{"models":{" ":{}}}`, false},
		{"blank display", `{"models":{"k3":{"display_name":" "}}}`, false},
		{"duplicate normalized model", `{"models":{"k3":{}," k3 ":{}}}`, false},
		{"null override", `{"models":{"k3":null}}`, false},
		{"non boolean", `{"show_degradation_alerts":"false"}`, false},
		{"null", `null`, false},
		{"array", `[]`, false},
		{"scalar", `true`, false},
		{"malformed", `{`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateModelRadarSettings(tc.raw)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestGetModelRadarSettings(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	})
	for _, raw := range []string{"", `{}`, `null`, `{"default_vendor":"bad vendor"}`} {
		common.OptionMapRWMutex.Lock()
		common.OptionMap["ModelRadarSettings"] = raw
		common.OptionMapRWMutex.Unlock()
		settings := GetModelRadarSettings()
		assert.Equal(t, "openai", settings.DefaultVendor)
		assert.True(t, settings.ShowDegradationAlerts)
		assert.NotNil(t, settings.Models)
		assert.Empty(t, settings.Models)
	}
	common.OptionMapRWMutex.Lock()
	common.OptionMap["ModelRadarSettings"] = `{"default_vendor":"anthropic","show_degradation_alerts":false,"models":{" k3 ":{"display_name":" kimi-k3 ","hidden":true}}}`
	common.OptionMapRWMutex.Unlock()
	settings := GetModelRadarSettings()
	assert.Equal(t, "anthropic", settings.DefaultVendor)
	assert.False(t, settings.ShowDegradationAlerts)
	assert.Equal(t, ModelRadarModelOverride{DisplayName: "kimi-k3", Hidden: true}, settings.Models["k3"])
}
