package model

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/account_pool_setting"
)

func UpdateAccountPoolSetting(setting account_pool_setting.Setting) error {
	prepared, err := account_pool_setting.PrepareSetting(setting)
	if err != nil {
		return err
	}
	groupsJSON, err := common.Marshal(prepared.AllowedGroups)
	if err != nil {
		return err
	}

	return UpdateOptionsBulk(map[string]string{
		account_pool_setting.EnabledOptionKey:                   strconv.FormatBool(prepared.Enabled),
		account_pool_setting.HideEmailFromNonAdminsOptionKey:    strconv.FormatBool(prepared.HideEmailFromNonAdmins),
		account_pool_setting.AllowedGroupsOptionKey:             string(groupsJSON),
		account_pool_setting.RegularRefreshSecondsOptionKey:     strconv.Itoa(prepared.RegularRefreshSeconds),
		account_pool_setting.NearResetThresholdSecondsOptionKey: strconv.Itoa(prepared.NearResetThresholdSeconds),
		account_pool_setting.NearResetRefreshSecondsOptionKey:   strconv.Itoa(prepared.NearResetRefreshSeconds),
		account_pool_setting.PostResetDelaySecondsOptionKey:     strconv.Itoa(prepared.PostResetDelaySeconds),
		account_pool_setting.ManualRefreshCooldownOptionKey:     strconv.Itoa(prepared.ManualRefreshCooldown),
	})
}
