import type { RankingUser, RankingUserGroup, RankingUserUsage } from '../types'
import { formatShare, formatUSD, formatUsageColumns } from './format'

export type RankingPieSlice = {
  key: string
  name: string
  userRank?: number
  quota: number
  usd: number
  share: number
  isOther: boolean
  groups: RankingUserGroup[]
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
    })
  }
  return slices
}

export function formatRankingUserTooltip(
  slice: RankingPieSlice,
  translate: (key: string) => string
): string {
  const summary = `${formatShare(slice.share)} · ${formatUSD(slice.usd)}`
  if (slice.groups.length === 0) return summary

  // Group rows are padded as one set, so their charge and token columns line up
  // under each other instead of drifting with each group's own value widths.
  const columns = formatUsageColumns(
    slice.groups.map((group) => ({
      share: group.quota_share,
      tokens: group.total_tokens,
      usd: group.total_usd,
    }))
  )
  const groups = slice.groups
    .map((group, index) => `${group.use_group}: ${columns[index]}`)
    .join('\n')
  return `${summary}\n${translate('Usage by group')}:\n${groups}`
}

export function findRankingUser(
  usage: RankingUserUsage | undefined,
  rank: number | undefined
): RankingUser | undefined {
  if (!usage || rank === undefined) return undefined
  return usage.users.find((user) => user.rank === rank)
}
