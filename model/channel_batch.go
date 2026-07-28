package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/samber/lo"
	"gorm.io/gorm"
)

const (
	ChannelBatchListReplace = "replace"
	ChannelBatchListAdd     = "add"
	ChannelBatchListRemove  = "remove"
)

type ChannelBatchListUpdate struct {
	Mode   string   `json:"mode"`
	Values []string `json:"values"`
}

type ChannelBatchStringUpdate struct {
	Value string `json:"value"`
}

type ChannelBatchInt64Update struct {
	Value int64 `json:"value"`
}

type ChannelBatchUintUpdate struct {
	Value uint `json:"value"`
}

type ChannelBatchIntUpdate struct {
	Value int `json:"value"`
}

type ChannelBatchBoolUpdate struct {
	Value bool `json:"value"`
}

type ChannelBatchStringListValueUpdate struct {
	Value []string `json:"value"`
}

type ChannelBatchClientPolicyUpdate struct {
	Mode    string   `json:"mode"`
	Clients []string `json:"clients"`
}

type ChannelBatchTimestampUpdate struct {
	Value *int64 `json:"value"`
}

type ChannelBatchWeeklyScheduleUpdate struct {
	Enabled bool                               `json:"enabled"`
	Windows map[string][]ChannelScheduleWindow `json:"windows"`
}

type ChannelBatchUpdate struct {
	Group                              *ChannelBatchListUpdate            `json:"group,omitempty"`
	Priority                           *ChannelBatchInt64Update           `json:"priority,omitempty"`
	Weight                             *ChannelBatchUintUpdate            `json:"weight,omitempty"`
	Tag                                *ChannelBatchStringUpdate          `json:"tag,omitempty"`
	Models                             *ChannelBatchListUpdate            `json:"models,omitempty"`
	ModelMapping                       *ChannelBatchStringUpdate          `json:"model_mapping,omitempty"`
	AutoBan                            *ChannelBatchIntUpdate             `json:"auto_ban,omitempty"`
	TestModel                          *ChannelBatchStringUpdate          `json:"test_model,omitempty"`
	Remark                             *ChannelBatchStringUpdate          `json:"remark,omitempty"`
	StartsAt                           *ChannelBatchTimestampUpdate       `json:"starts_at,omitempty"`
	ExpiresAt                          *ChannelBatchTimestampUpdate       `json:"expires_at,omitempty"`
	PausedUntil                        *ChannelBatchTimestampUpdate       `json:"paused_until,omitempty"`
	WeeklySchedule                     *ChannelBatchWeeklyScheduleUpdate  `json:"weekly_schedule,omitempty"`
	ClientPolicy                       *ChannelBatchClientPolicyUpdate    `json:"client_policy,omitempty"`
	UpstreamModelUpdateCheckEnabled    *ChannelBatchBoolUpdate            `json:"upstream_model_update_check_enabled,omitempty"`
	UpstreamModelUpdateAutoSyncEnabled *ChannelBatchBoolUpdate            `json:"upstream_model_update_auto_sync_enabled,omitempty"`
	UpstreamModelUpdateIgnoredModels   *ChannelBatchStringListValueUpdate `json:"upstream_model_update_ignored_models,omitempty"`
}

func (update ChannelBatchUpdate) Empty() bool {
	return update.Group == nil &&
		update.Priority == nil &&
		update.Weight == nil &&
		update.Tag == nil &&
		update.Models == nil &&
		update.ModelMapping == nil &&
		update.AutoBan == nil &&
		update.TestModel == nil &&
		update.Remark == nil &&
		update.StartsAt == nil &&
		update.ExpiresAt == nil &&
		update.PausedUntil == nil &&
		update.WeeklySchedule == nil &&
		update.ClientPolicy == nil &&
		update.UpstreamModelUpdateCheckEnabled == nil &&
		update.UpstreamModelUpdateAutoSyncEnabled == nil &&
		update.UpstreamModelUpdateIgnoredModels == nil
}

