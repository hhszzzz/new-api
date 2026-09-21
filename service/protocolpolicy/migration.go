package protocolpolicy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/service/modelmapping"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const optionKey = "global.protocol_policy"

var legacyOptionKeys = []string{"global.protocol_bridge_policy", "global.chat_completions_to_responses_policy", "global.pass_through_request_enabled"}
var settingsKeys = []string{"protocol_policy", "protocol_capabilities", "tool_loss_policy", "advanced_custom"}

type ChannelChange struct {
	ID         int    `json:"id"`
	Before     string `json:"before"`
	BeforeNull bool   `json:"before_null,omitempty"`
	After      string `json:"after"`
}
type OptionChange struct {
	Key    string  `json:"key"`
	Before *string `json:"before"`
	After  *string `json:"after"`
}

// Manifest contains rollback configuration and may include route credentials.
// Only Review is suitable for logs/API output. Backups are owner-readable only.
type Manifest struct {
	Version     int             `json:"version"`
	CreatedAt   time.Time       `json:"created_at"`
	Channels    []ChannelChange `json:"channels"`
	Options     []OptionChange  `json:"options"`
	Differences []string        `json:"differences"`
	Blockers    []string        `json:"blockers"`
}

func (m *Manifest) Review() map[string]any {
	ids := make([]int, len(m.Channels))
	for i := range m.Channels {
		ids[i] = m.Channels[i].ID
	}
	return map[string]any{"version": m.Version, "channel_ids": ids, "channel_changes": len(ids), "option_changes": len(m.Options), "differences": m.Differences, "blockers": m.Blockers}
}

// Preflight only selects configuration. It never invokes Channel.GetSetting,
// whose historical error recovery could write malformed configuration back.
func Preflight(db *gorm.DB) (*Manifest, error) {
	db = db.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	m := &Manifest{Version: hostdto.ProtocolPolicyVersion, CreatedAt: time.Now().UTC(), Channels: []ChannelChange{}, Options: []OptionChange{}, Differences: []string{}, Blockers: []string{}}
	var options []model.Option
	keys := append(slices.Clone(legacyOptionKeys), optionKey)
	if err := db.Where(map[string]any{"key": keys}).Find(&options).Error; err != nil {
		return nil, fmt.Errorf("read protocol options: %w", err)
	}
	values := make(map[string]string, len(options))
	for _, option := range options {
		values[option.Key] = option.Value
	}
	global := model_setting.GlobalSettings{
		ChatCompletionsToResponsesPolicy: model_setting.ChatCompletionsToResponsesPolicy{AllChannels: true},
		ProtocolBridgePolicy: model_setting.ProtocolBridgePolicy{
			StateTTLSeconds: model_setting.DefaultProtocolBridgeStateTTLSeconds,
			MaxStateTurns:   model_setting.DefaultProtocolBridgeMaxStateTurns,
			MaxStateBytes:   model_setting.DefaultProtocolBridgeMaxStateBytes,
		},
	}
	for key, target := range map[string]any{"global.protocol_bridge_policy": &global.ProtocolBridgePolicy, "global.chat_completions_to_responses_policy": &global.ChatCompletionsToResponsesPolicy, "global.pass_through_request_enabled": &global.PassThroughRequestEnabled} {
		if value, ok := values[key]; ok {
			if err := validateMigrationFields(json.RawMessage(value), reflect.TypeOf(target)); err != nil {
				m.Blockers = append(m.Blockers, "unrecognized fields in "+key)
			} else if err := common.UnmarshalJsonStr(value, target); err != nil {
				m.Blockers = append(m.Blockers, "invalid "+key)
			}
		}
	}
	var channels []model.Channel
	if err := db.Select("id", "type", "models", "model_mapping", "base_url", "setting", "settings").Order("id").Find(&channels).Error; err != nil {
		return nil, fmt.Errorf("read channel protocol configuration: %w", err)
	}
	var nullSettingsIDs []int
	if err := db.Model(&model.Channel{}).Where(map[string]any{"settings": nil}).Pluck("id", &nullSettingsIDs).Error; err != nil {
		return nil, errors.New("read nullable channel settings failed")
	}
	policy := hostdto.DefaultProtocolPolicy()
	if value, ok := values[optionKey]; ok {
		if err := validateMigrationFields(json.RawMessage(value), reflect.TypeFor[hostdto.ProtocolPolicy]()); err != nil {
			m.Blockers = append(m.Blockers, "unrecognized fields in "+optionKey)
		} else if err := common.UnmarshalJsonStr(value, &policy); err != nil {
			m.Blockers = append(m.Blockers, "invalid "+optionKey)
		} else if err := policy.Validate(); err != nil {
			m.Blockers = append(m.Blockers, err.Error())
		}
		global.ProtocolPolicy = &policy
	} else {
		if len(channels) > 0 || len(values) > 0 {
			policy = global.EffectiveProtocolPolicy()
			if global.ProtocolBridgePolicy.Enabled {
				policy.StateScope = hostdto.ProtocolStateAll
			}
		}
		legacy := global.ChatCompletionsToResponsesPolicy
		if legacy.Enabled && !global.PassThroughRequestEnabled {
			for _, pattern := range legacy.ModelPatterns {
				if pattern == "" {
					continue
				}
				if _, err := regexp.Compile(pattern); err != nil {
					m.Blockers = append(m.Blockers, "invalid Chat-to-Responses model pattern")
					continue
				}
				inputs := []relayconvert.Protocol{relayconvert.ProtocolChat}
				if !global.ProtocolBridgePolicy.Enabled {
					inputs = append(inputs, relayconvert.ProtocolMessages)
				}
				for _, input := range inputs {
					rule := hostdto.ProtocolModelRule{ModelPattern: pattern, RequestProtocol: input, TargetProtocol: relayconvert.ProtocolResponses, RequireStructured: true}
					if !legacy.AllChannels {
						rule.ChannelIDs = slices.Clone(legacy.ChannelIDs)
						rule.ChannelTypes = slices.Clone(legacy.ChannelTypes)
						if len(rule.ChannelIDs) == 0 && len(rule.ChannelTypes) == 0 {
							continue
						}
					}
					policy.Rules = append(policy.Rules, rule)
				}
			}
		}
		encoded, err := common.Marshal(policy)
		if err != nil {
			return nil, err
		}
		m.Options = append(m.Options, OptionChange{Key: optionKey, After: common.GetPointer(string(encoded))})
	}
	for _, key := range legacyOptionKeys {
		if value, ok := values[key]; ok {
			m.Options = append(m.Options, OptionChange{Key: key, Before: common.GetPointer(value)})
		}
	}
	for _, channel := range channels {
		after, differences, err := NormalizeChannel(&channel, global)
		if err != nil {
			m.Blockers = append(m.Blockers, fmt.Sprintf("channel %d: %s", channel.Id, err))
			continue
		}
		m.Differences = append(m.Differences, differences...)
		if after != channel.OtherSettings {
			m.Channels = append(m.Channels, ChannelChange{ID: channel.Id, Before: channel.OtherSettings, BeforeNull: slices.Contains(nullSettingsIDs, channel.Id), After: after})
		}
	}
	return m, nil
}

