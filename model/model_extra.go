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

// GetModelContextLimit 返回指定模型声明的上下文上限（来自定价缓存）；
// 未声明或模型不在缓存中时返回 0, false。
func GetModelContextLimit(modelName string) (int, bool) {
	if modelName == "" {
		return 0, false
	}
	for {
		updatePricingLock.RLock()
		if pricingCacheFreshLocked() {
			modelContextLimitsLock.RLock()
			limit, ok := modelContextLimits[modelName]
			modelContextLimitsLock.RUnlock()
			updatePricingLock.RUnlock()
			return limit, ok
		}
		updatePricingLock.RUnlock()

		if ensurePricingCache() {
			continue
		}

		updatePricingLock.RLock()
		modelContextLimitsLock.RLock()
		limit, ok := modelContextLimits[modelName]
		modelContextLimitsLock.RUnlock()
		updatePricingLock.RUnlock()
		return limit, ok
	}
}
