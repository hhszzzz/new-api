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
export type ModelRadarSource = {
  name: string
  url: string
  attribution: string
}

export type ModelRadarFourTaskComponent = {
  /** Component result on its 0-100 scale; null when the component was not graded. */
  score: number | null
  /** Share of the 150-point total. */
  weight: number
  /** Weight * score / 100. */
  contribution: number | null
  cost_usd: number | null
  duration_minutes: number | null
  state: string
  missing_reason?: string
}

export type ModelRadarFourTaskCell = {
  model: string
  effort: string
  /** Weighted total; null unless every component was graded. */
  score: number | null
  graded_tasks: number
  required_tasks: number
  /** Place among the published totals, highest first; null without a total. */
  rank: number | null
  components: Record<string, ModelRadarFourTaskComponent>
  cost_usd: number | null
  duration_minutes: number | null
}

export type ModelRadarFourTask = {
  protocol: string
  round_id: string
  score_max: number
  source_sha256: string
  publication_status: string
  verified_at: number
  test_started_at: number
  test_completed_at: number
  graded: number
  expected: number
  ungraded: number
  valid_totals: number
  task_weights: Record<string, number>
  model_names: Record<string, string>
  model_order: string[]
  read_at: number
  cells: ModelRadarFourTaskCell[]
}

export type ModelRadarData = {
  settings?: ModelRadarSettings
  schema_version: number
  fetched_at: number
  source_updated_at: number
  stale: boolean
  source: ModelRadarSource
  model_count: number
  configuration_count: number
  four_task: ModelRadarFourTask | null
}

export type ModelRadarModelOverride = {
  display_name?: string
  vendor?: string
  hidden?: boolean
  /** Administrator opt-in for radar auto-effort on this model. */
  auto_effort?: boolean
  /** Extra gateway model names that resolve to this radar model. */
  aliases?: string[]
}

export type ModelRadarSettings = {
  /** Administrator kill switch for radar auto-effort. */
  auto_effort_enabled: boolean
  models: Record<string, ModelRadarModelOverride>
}

export type ModelRadarResponse = {
  success: boolean
  message: string
  code?: string
  data: ModelRadarData
}
