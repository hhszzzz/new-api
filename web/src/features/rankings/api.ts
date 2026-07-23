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
import { api } from '@/lib/api'

import type { RankingPeriod, RankingsQuery, RankingsSnapshot } from './types'

type RankingsResponse = {
  success: boolean
  message?: string
  data: RankingsSnapshot
}

export async function getRankings(
  query: RankingsQuery | RankingPeriod
): Promise<RankingsResponse> {
  const normalized: RankingsQuery =
    typeof query === 'string' ? { period: query } : query
  const params: Record<string, number | string> = {
    period: normalized.period,
  }
  if (normalized.period === 'custom') {
    if (normalized.startTimestamp !== undefined) {
      params.start_timestamp = normalized.startTimestamp
    }
    if (normalized.endTimestamp !== undefined) {
      params.end_timestamp = normalized.endTimestamp
    }
  }
  const res = await api.get('/api/rankings', { params })
  return res.data
}
