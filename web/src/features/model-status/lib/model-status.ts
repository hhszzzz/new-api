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
  ModelStatusModel,
  ModelStatusSnapshot,
  ModelStatusTimelinePoint,
} from '../types'

const STATUS_PRIORITY: Record<ModelHealthStatus, number> = {
  failed: 0,
  degraded: 1,
  operational: 2,
  no_data: 3,
}

export type ModelStatusContentKind = 'loading' | 'error' | 'empty' | 'ready'

const HOUR_SECONDS = 60 * 60

export function sortModelStatuses(
  models: ModelStatusModel[]
): ModelStatusModel[] {
  return [...models].sort((left, right) => {
    const statusOrder =
      STATUS_PRIORITY[left.status] - STATUS_PRIORITY[right.status]
    if (statusOrder !== 0) return statusOrder
    return left.model_name.localeCompare(right.model_name)
  })
}

export function getModelStatusContentKind(
  snapshot: ModelStatusSnapshot | undefined,
  isLoading: boolean,
  isError: boolean
): ModelStatusContentKind {
  if (!snapshot) {
    return isLoading && !isError ? 'loading' : 'error'
  }
  return snapshot.models.length === 0 ? 'empty' : 'ready'
}

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
        success_rate: null,
      }
    )
  })
}
