package model

import "sort"

func quotaVisibleModelSet(modelNames []string) map[string]struct{} {
	visible := make(map[string]struct{}, len(modelNames))
	for _, modelName := range modelNames {
		if modelName != "" {
			visible[modelName] = struct{}{}
		}
	}
	return visible
}

func selfQuotaModelName(modelName string, modelScope int, canViewPrivate bool) string {
	if canViewPrivate {
		return modelName
	}
	if modelScope == QuotaModelScopeRequested {
		return modelName
	}
	return ""
}

func rankingQuotaModelName(modelName string, modelScope int, visibleModels map[string]struct{}, canViewPrivate bool) string {
	if canViewPrivate {
		return modelName
	}
	if modelScope != QuotaModelScopeRequested {
		return ""
	}
	if _, ok := visibleModels[modelName]; ok {
		return modelName
	}
	return ""
}

func sanitizeSelfQuotaData(rows []*QuotaData, canViewPrivate bool) []*QuotaData {
	type aggregateKey struct {
		userID    int
		username  string
		modelName string
		createdAt int64
	}
	aggregated := make(map[aggregateKey]*QuotaData, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		modelName := selfQuotaModelName(row.ModelName, row.ModelScope, canViewPrivate)
		key := aggregateKey{
			userID:    row.UserID,
			username:  row.Username,
			modelName: modelName,
			createdAt: row.CreatedAt,
		}
		current, ok := aggregated[key]
		if !ok {
			copyRow := *row
			copyRow.ModelName = modelName
			copyRow.ModelScope = QuotaModelScopeLegacy
			aggregated[key] = &copyRow
			continue
		}
		current.Count += row.Count
		current.Quota += row.Quota
		current.TokenUsed += row.TokenUsed
	}

	result := make([]*QuotaData, 0, len(aggregated))
	for _, row := range aggregated {
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt != result[j].CreatedAt {
			return result[i].CreatedAt < result[j].CreatedAt
		}
		return result[i].ModelName < result[j].ModelName
	})
	return result
}

func sanitizeSelfFlowQuotaData(rows []*FlowQuotaData, canViewPrivate bool) []*FlowQuotaData {
	type aggregateKey struct {
		tokenID   int
		useGroup  string
		modelName string
	}
	aggregated := make(map[aggregateKey]*FlowQuotaData, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		modelName := selfQuotaModelName(row.ModelName, row.ModelScope, canViewPrivate)
		key := aggregateKey{
			tokenID:   row.TokenID,
			useGroup:  row.UseGroup,
			modelName: modelName,
		}
		current, ok := aggregated[key]
		if !ok {
			copyRow := *row
			copyRow.ModelName = modelName
			copyRow.ModelScope = QuotaModelScopeLegacy
			aggregated[key] = &copyRow
			continue
		}
		current.Count += row.Count
		current.Quota += row.Quota
		current.TokenUsed += row.TokenUsed
	}

	result := make([]*FlowQuotaData, 0, len(aggregated))
	for _, row := range aggregated {
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Quota != result[j].Quota {
			return result[i].Quota > result[j].Quota
		}
		if result[i].ModelName != result[j].ModelName {
			return result[i].ModelName < result[j].ModelName
		}
		if result[i].UseGroup != result[j].UseGroup {
			return result[i].UseGroup < result[j].UseGroup
		}
		return result[i].TokenID < result[j].TokenID
	})
	return result
}

func mergeAdminFlowQuotaData(rows []*FlowQuotaData) []*FlowQuotaData {
	type aggregateKey struct {
		userID    int
		username  string
		useGroup  string
		modelName string
		channelID int
	}
	aggregated := make(map[aggregateKey]*FlowQuotaData, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		key := aggregateKey{
			userID:    row.UserID,
			username:  row.Username,
			useGroup:  row.UseGroup,
			modelName: row.ModelName,
			channelID: row.ChannelID,
		}
		current, ok := aggregated[key]
		if !ok {
			copyRow := *row
			copyRow.ModelScope = QuotaModelScopeLegacy
			aggregated[key] = &copyRow
			continue
		}
		current.Count += row.Count
		current.Quota += row.Quota
		current.TokenUsed += row.TokenUsed
	}
	return sortedFlowQuotaData(aggregated)
}