func NormalizeChannel(channel *model.Channel, global model_setting.GlobalSettings) (string, []string, error) {
	var settings hostdto.ChannelOtherSettings
	raw := make(map[string]json.RawMessage)
	if channel.OtherSettings != "" {
		if common.GetJsonType(json.RawMessage(channel.OtherSettings)) != "object" {
			return "", nil, errors.New("channel settings must be a JSON object")
		}
		if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
			return "", nil, errors.New("malformed settings")
		}
		if err := common.UnmarshalJsonStr(channel.OtherSettings, &raw); err != nil {
			return "", nil, errors.New("malformed settings")
		}
	}
	for key, shape := range map[string]reflect.Type{
		"protocol_capabilities": reflect.TypeFor[hostdto.ProtocolCapabilities](),
		"protocol_policy":       reflect.TypeFor[hostdto.ProtocolPolicy](),
	} {
		if value, ok := raw[key]; ok {
			if err := validateMigrationFields(value, shape); err != nil {
				return "", nil, fmt.Errorf("unrecognized fields in %s", key)
			}
		}
	}
	if err := settings.ProtocolCapabilities.Validate(); err != nil {
		return "", nil, err
	}
	if err := settings.ValidateToolLossPolicy(); err != nil {
		return "", nil, err
	}
	if err := settings.ProtocolPolicy.Validate(); err != nil {
		return "", nil, err
	}
	if settings.ProtocolPolicy == nil && global.ProtocolPolicy != nil {
		if settings.ProtocolCapabilities == nil && settings.ToolLossPolicy == "" {
			settings.ProtocolPolicy = &hostdto.ProtocolPolicy{Version: hostdto.ProtocolPolicyVersion}
			if channel.Setting != nil && *channel.Setting != "" {
				var requestSettings hostdto.ChannelSettings
				if err := common.UnmarshalJsonStr(*channel.Setting, &requestSettings); err != nil {
					return "", nil, errors.New("malformed request settings")
				}
				if requestSettings.PassThroughBodyEnabled {
					settings.ProtocolPolicy.RequestMode = hostdto.ProtocolRequestPassthrough
				}
			}
			encoded, err := common.Marshal(settings.ProtocolPolicy)
			if err != nil {
				return "", nil, err
			}
			raw["protocol_policy"] = encoded
		} else {
			// Imports carry explicit legacy channel intent; apply it under the
			// current global defaults rather than reviving a retired global gate.
			global.PassThroughRequestEnabled = global.ProtocolPolicy.RequestMode == hostdto.ProtocolRequestPassthrough
			global.ProtocolBridgePolicy.Enabled = true
			global.ProtocolBridgePolicy.DefaultAllowConversion = true
			global.ProtocolPolicy = nil
		}
	}
	var differences []string
	if settings.ProtocolPolicy == nil {
		policy := hostdto.ProtocolPolicy{Version: hostdto.ProtocolPolicyVersion, Conversion: hostdto.ProtocolConversionSafe}
		if settings.ToolLossPolicy == "strict" {
			policy.Conversion = hostdto.ProtocolConversionLossless
		}
		if channel.Setting != nil && *channel.Setting != "" {
			var setting hostdto.ChannelSettings
			if err := common.UnmarshalJsonStr(*channel.Setting, &setting); err != nil {
				return "", nil, errors.New("malformed request settings")
			}
			if setting.PassThroughBodyEnabled {
				policy.RequestMode = hostdto.ProtocolRequestPassthrough
			}
		}
		caps := settings.ProtocolCapabilities
		if caps != nil && caps.GetSelectionMode() == hostdto.ProtocolSelectionModeAuto {
			policy.Selection = hostdto.ProtocolSelectionAutomatic
		}
		if policy.Conversion == hostdto.ProtocolConversionSafe && caps != nil && caps.AllowLossyConversion {
			// The channel opted into lossy conversion under the old settings;
			// encode that effective behavior in the generated policy instead of
			// dropping the opt-in. Strict tool-loss channels stay lossless.
			policy.Conversion = hostdto.ProtocolConversionLossy
		}
		if channel.Type != constant.ChannelTypeAdvancedCustom {
			// Freeze each existing configured model's effective route, then preserve
			// ordered regex overrides and a fallback for later model-list updates.
			models := strings.Split(channel.Models, ",")
			seen := make(map[string]bool)
			for _, modelName := range models {
				modelName = strings.TrimSpace(modelName)
				if modelName == "" {
					continue
				}
				if _, err := modelmapping.Resolve(channel.GetModelMapping(), modelName); err != nil {
					return "", nil, err
				}
				if seen[modelName] {
					continue
				}
				seen[modelName] = true
				for _, protocol := range relayconvert.Protocols() {
					policy.Rules = append(policy.Rules, legacyRule(channel, global, settings, protocol, modelName, "^"+regexp.QuoteMeta(modelName)+"$"))
				}
			}
			if caps != nil && global.ProtocolBridgePolicy.Enabled {
				for _, override := range caps.ModelOverrides {
					for _, protocol := range []relayconvert.Protocol{relayconvert.ProtocolMessages, relayconvert.ProtocolResponses} {
						upstream := override.UpstreamProtocols
						if len(upstream) == 0 {
							upstream = caps.UpstreamProtocols
						}
						conversion := policy.Conversion
						allowed := override.AllowConversion
						if allowed == nil {
							allowed = caps.AllowConversion
						}
						if allowed != nil && !*allowed {
							conversion = hostdto.ProtocolConversionNative
						}
						policy.Rules = append(policy.Rules, hostdto.ProtocolModelRule{ModelPattern: override.ModelPattern, RequestProtocol: protocol, Conversion: conversion, UpstreamProtocols: slices.Clone(upstream)})
					}
				}
			}
			for _, protocol := range relayconvert.Protocols() {
				policy.Rules = append(policy.Rules, legacyRule(channel, global, settings, protocol, "", ""))
			}
		} else if caps != nil && caps.AllowConversion != nil && !*caps.AllowConversion && global.ProtocolBridgePolicy.Enabled {
			for _, protocol := range []relayconvert.Protocol{relayconvert.ProtocolMessages, relayconvert.ProtocolResponses} {
				policy.Rules = append(policy.Rules, hostdto.ProtocolModelRule{RequestProtocol: protocol, Conversion: hostdto.ProtocolConversionNative})
			}
		}
		if err := policy.Validate(); err != nil {
			return "", nil, err
		}
		encoded, err := common.Marshal(policy)
		if err != nil {
			return "", nil, err
		}
		raw["protocol_policy"] = encoded
		if settings.ToolLossPolicy == "" || settings.ToolLossPolicy == "allow" || caps != nil && caps.AllowLossyConversion {
			differences = append(differences, fmt.Sprintf("channel %d: unclassified loss, hosted-tool removal, and opaque-state removal now fail before upstream dispatch", channel.Id))
		}
		differences = append(differences, fmt.Sprintf("channel %d: compact requires a native compact endpoint; generation is never substituted for compaction", channel.Id))
	}
	delete(raw, "protocol_capabilities")
	delete(raw, "tool_loss_policy")
	if settings.AdvancedCustom != nil {
		if err := settings.AdvancedCustom.Validate(); err != nil {
			return "", nil, err
		}
		var custom map[string]json.RawMessage
		if err := common.Unmarshal(raw["advanced_custom"], &custom); err != nil {
			return "", nil, err
		}
		var routes []map[string]json.RawMessage
		if err := common.Unmarshal(custom["advanced_routes"], &routes); err != nil {
			return "", nil, err
		}
		for i, route := range settings.AdvancedCustom.Routes {
			target, err := route.ResolveTarget()
			if err != nil {
				return "", nil, err
			}
			name := string(target)
			// Keep the native marker stable after legacy converters are removed.
			if route.TargetProtocol == "native" || route.TargetProtocol == "" && (route.Converter == "" || route.Converter == relayconvert.ConverterNone) {
				name = "native"
			}
			if name == "" {
				name = "native"
			}
			routes[i]["target_protocol"], err = common.Marshal(name)
			if err != nil {
				return "", nil, err
			}
			delete(routes[i], "converter")
		}
		custom["advanced_routes"], _ = common.Marshal(routes)
		raw["advanced_custom"], _ = common.Marshal(custom)
	}
	after, err := common.Marshal(raw)
	if err != nil {
		return "", nil, err
	}
	var beforeValue, afterValue any
	_ = common.UnmarshalJsonStr(channel.OtherSettings, &beforeValue)
	_ = common.Unmarshal(after, &afterValue)
	if reflect.DeepEqual(beforeValue, afterValue) {
		return channel.OtherSettings, differences, nil
	}
	return string(after), differences, nil
}