func (update ChannelBatchUpdate) ChangedFields() []string {
	fields := make([]string, 0, 17)
	for _, field := range []struct {
		name    string
		changed bool
	}{
		{name: "group", changed: update.Group != nil},
		{name: "priority", changed: update.Priority != nil},
		{name: "weight", changed: update.Weight != nil},
		{name: "tag", changed: update.Tag != nil},
		{name: "models", changed: update.Models != nil},
		{name: "model_mapping", changed: update.ModelMapping != nil},
		{name: "auto_ban", changed: update.AutoBan != nil},
		{name: "test_model", changed: update.TestModel != nil},
		{name: "remark", changed: update.Remark != nil},
		{name: "starts_at", changed: update.StartsAt != nil},
		{name: "expires_at", changed: update.ExpiresAt != nil},
		{name: "paused_until", changed: update.PausedUntil != nil},
		{name: "weekly_schedule", changed: update.WeeklySchedule != nil},
		{name: "client_policy", changed: update.ClientPolicy != nil},
		{name: "upstream_model_update_check_enabled", changed: update.UpstreamModelUpdateCheckEnabled != nil},
		{name: "upstream_model_update_auto_sync_enabled", changed: update.UpstreamModelUpdateAutoSyncEnabled != nil},
		{name: "upstream_model_update_ignored_models", changed: update.UpstreamModelUpdateIgnoredModels != nil},
	} {
		if field.changed {
			fields = append(fields, field.name)
		}
	}
	return fields
}

type preparedChannelBatchUpdate struct {
	channel         *Channel
	databaseUpdates map[string]any
	rebuildAbility  bool
}

