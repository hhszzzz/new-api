/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type {
  RankingUserGroup,
  RankingUserModel,
  RankingUserUsage,
} from '../types'

export type RankingBreakdownMode = 'group' | 'model'

export type RankingPieSlice = {
  key: string
  name: string
  userRank?: number
  quota: number
  usd: number
  share: number
  isOther: boolean
  groups: RankingUserGroup[]
  models: RankingUserModel[]
}

export function buildRankingPieSlices(
  usage: RankingUserUsage,
  limit = 5,
  otherLabel = 'Other'
): RankingPieSlice[] {
  const users = [...usage.users]
    .filter((user) => Number.isFinite(user.total_quota) && user.total_quota > 0)
    .sort((a, b) => b.total_quota - a.total_quota || a.rank - b.rank)
  const top = users.slice(0, Math.max(0, Math.floor(limit)))
  const topQuota = top.reduce((sum, user) => sum + user.total_quota, 0)
  const reportedTotal =
    Number.isFinite(usage.total_quota) && usage.total_quota > 0
      ? usage.total_quota
      : 0
  const totalQuota = Math.max(reportedTotal, topQuota)
  const remainder = totalQuota - topQuota
  let allocatedShare = 0
  const slices: RankingPieSlice[] = top.map((user) => ({
    key: `user-${user.rank}`,
    name: user.username,
    userRank: user.rank,
    quota: user.total_quota,
    usd: user.total_usd,
    share: 0,
    isOther: false,
    groups: user.groups,
    models: user.models ?? [],
  }))
  for (let index = 0; index < slices.length; index += 1) {
    const isLastSlice = index === slices.length - 1 && remainder === 0
    slices[index].share = isLastSlice
      ? Math.max(0, 1 - allocatedShare)
      : slices[index].quota / totalQuota
    allocatedShare += slices[index].share
  }
  if (remainder > 0) {
    const topUSD = top.reduce(
      (sum, user) =>
        sum +
        (Number.isFinite(user.total_usd) && user.total_usd > 0
          ? user.total_usd
          : 0),
      0
    )
    const totalUSD =
      Number.isFinite(usage.total_usd) && usage.total_usd > 0
        ? usage.total_usd
        : 0
    slices.push({
      key: 'other',
      name: otherLabel,
      quota: remainder,
      usd: Math.max(0, totalUSD - topUSD),
      share: Math.max(0, 1 - allocatedShare),
      isOther: true,
      groups: [],
      models: [],
    })
  }
  return slices
}

export type RankingBreakdownRow = {
  label: string
  tokens: number
  usd: number
}

// The hover tooltip card and the ranked-row popover list one usage breakdown
// per row; the mode picks whether rows are keyed by request group or by model.
export function rankingBreakdownRows(
  breakdown: Pick<RankingPieSlice, 'groups' | 'models'>,
  mode: RankingBreakdownMode
): RankingBreakdownRow[] {
  if (mode === 'group') {
    return breakdown.groups.map((group) => ({
      label: group.use_group,
      tokens: group.total_tokens,
      usd: group.total_usd,
    }))
  }
  return breakdown.models.map((model) => ({
    label: model.model_name,
    tokens: model.total_tokens,
    usd: model.total_usd,
  }))
}
