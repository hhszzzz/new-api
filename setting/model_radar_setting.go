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
	// AutoEffort opts this model into radar auto-effort. It defaults to false
	// so an administrator must switch each model on before the radar may
	// replace its reasoning tier.
	AutoEffort bool `json:"auto_effort,omitempty"`
	// Aliases are additional gateway model names that resolve to this radar
	// model for radar auto-effort matching.
	Aliases []string `json:"aliases,omitempty"`
}

type ModelRadarSettings struct {
	DefaultVendor string `json:"default_vendor"`
	// AutoEffortEnabled is the administrator kill switch for radar auto-effort.
	// It defaults to true when the option does not set it.
	AutoEffortEnabled     bool                               `json:"auto_effort_enabled"`
	ShowDegradationAlerts bool                               `json:"show_degradation_alerts"`
	Models                map[string]ModelRadarModelOverride `json:"models"`
}

var modelRadarVendorPattern = regexp.MustCompile(`^[a-z0-9-]{0,32}$`)

// modelRadarMaxAliasesPerModel bounds the alias list an administrator may
// declare for one radar model.
const modelRadarMaxAliasesPerModel = 16

func parseModelRadarSettings(raw string) (ModelRadarSettings, error) {
	settings := ModelRadarSettings{
		DefaultVendor: "openai", ShowDegradationAlerts: true, AutoEffortEnabled: true,
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
		AutoEffortEnabled     *bool                               `json:"auto_effort_enabled"`
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
	if config.AutoEffortEnabled != nil {
		settings.AutoEffortEnabled = *config.AutoEffortEnabled
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
		if len(override.Aliases) > modelRadarMaxAliasesPerModel {
			return settings, fmt.Errorf("model radar model %q cannot declare more than %d aliases", model, modelRadarMaxAliasesPerModel)
		}
		for _, alias := range override.Aliases {
			value := strings.TrimSpace(alias)
			if value == "" || utf8.RuneCountInString(value) > 128 {
				return settings, fmt.Errorf("model radar aliases must contain 1 to 128 characters")
			}
		}
		override.DisplayName = displayName
		override.Aliases = normalizeModelRadarAliases(model, override.Aliases)
		if len(override.Aliases) == 0 {
			override.Aliases = nil
		}
		settings.Models[model] = *override
	}
	if err := validateModelRadarAliasConflicts(settings.Models); err != nil {
		return settings, err
	}
	return settings, nil
}

// normalizeModelRadarAliases lowercases and deduplicates aliases, dropping one
// that merely repeats its own model name.
func normalizeModelRadarAliases(model string, aliases []string) []string {
	if len(aliases) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(aliases))
	seen := make(map[string]struct{}, len(aliases))
	for _, alias := range aliases {
		value := strings.ToLower(strings.TrimSpace(alias))
		if value == "" || value == strings.ToLower(model) {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

// validateModelRadarAliasConflicts rejects an alias that another radar model
// already claims, either as its own name or as its alias.
func validateModelRadarAliasConflicts(models map[string]ModelRadarModelOverride) error {
	owner := make(map[string]string, len(models))
	for model := range models {
		owner[strings.ToLower(model)] = model
	}
	for model, override := range models {
		for _, alias := range override.Aliases {
			if other, exists := owner[alias]; exists && other != model {
				return fmt.Errorf("model radar alias %q conflicts with model %q", alias, other)
			}
			owner[alias] = model
		}
	}
	return nil
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

// ModelRadarSettingsRaw returns the stored option value. Callers use it to
// detect configuration changes without re-parsing the option.
func ModelRadarSettingsRaw() string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap["ModelRadarSettings"]
}