func BatchUpdateChannels(ids []int, update ChannelBatchUpdate) (int, error) {
	if update.Empty() {
		return 0, errors.New("channel batch update has no selected fields")
	}
	ids = normalizeChannelBatchIDs(ids)
	if len(ids) == 0 {
		return 0, errors.New("channel batch update has no target channels")
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		channels := make([]*Channel, 0, len(ids))
		for _, chunk := range lo.Chunk(ids, 200) {
			batch := make([]*Channel, 0, len(chunk))
			if err := lockForUpdate(tx.Model(&Channel{})).
				Where("id IN ?", chunk).
				Order("id ASC").
				Find(&batch).Error; err != nil {
				return err
			}
			channels = append(channels, batch...)
		}
		if len(channels) != len(ids) {
			return errors.New("one or more target channels no longer exist")
		}

		prepared := make([]preparedChannelBatchUpdate, 0, len(channels))
		for _, channel := range channels {
			item, err := prepareChannelBatchUpdate(channel, update)
			if err != nil {
				return fmt.Errorf("channel %d: %w", channel.Id, err)
			}
			prepared = append(prepared, item)
		}

		for _, item := range prepared {
			if err := tx.Model(&Channel{}).
				Where("id = ?", item.channel.Id).
				Updates(item.databaseUpdates).Error; err != nil {
				return err
			}
			if item.rebuildAbility {
				if err := item.channel.UpdateAbilities(tx); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	InitChannelCache()
	return len(ids), nil
}

func normalizeChannelBatchIDs(ids []int) []int {
	normalized := make([]int, 0, len(ids))
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	sort.Ints(normalized)
	return normalized
}

func prepareChannelBatchUpdate(channel *Channel, update ChannelBatchUpdate) (preparedChannelBatchUpdate, error) {
	databaseUpdates := make(map[string]any)
	rebuildAbility := false

	if update.Group != nil {
		value, err := applyChannelBatchList(channel.Group, *update.Group)
		if err != nil {
			return preparedChannelBatchUpdate{}, fmt.Errorf("invalid group update: %w", err)
		}
		if len(value) > 64 {
			return preparedChannelBatchUpdate{}, errors.New("combined group value exceeds 64 characters")
		}
		channel.Group = value
		databaseUpdates["group"] = value
		rebuildAbility = true
	}
	if update.Models != nil {
		value, err := applyChannelBatchList(channel.Models, *update.Models)
		if err != nil {
			return preparedChannelBatchUpdate{}, fmt.Errorf("invalid models update: %w", err)
		}
		for _, modelName := range strings.Split(value, ",") {
			if len(modelName) > 255 {
				return preparedChannelBatchUpdate{}, fmt.Errorf("model name exceeds 255 characters: %s", modelName)
			}
		}
		channel.Models = value
		databaseUpdates["models"] = value
		rebuildAbility = true
	}
	if update.Priority != nil {
		value := update.Priority.Value
		channel.Priority = &value
		databaseUpdates["priority"] = value
		rebuildAbility = true
	}
	if update.Weight != nil {
		value := update.Weight.Value
		channel.Weight = &value
		databaseUpdates["weight"] = value
		rebuildAbility = true
	}
	if update.Tag != nil {
		value := strings.TrimSpace(update.Tag.Value)
		channel.Tag = &value
		databaseUpdates["tag"] = value
		rebuildAbility = true
	}
	if update.ModelMapping != nil {
		value := strings.TrimSpace(update.ModelMapping.Value)
		if value != "" {
			if common.GetJsonType([]byte(value)) != "object" {
				return preparedChannelBatchUpdate{}, errors.New("model mapping must be a JSON object of string values")
			}
			mapping := make(map[string]string)
			if err := common.Unmarshal([]byte(value), &mapping); err != nil {
				return preparedChannelBatchUpdate{}, fmt.Errorf("model mapping must be a JSON object of string values: %w", err)
			}
		}
		channel.ModelMapping = &value
		databaseUpdates["model_mapping"] = value
	}
	if update.AutoBan != nil {
		if update.AutoBan.Value != 0 && update.AutoBan.Value != 1 {
			return preparedChannelBatchUpdate{}, errors.New("auto_ban must be 0 or 1")
		}
		value := update.AutoBan.Value
		channel.AutoBan = &value
		databaseUpdates["auto_ban"] = value
	}
	if update.TestModel != nil {
		value := strings.TrimSpace(update.TestModel.Value)
		if len(value) > 255 {
			return preparedChannelBatchUpdate{}, errors.New("test model exceeds 255 characters")
		}
		channel.TestModel = &value
		databaseUpdates["test_model"] = value
	}
	if update.Remark != nil {
		value := strings.TrimSpace(update.Remark.Value)
		if utf8.RuneCountInString(value) > 255 {
			return preparedChannelBatchUpdate{}, errors.New("remark exceeds 255 characters")
		}
		channel.Remark = &value
		databaseUpdates["remark"] = value
	}

	scheduleChanged := false
	if update.StartsAt != nil {
		channel.Schedule.StartsAt = update.StartsAt.Value
		scheduleChanged = true
	}
	if update.ExpiresAt != nil {
		channel.Schedule.ExpiresAt = update.ExpiresAt.Value
		scheduleChanged = true
	}
	if update.PausedUntil != nil {
		channel.Schedule.PausedUntil = update.PausedUntil.Value
		scheduleChanged = true
	}
	if update.WeeklySchedule != nil {
		channel.Schedule.WeeklyEnabled = update.WeeklySchedule.Enabled
		channel.Schedule.WeeklyWindows = update.WeeklySchedule.Windows
		scheduleChanged = true
	}
	if scheduleChanged {
		if err := channel.Schedule.Normalize(); err != nil {
			return preparedChannelBatchUpdate{}, err
		}
		databaseUpdates["schedule"] = channel.Schedule
	}

	settingsChanged := update.ClientPolicy != nil ||
		update.UpstreamModelUpdateCheckEnabled != nil ||
		update.UpstreamModelUpdateAutoSyncEnabled != nil ||
		update.UpstreamModelUpdateIgnoredModels != nil
	if settingsChanged {
		settingsRecord := make(map[string]any)
		rawSettings := strings.TrimSpace(channel.OtherSettings)
		if rawSettings != "" {
			if common.GetJsonType([]byte(rawSettings)) != "object" {
				return preparedChannelBatchUpdate{}, errors.New("channel settings must be a JSON object")
			}
			if err := common.UnmarshalJsonStr(rawSettings, &settingsRecord); err != nil {
				return preparedChannelBatchUpdate{}, fmt.Errorf("invalid channel settings: %w", err)
			}
		}

		if update.ClientPolicy != nil {
			mode := strings.ToLower(strings.TrimSpace(update.ClientPolicy.Mode))
			switch mode {
			case operation_setting.ClientPolicyModeUnrestricted,
				operation_setting.ClientPolicyModeAllow,
				operation_setting.ClientPolicyModeDeny:
			default:
				return preparedChannelBatchUpdate{}, errors.New("invalid client policy mode")
			}

			clients := make([]string, 0, len(update.ClientPolicy.Clients))
			seenClients := make(map[string]struct{}, len(update.ClientPolicy.Clients))
			for _, client := range update.ClientPolicy.Clients {
				client = strings.ToLower(strings.TrimSpace(client))
				if client == "" {
					continue
				}
				if _, exists := seenClients[client]; exists {
					continue
				}
				seenClients[client] = struct{}{}
				clients = append(clients, client)
			}
			policy := operation_setting.ClientAccessPolicy{Mode: mode, Clients: clients}
			if err := operation_setting.ValidateClientAccessPolicy(policy); err != nil {
				return preparedChannelBatchUpdate{}, fmt.Errorf("invalid channel client policy: %w", err)
			}
			if mode == operation_setting.ClientPolicyModeUnrestricted {
				delete(settingsRecord, "client_policy")
			} else {
				settingsRecord["client_policy"] = policy
			}
		}
		if update.UpstreamModelUpdateCheckEnabled != nil {
			settingsRecord["upstream_model_update_check_enabled"] = update.UpstreamModelUpdateCheckEnabled.Value
		}
		if update.UpstreamModelUpdateAutoSyncEnabled != nil {
			settingsRecord["upstream_model_update_auto_sync_enabled"] = update.UpstreamModelUpdateAutoSyncEnabled.Value
		}
		if update.UpstreamModelUpdateIgnoredModels != nil {
			settingsRecord["upstream_model_update_ignored_models"] = normalizeChannelBatchListValues(
				update.UpstreamModelUpdateIgnoredModels.Value,
			)
		}

		settingsBytes, err := common.Marshal(settingsRecord)
		if err != nil {
			return preparedChannelBatchUpdate{}, err
		}
		channel.OtherSettings = string(settingsBytes)
		if err := channel.ValidateSettings(); err != nil {
			return preparedChannelBatchUpdate{}, err
		}
		databaseUpdates["settings"] = channel.OtherSettings
	}

	return preparedChannelBatchUpdate{
		channel:         channel,
		databaseUpdates: databaseUpdates,
		rebuildAbility:  rebuildAbility,
	}, nil
}

func applyChannelBatchList(existing string, update ChannelBatchListUpdate) (string, error) {
	current := normalizeChannelBatchListValues([]string{existing})
	values := normalizeChannelBatchListValues(update.Values)
	if len(values) == 0 {
		return "", errors.New("values cannot be empty")
	}

	switch update.Mode {
	case ChannelBatchListReplace:
		current = values
	case ChannelBatchListAdd:
		seen := make(map[string]struct{}, len(current)+len(values))
		for _, value := range current {
			seen[value] = struct{}{}
		}
		for _, value := range values {
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			current = append(current, value)
		}
	case ChannelBatchListRemove:
		removed := make(map[string]struct{}, len(values))
		for _, value := range values {
			removed[value] = struct{}{}
		}
		current = lo.Filter(current, func(value string, _ int) bool {
			_, found := removed[value]
			return !found
		})
	default:
		return "", fmt.Errorf("unsupported mode %q", update.Mode)
	}
	if len(current) == 0 {
		return "", errors.New("operation would leave the field empty")
	}
	return strings.Join(current, ","), nil
}

func normalizeChannelBatchListValues(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, input := range values {
		for _, value := range strings.Split(input, ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			normalized = append(normalized, value)
		}
	}
	return normalized
}
