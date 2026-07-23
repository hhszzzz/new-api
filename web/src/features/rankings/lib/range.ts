import type { RankingPeriod, RankingsQuery } from '../types'

export const MAX_RANKING_CUSTOM_DAYS = 366

export type RankingDateRange = {
  from: Date
  to?: Date
}

export function startOfLocalDayTimestamp(date: Date): number {
  return Math.floor(
    new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime() /
      1000
  )
}

export function endOfLocalDayTimestamp(date: Date): number {
  return Math.floor(
    new Date(
      date.getFullYear(),
      date.getMonth(),
      date.getDate(),
      23,
      59,
      59,
      999
    ).getTime() / 1000
  )
}

export function normalizeRankingDateRange(
  range: RankingDateRange | undefined,
  now = new Date()
): RankingsQuery | null {
  if (!range?.from || !range.to) return null
  const startTimestamp = startOfLocalDayTimestamp(range.from)
  const endTimestamp = endOfLocalDayTimestamp(range.to)
  const maxSeconds = MAX_RANKING_CUSTOM_DAYS * 24 * 60 * 60
  if (
    endTimestamp < startTimestamp ||
    endTimestamp - startTimestamp + 1 > maxSeconds
  ) {
    return null
  }
  if (startTimestamp > Math.floor(now.getTime() / 1000)) return null
  return {
    period: 'custom',
    startTimestamp,
    endTimestamp,
  }
}

export function defaultRankingDateRange(now = new Date()): RankingDateRange {
  const to = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const from = new Date(to)
  from.setDate(from.getDate() - 6)
  return { from, to }
}

export function rankingSearchForPeriod(
  period: RankingPeriod,
  range?: RankingDateRange
): RankingsQuery | null {
  if (period !== 'custom') return { period }
  return normalizeRankingDateRange(range)
}

export function rankingQueryFromSearch(search: {
  period?: RankingPeriod
  start_timestamp?: number
  end_timestamp?: number
}): RankingsQuery {
  const period = search.period ?? 'week'
  if (
    period === 'custom' &&
    search.start_timestamp !== undefined &&
    search.end_timestamp !== undefined
  ) {
    return {
      period,
      startTimestamp: search.start_timestamp,
      endTimestamp: search.end_timestamp,
    }
  }
  return { period }
}

export function datesFromRankingQuery(
  query: RankingsQuery
): RankingDateRange | undefined {
  if (
    query.period !== 'custom' ||
    query.startTimestamp === undefined ||
    query.endTimestamp === undefined
  ) {
    return undefined
  }
  return {
    from: new Date(query.startTimestamp * 1000),
    to: new Date(query.endTimestamp * 1000),
  }
}
