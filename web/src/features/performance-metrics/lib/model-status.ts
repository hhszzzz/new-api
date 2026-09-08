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
  ModelHealthStatus,
  ModelStatusTimelinePoint,
} from '../status-types'

export const STATUS_PRIORITY: Record<ModelHealthStatus, number> = {
  failed: 0,
  degraded: 1,
  operational: 2,
  no_data: 3,
}

const HOUR_SECONDS = 60 * 60

export function normalizeStatusTimeline(
  timeline: ModelStatusTimelinePoint[],
  generatedAt: number,
  windowHours = 24
): ModelStatusTimelinePoint[] {
  const safeGeneratedAt = Number.isFinite(generatedAt)
    ? generatedAt
    : Math.floor(Date.now() / 1000)
  const currentHour = Math.floor(safeGeneratedAt / HOUR_SECONDS) * HOUR_SECONDS
  const pointsByHour = new Map<number, ModelStatusTimelinePoint>()

  for (const point of timeline) {
    const hour = Math.floor(point.ts / HOUR_SECONDS) * HOUR_SECONDS
    pointsByHour.set(hour, { ...point, ts: hour })
  }

  return Array.from({ length: windowHours }, (_, index) => {
    const ts = currentHour - (windowHours - index - 1) * HOUR_SECONDS
    return (
      pointsByHour.get(ts) ?? {
        ts,
        status: 'no_data',
        request_count: 0,
        success_count: 0,
        success_rate: null,
        avg_ttft_ms: null,
        avg_latency_ms: null,
        avg_tps: null,
      }
    )
  })
}
