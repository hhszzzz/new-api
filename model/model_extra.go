package model

func GetModelEnableGroups(modelName string) []string {
	if modelName == "" {
		return make([]string, 0)
	}
	for {
		updatePricingLock.RLock()
		if pricingCacheFreshLocked() {
			modelEnableGroupsLock.RLock()
			groups := make([]string, len(modelEnableGroups[modelName]))
			copy(groups, modelEnableGroups[modelName])
			modelEnableGroupsLock.RUnlock()
			updatePricingLock.RUnlock()
			return groups
		}
		updatePricingLock.RUnlock()

		if ensurePricingCache() {
			continue
		}

		updatePricingLock.RLock()
		modelEnableGroupsLock.RLock()
		groups := make([]string, len(modelEnableGroups[modelName]))
		copy(groups, modelEnableGroups[modelName])
		modelEnableGroupsLock.RUnlock()
		updatePricingLock.RUnlock()
		return groups
	}
}

// GetModelQuotaTypes 返回指定模型的计费类型集合（来自缓存）
func GetModelQuotaTypes(modelName string) []int {
	for {
		updatePricingLock.RLock()
		if pricingCacheFreshLocked() {
			modelEnableGroupsLock.RLock()
			quota, ok := modelQuotaTypeMap[modelName]
			modelEnableGroupsLock.RUnlock()
			updatePricingLock.RUnlock()
			if !ok {
				return []int{}
			}
			return []int{quota}
		}
		updatePricingLock.RUnlock()

		if ensurePricingCache() {
			continue
		}

		updatePricingLock.RLock()
		modelEnableGroupsLock.RLock()
		quota, ok := modelQuotaTypeMap[modelName]
		modelEnableGroupsLock.RUnlock()
		updatePricingLock.RUnlock()
		if !ok {
			return []int{}
		}
		return []int{quota}
	}
}