func legacyRule(channel *model.Channel, global model_setting.GlobalSettings, settings hostdto.ChannelOtherSettings, protocol relayconvert.Protocol, modelName, pattern string) hostdto.ProtocolModelRule {
	policy, candidates, err := channelcompat.LegacyPolicyForRequest(channel, protocol, modelName, relayconvert.CanonicalPath(protocol, modelName), settings, global)
	rule := hostdto.ProtocolModelRule{ModelPattern: pattern, RequestProtocol: protocol, Conversion: policy.Conversion}
	if err != nil {
		rule.Deny = true
		return rule
	}
	for _, candidate := range candidates {
		rule.UpstreamProtocols = append(rule.UpstreamProtocols, string(candidate))
	}
	return rule
}

// Configuration that is being retired must not contain fields this version
// cannot interpret. Maps and raw provider payloads are deliberately opaque.
func validateMigrationFields(data json.RawMessage, shape reflect.Type) error {
	for shape.Kind() == reflect.Pointer {
		shape = shape.Elem()
	}
	if shape == reflect.TypeFor[json.RawMessage]() {
		return nil
	}
	switch shape.Kind() {
	case reflect.Struct:
		var object map[string]json.RawMessage
		if err := common.Unmarshal(data, &object); err != nil {
			return err
		}
		fields := make(map[string]reflect.Type)
		for i := range shape.NumField() {
			field := shape.Field(i)
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if field.IsExported() && name != "" && name != "-" {
				fields[name] = field.Type
			}
		}
		for key, value := range object {
			field, ok := fields[key]
			if !ok {
				return errors.New("unknown protocol configuration field")
			}
			if err := validateMigrationFields(value, field); err != nil {
				return err
			}
		}
	case reflect.Slice:
		var items []json.RawMessage
		if err := common.Unmarshal(data, &items); err != nil {
			return err
		}
		for _, item := range items {
			if err := validateMigrationFields(item, shape.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func SaveBackup(path string, m *Manifest) error {
	if m == nil || len(m.Blockers) > 0 {
		return errors.New("protocol migration preflight has blockers")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	data, err := common.Marshal(m)
	if err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func LoadBackup(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := common.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.Version != hostdto.ProtocolPolicyVersion {
		return nil, fmt.Errorf("unsupported migration backup version %d", m.Version)
	}
	return &m, nil
}

// Apply is atomic and compares every source row with the backed-up snapshot.
// Repeating Apply with the same manifest is harmless.
func Apply(db *gorm.DB, m *Manifest) error    { return changeConfiguration(db, m, false) }
func Rollback(db *gorm.DB, m *Manifest) error { return changeConfiguration(db, m, true) }

func changeConfiguration(db *gorm.DB, m *Manifest, rollback bool) error {
	if m == nil || m.Version != hostdto.ProtocolPolicyVersion || len(m.Blockers) > 0 {
		return errors.New("invalid or blocked protocol migration manifest")
	}
	db = db.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	return db.Transaction(func(tx *gorm.DB) error {
		for _, change := range m.Channels {
			var row model.Channel
			if err := tx.Select("id", "settings").First(&row, change.ID).Error; err != nil {
				return fmt.Errorf("channel %d is unavailable", change.ID)
			}
			if rollback && row.OtherSettings == change.Before {
				continue
			}
			from, to := change.Before, change.After
			var sourceValue any = from
			var targetValue any = to
			if change.BeforeNull {
				sourceValue = nil
			}
			if rollback {
				var current, before, after map[string]json.RawMessage
				if err := common.UnmarshalJsonStr(row.OtherSettings, &current); err != nil {
					return fmt.Errorf("channel %d has invalid current settings", change.ID)
				}
				_ = common.UnmarshalJsonStr(change.Before, &before)
				_ = common.UnmarshalJsonStr(change.After, &after)
				for _, key := range settingsKeys {
					var actual, want, original any
					_ = common.Unmarshal(current[key], &actual)
					_ = common.Unmarshal(after[key], &want)
					_ = common.Unmarshal(before[key], &original)
					if reflect.DeepEqual(actual, original) {
						continue
					}
					if !reflect.DeepEqual(actual, want) {
						return fmt.Errorf("channel %d protocol settings changed after migration", change.ID)
					}
					if value, ok := before[key]; ok {
						current[key] = value
					} else {
						delete(current, key)
					}
				}
				encoded, err := common.Marshal(current)
				if err != nil {
					return err
				}
				from, to = row.OtherSettings, string(encoded)
				sourceValue, targetValue = from, to
				if len(current) == 0 && change.Before == "" {
					to = ""
					targetValue = ""
					if change.BeforeNull {
						targetValue = nil
					}
				}
			}
			if row.OtherSettings == to {
				continue
			}
			if row.OtherSettings != from {
				return fmt.Errorf("channel %d changed since preflight", change.ID)
			}
			result := tx.Model(&model.Channel{}).Where(map[string]any{"id": change.ID, "settings": sourceValue}).UpdateColumn("settings", targetValue)
			if result.Error != nil {
				return fmt.Errorf("update channel %d protocol settings failed", change.ID)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("channel %d changed during migration", change.ID)
			}
		}
		for _, change := range m.Options {
			from, to := change.Before, change.After
			if rollback {
				from, to = to, from
			}
			var row model.Option
			err := tx.Where(map[string]any{"key": change.Key}).First(&row).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("read protocol option %s failed", change.Key)
			}
			exists := err == nil
			if !exists && to == nil || exists && to != nil && row.Value == *to {
				continue
			}
			if exists != (from != nil) || exists && row.Value != *from {
				return fmt.Errorf("protocol option %s changed since preflight", change.Key)
			}
			if to == nil {
				result := tx.Where(map[string]any{"key": change.Key, "value": row.Value}).Delete(&model.Option{})
				err = result.Error
				if err == nil && result.RowsAffected != 1 {
					return fmt.Errorf("protocol option %s changed during migration", change.Key)
				}
			} else if !exists {
				err = tx.Create(&model.Option{Key: change.Key, Value: *to}).Error
			} else {
				result := tx.Model(&model.Option{}).Where(map[string]any{"key": change.Key, "value": row.Value}).UpdateColumn("value", *to)
				err = result.Error
				if err == nil && result.RowsAffected != 1 {
					return fmt.Errorf("protocol option %s changed during migration", change.Key)
				}
			}
			if err != nil {
				return fmt.Errorf("update protocol option %s failed", change.Key)
			}
		}
		return nil
	})
}
