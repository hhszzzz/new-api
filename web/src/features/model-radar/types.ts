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

export type ModelRadarBenchMetricStat = {
  /** Observed mean over the known samples; null when no sample has the measurement. */
  mean: number | null
  known_samples: number | null
  total_samples: number | null
}

/** One token or turn aggregate over all retained valid grades of a tier. */
export type ModelRadarBenchDetailMetric = {
  mean: number | null
  known_samples: number
  total_samples: number
}

/**
 * Grading activity and token usage published with the v6 RadarBench summary.
 * `metrics` is empty unless `metrics_state` is `available`.
 */
export type ModelRadarBenchDetailMetrics = {
  valid_grade_events: number
  latest_graded_at: number | null
  as_of: number
  graded_24h: number
  graded_48h: number
  metrics_state: string
  metrics: Record<string, ModelRadarBenchDetailMetric>
}

export type ModelRadarBenchSourceScore = {
  score: number | null
  coverage: number
  required_tasks: number
  graded_runs_count: number | null
}

export type ModelRadarBenchHistoryPoint = {
  ts: number
  score: number | null
  coverage: number
  score_status: string
}

export type ModelRadarBenchCell = {
  model: string
  effort: string
  score: number | null
  coverage: number
  required_tasks: number
  score_status: string
  graded_runs_count: number | null
  source_scores: Record<string, ModelRadarBenchSourceScore>
  price: ModelRadarBenchMetricStat
  duration: ModelRadarBenchMetricStat
  statistics_state: string
  /** How the score was aggregated; absent in pre-v6 snapshots. */
  scoring_policy_version?: string
  /** v6 grading-activity and token detail; null when the snapshot predates it. */
  detail_metrics: ModelRadarBenchDetailMetrics | null
  history_state: string
  history: ModelRadarBenchHistoryPoint[]
  last_graded_day: number
  read_at: number
}

export type ModelRadarBench = {
  benchmark: string
  catalog_version: string
  task_set_sha256: string
  score_version: string
  score_max: number
  source_counts: Record<string, number>
  /** Time of the last round that read every bound selection; 0 until it happens. */
  read_at: number
  loaded_count: number
  total_count: number
  failed_cells: string[]
  cells: ModelRadarBenchCell[]
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
  bench: ModelRadarBench | null
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
