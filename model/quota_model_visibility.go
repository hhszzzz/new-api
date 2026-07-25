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
	totals := make(map[string]*RankingQuotaTotal, len(rows))
	for _, row := range rows {
		modelName := rankingQuotaModelName(row.ModelName, row.ModelScope, visibleModels, canViewPrivate)
		aggregate, ok := totals[modelName]
		if !ok {
			aggregate = &RankingQuotaTotal{ModelName: modelName}
			totals[modelName] = aggregate
		}
		aggregate.TotalTokens += row.TotalTokens
		aggregate.TotalQuota += row.TotalQuota
	}
	result := make([]RankingQuotaTotal, 0, len(totals))
	for _, aggregate := range totals {
		result = append(result, *aggregate)
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
	totals := make(map[aggregateKey]*RankingQuotaBucket, len(rows))
	for _, row := range rows {
		modelName := rankingQuotaModelName(row.ModelName, row.ModelScope, visibleModels, canViewPrivate)
		key := aggregateKey{modelName: modelName, bucket: row.Bucket}
		aggregate, ok := totals[key]
		if !ok {
			aggregate = &RankingQuotaBucket{ModelName: modelName, Bucket: row.Bucket}
			totals[key] = aggregate
		}
		aggregate.Tokens += row.Tokens
		aggregate.Quota += row.Quota
	}
	result := make([]RankingQuotaBucket, 0, len(totals))
	for _, aggregate := range totals {
		result = append(result, *aggregate)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Bucket != result[j].Bucket {
			return result[i].Bucket < result[j].Bucket
		}
		return result[i].ModelName < result[j].ModelName
	})
	return result
}

// sanitizeRankingUserQuotaRows applies the same model-scope visibility rules
// as the public model leaderboard. Hidden rows are marked and merged by user
// and request group so callers can preserve total usage without exposing a
// private model's provenance.
func sanitizeRankingUserQuotaRows(rows []RankingUserQuotaRow, visibleModelNames []string, canViewPrivate bool) []RankingUserQuotaRow {
	visibleModels := quotaVisibleModelSet(visibleModelNames)
	type aggregateKey struct {
		userID      int
		username    string
		useGroup    string
		hiddenModel bool
	}
	aggregated := make(map[aggregateKey]*RankingUserQuotaRow, len(rows))
	for _, row := range rows {
		modelName := rankingQuotaModelName(row.ModelName, row.ModelScope, visibleModels, canViewPrivate)
		hidden := !canViewPrivate && modelName == ""
		key := aggregateKey{
			userID:      row.UserID,
			username:    row.Username,
			useGroup:    row.UseGroup,
			hiddenModel: hidden,
		}
		aggregate, ok := aggregated[key]
		if !ok {
			aggregate = &RankingUserQuotaRow{
				UserID:      row.UserID,
				Username:    row.Username,
				UseGroup:    row.UseGroup,
				ModelName:   modelName,
				HiddenModel: hidden,
			}
			aggregated[key] = aggregate
		}
		aggregate.TotalTokens += row.TotalTokens
		aggregate.TotalQuota += row.TotalQuota
	}

	result := make([]RankingUserQuotaRow, 0, len(aggregated))
	for _, aggregate := range aggregated {
		result = append(result, *aggregate)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].TotalQuota != result[j].TotalQuota {
			return result[i].TotalQuota > result[j].TotalQuota
		}
		if result[i].TotalTokens != result[j].TotalTokens {
			return result[i].TotalTokens > result[j].TotalTokens
		}
		if result[i].Username != result[j].Username {
			return result[i].Username < result[j].Username
		}
		if result[i].UseGroup != result[j].UseGroup {
			return result[i].UseGroup < result[j].UseGroup
		}
		return !result[i].HiddenModel && result[j].HiddenModel
	})
	return result
}
