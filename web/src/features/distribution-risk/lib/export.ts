import { downloadCsv, toCsvLine } from '@/features/usage-logs/lib/export'
import { formatTimestampToDate } from '@/lib/format'

import type { DistributionRiskEvent, DistributionRiskEventList } from '../types'
import { distributionRiskTokenLabel } from './format'

type Translate = (key: string) => string
type FetchPage = (
  page: number,
  pageSize: number,
  signal: AbortSignal
) => Promise<DistributionRiskEventList>

const EXPORT_PAGE_SIZE = 100

function csvHeader(t: Translate): string[] {
  return [
    t('Observed at'),
    t('User'),
    t('Token'),
    t('Risk score'),
    t('Risk level'),
    t('Status'),
    t('Signal scores'),
    t('Reviewed at'),
    t('Reviewer ID'),
    t('Review reason'),
  ]
}

function categoryLabel(category: string): string {
  switch (category) {
    case 'network':
      return 'Network'
    case 'client':
      return 'Client'
    case 'session':
      return 'Session'
    default:
      return 'Activity'
  }
}

function statusLabel(status: DistributionRiskEvent['status']): string {
  if (status === 'confirmed') return 'Confirmed'
  if (status === 'false_positive') return 'False positive'
  return 'Pending'
}

export async function fetchAllDistributionRiskEvents(
  fetchPage: FetchPage,
  signal: AbortSignal,
  onProgress: (count: number) => void
): Promise<DistributionRiskEvent[]> {
  signal.throwIfAborted()
  const firstPage = await fetchPage(1, EXPORT_PAGE_SIZE, signal)
  const pageCount = Math.max(1, Math.ceil(firstPage.total / EXPORT_PAGE_SIZE))
  const events: DistributionRiskEvent[] = []
  const seenIDs = new Set<number>()

  for (let page = 1; page <= pageCount; page++) {
    signal.throwIfAborted()
    const result =
      page === 1 ? firstPage : await fetchPage(page, EXPORT_PAGE_SIZE, signal)
    signal.throwIfAborted()
    for (const event of result.items) {
      if (seenIDs.has(event.id)) continue
      seenIDs.add(event.id)
      events.push(event)
    }
    onProgress(events.length)
  }

  return events
}

export function buildDistributionRiskCsv(
  events: DistributionRiskEvent[],
  t: Translate
): string[] {
  const lines = [toCsvLine(csvHeader(t))]

  for (const event of events) {
    const categoryScores = Object.entries(event.category_scores ?? {})
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([category, score]) => `${t(categoryLabel(category))}: ${score}`)
      .join('; ')
    lines.push(
      toCsvLine([
        formatTimestampToDate(event.created_at),
        event.username || '',
        event.tokens
          .map((token) => distributionRiskTokenLabel(token, true))
          .join(', '),
        String(event.score),
        t(event.level === 'high' ? 'High' : 'Medium'),
        t(statusLabel(event.status)),
        categoryScores,
        event.reviewed_at > 0 ? formatTimestampToDate(event.reviewed_at) : '',
        event.reviewer_id > 0 ? String(event.reviewer_id) : '',
        event.review_reason ?? '',
      ])
    )
  }

  return lines
}

export { downloadCsv }
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
