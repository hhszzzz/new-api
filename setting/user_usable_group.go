package setting

import (
	"maps"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// The default descriptions are English source keys that the web console
// translates; descriptions an administrator saves are shown as written.
var userUsableGroups = map[string]string{
	"default": "Default group",
	"vip":     "VIP group",
}
var userUsableGroupsMutex sync.RWMutex

func GetUserUsableGroupsCopy() map[string]string {
	userUsableGroupsMutex.RLock()
	defer userUsableGroupsMutex.RUnlock()

	copyUserUsableGroups := make(map[string]string)
	maps.Copy(copyUserUsableGroups, userUsableGroups)
	return copyUserUsableGroups
}

func UserUsableGroups2JSONString() string {
	userUsableGroupsMutex.RLock()
	defer userUsableGroupsMutex.RUnlock()

	jsonBytes, err := common.Marshal(userUsableGroups)
	if err != nil {
		common.SysLog(common.LogText("error marshalling user groups: %s", err.Error()))
	}
	return string(jsonBytes)
}

func UpdateUserUsableGroupsByJSONString(jsonStr string) error {
	parsed := make(map[string]string)
	if err := common.UnmarshalJsonStr(jsonStr, &parsed); err != nil {
		return err
	}
	userUsableGroupsMutex.Lock()
	userUsableGroups = parsed
	userUsableGroupsMutex.Unlock()
	return nil
}

func GetUsableGroupDescription(groupName string) string {
	userUsableGroupsMutex.RLock()
	defer userUsableGroupsMutex.RUnlock()

	if desc, ok := userUsableGroups[groupName]; ok {
		return desc
	}
	return groupName
}
