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
  ModelRadarConfiguration,
  ModelRadarHistoryFrame,
  ModelRadarHistoryPoint,
  RadarMetric,
} from '../types'

export const EFFORT_ORDER = [
  'low',
  'medium',
  'high',
  'xhigh',
  'max',
  'ultra',
] as const

export const MODEL_COLORS = [
  '#2563eb',
  '#e11d48',
  '#059669',
  '#d97706',
  '#7c3aed',
  '#0891b2',
  '#db2777',
  '#4f46e5',
  '#65a30d',
  '#dc2626',
] as const

export const EFFORT_LINE_DASH: Record<string, number[]> = {
  low: [],
  medium: [6, 3],
  high: [2, 2],
  xhigh: [9, 3, 2, 3],
  max: [1, 3],
  ultra: [10, 4],
}

export function configurationKey(model: string, effort: string): string {
  return `${model}\u0000${effort}`
}

export function compareEfforts(left: string, right: string): number {
  const leftIndex = EFFORT_ORDER.indexOf(
    left.toLowerCase() as (typeof EFFORT_ORDER)[number]
  )
  const rightIndex = EFFORT_ORDER.indexOf(
    right.toLowerCase() as (typeof EFFORT_ORDER)[number]
  )
  if (leftIndex === -1 && rightIndex === -1) {
    return left.localeCompare(right)
  }
  if (leftIndex === -1) return 1
  if (rightIndex === -1) return -1
  return leftIndex - rightIndex
}

export type ModelRadarGroup = {
  model: string
  color: string
  configurations: ModelRadarConfiguration[]
}

export function groupConfigurations(
  configurations: ModelRadarConfiguration[]
): ModelRadarGroup[] {
  const groups = new Map<string, ModelRadarConfiguration[]>()
  for (const configuration of configurations) {
    const existing = groups.get(configuration.model)
    if (existing) {
      existing.push(configuration)
    } else {
      groups.set(configuration.model, [configuration])
    }
  }

  return Array.from(groups, ([model, modelConfigurations], index) => ({
    model,
    color: MODEL_COLORS[index % MODEL_COLORS.length],
    configurations: [...modelConfigurations].sort((left, right) =>
      compareEfforts(left.effort, right.effort)
    ),
  }))
}

export function createModelColorMap(
  configurations: ModelRadarConfiguration[]
): Map<string, string> {
  return new Map(
    groupConfigurations(configurations).map((group) => [
      group.model,
      group.color,
    ])
  )
}

export type RadarHistoryDatum = ModelRadarHistoryPoint & {
  ts: number
  configuration: string
}

export function flattenHistory(
  history: ModelRadarHistoryFrame[]
): RadarHistoryDatum[] {
  return history.flatMap((frame) =>
    frame.points.map((point) => ({
      ...point,
      ts: frame.ts,
      configuration: configurationKey(point.model, point.effort),
    }))
  )
}

export function getMetricValue(
  point: ModelRadarHistoryPoint,
  metric: RadarMetric
): number | null {
  const value = point[metric]
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

export function getEffortLineDash(effort: string): number[] {
  return EFFORT_LINE_DASH[effort.toLowerCase()] ?? [4, 3, 1, 3]
}

export function getEffortBorderStyle(
  effort: string
): 'solid' | 'dashed' | 'dotted' {
  if (effort.toLowerCase() === 'low') return 'solid'
  if (effort.toLowerCase() === 'high') return 'dotted'
  return 'dashed'
}
