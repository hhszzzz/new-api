package setting

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

type ModelRadarModelOverride struct {
	DisplayName string `json:"display_name,omitempty"`
	Vendor      string `json:"vendor,omitempty"`
	Hidden      bool   `json:"hidden,omitempty"`
}

type ModelRadarSettings struct {
	DefaultVendor         string                             `json:"default_vendor"`
	ShowDegradationAlerts bool                               `json:"show_degradation_alerts"`
	Models                map[string]ModelRadarModelOverride `json:"models"`
}

var modelRadarVendorPattern = regexp.MustCompile(`^[a-z0-9-]{0,32}$`)

func parseModelRadarSettings(raw string) (ModelRadarSettings, error) {
	settings := ModelRadarSettings{
		DefaultVendor: "openai", ShowDegradationAlerts: true,
		Models: make(map[string]ModelRadarModelOverride),
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return settings, nil
	}
	if raw[0] != '{' {
		return settings, fmt.Errorf("model radar settings must be a JSON object")
	}
	var config struct {
		DefaultVendor         string                              `json:"default_vendor"`
		ShowDegradationAlerts *bool                               `json:"show_degradation_alerts"`
		Models                map[string]*ModelRadarModelOverride `json:"models"`
	}
	if err := common.UnmarshalJsonStr(raw, &config); err != nil {
		return settings, fmt.Errorf("invalid model radar settings: %w", err)
	}
	if !modelRadarVendorPattern.MatchString(config.DefaultVendor) {
		return settings, fmt.Errorf("model radar default vendor must use up to 32 lowercase letters, digits or hyphens")
	}
	if config.DefaultVendor != "" {
		settings.DefaultVendor = config.DefaultVendor
	}
	if config.ShowDegradationAlerts != nil {
		settings.ShowDegradationAlerts = *config.ShowDegradationAlerts
	}
	if len(config.Models) > 256 {
		return settings, fmt.Errorf("model radar settings cannot contain more than 256 models")
	}
	for key, override := range config.Models {
		model := strings.TrimSpace(key)
		if model == "" || utf8.RuneCountInString(model) > 128 || override == nil {
			return settings, fmt.Errorf("model radar overrides require a model name of 1 to 128 characters and an object value")
		}
		if _, duplicate := settings.Models[model]; duplicate {
			return settings, fmt.Errorf("model radar settings contain duplicate model %q", model)
		}
		displayName := strings.TrimSpace(override.DisplayName)
		if utf8.RuneCountInString(displayName) > 128 || (override.DisplayName != "" && displayName == "") {
			return settings, fmt.Errorf("model radar display name must contain 1 to 128 characters when set")
		}
		if !modelRadarVendorPattern.MatchString(override.Vendor) || override.Vendor == "all" {
			return settings, fmt.Errorf("model radar vendor must use up to 32 lowercase letters, digits or hyphens")
		}
		override.DisplayName = displayName
		settings.Models[model] = *override
	}
	return settings, nil
}

func ValidateModelRadarSettings(raw string) error {
	_, err := parseModelRadarSettings(raw)
	return err
}

func GetModelRadarSettings() ModelRadarSettings {
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap["ModelRadarSettings"]
	common.OptionMapRWMutex.RUnlock()
	settings, err := parseModelRadarSettings(raw)
	if err != nil {
		settings, _ = parseModelRadarSettings("")
	}
	return settings
}