func mergeRootFlowQuotaData(rows []*FlowQuotaData) []*FlowQuotaData {
	type aggregateKey struct {
		userID    int
		username  string
		nodeName  string
		tokenID   int
		useGroup  string
		modelName string
		channelID int
	}
	aggregated := make(map[aggregateKey]*FlowQuotaData, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		key := aggregateKey{
			userID:    row.UserID,
			username:  row.Username,
			nodeName:  row.NodeName,
			tokenID:   row.TokenID,
			useGroup:  row.UseGroup,
			modelName: row.ModelName,
			channelID: row.ChannelID,
		}
		current, ok := aggregated[key]
		if !ok {
			copyRow := *row
			copyRow.ModelScope = QuotaModelScopeLegacy
			aggregated[key] = &copyRow
			continue
		}
		current.Count += row.Count
		current.Quota += row.Quota
		current.TokenUsed += row.TokenUsed
	}
	return sortedFlowQuotaData(aggregated)
}

func sortedFlowQuotaData[K comparable](aggregated map[K]*FlowQuotaData) []*FlowQuotaData {
	result := make([]*FlowQuotaData, 0, len(aggregated))
	for _, row := range aggregated {
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Quota != result[j].Quota {
			return result[i].Quota > result[j].Quota
		}
		if result[i].ModelName != result[j].ModelName {
			return result[i].ModelName < result[j].ModelName
		}
		if result[i].Username != result[j].Username {
			return result[i].Username < result[j].Username
		}
		if result[i].UseGroup != result[j].UseGroup {
			return result[i].UseGroup < result[j].UseGroup
		}
		return result[i].ChannelID < result[j].ChannelID
	})
	return result
}

func sanitizeRankingQuotaTotals(rows []RankingQuotaTotal, visibleModelNames []string, canViewPrivate bool) []RankingQuotaTotal {
	visibleModels := quotaVisibleModelSet(visibleModelNames)
	totals := make(map[string]int64, len(rows))
	for _, row := range rows {
		modelName := rankingQuotaModelName(row.ModelName, row.ModelScope, visibleModels, canViewPrivate)
		totals[modelName] += row.TotalTokens
	}
	result := make([]RankingQuotaTotal, 0, len(totals))
	for modelName, totalTokens := range totals {
		result = append(result, RankingQuotaTotal{
			ModelName:   modelName,
			TotalTokens: totalTokens,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].TotalTokens != result[j].TotalTokens {
			return result[i].TotalTokens > result[j].TotalTokens
		}
		return result[i].ModelName < result[j].ModelName
	})
	return result
}

func sanitizeRankingQuotaBuckets(rows []RankingQuotaBucket, visibleModelNames []string, canViewPrivate bool) []RankingQuotaBucket {
	visibleModels := quotaVisibleModelSet(visibleModelNames)
	type aggregateKey struct {
		modelName string
		bucket    int64
	}
	tokens := make(map[aggregateKey]int64, len(rows))
	for _, row := range rows {
		modelName := rankingQuotaModelName(row.ModelName, row.ModelScope, visibleModels, canViewPrivate)
		tokens[aggregateKey{modelName: modelName, bucket: row.Bucket}] += row.Tokens
	}
	result := make([]RankingQuotaBucket, 0, len(tokens))
	for key, bucketTokens := range tokens {
		result = append(result, RankingQuotaBucket{
			ModelName: key.modelName,
			Bucket:    key.bucket,
			Tokens:    bucketTokens,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Bucket != result[j].Bucket {
			return result[i].Bucket < result[j].Bucket
		}
		return result[i].ModelName < result[j].ModelName
	})
	return result
}
