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

export type ModelRadarConfiguration = {
  model: string
  effort: string
  harness: string
  runs_24h: number | null
  runs_48h: number | null
  average_price_usd_by_band: {
    off_peak: number | null
    peak: number | null
  } | null
  /** Software-engineering (DeepSWE) IQ. */
  iq: number
  /** Comprehensive IQ (software + visual, weighted by valid task count). Null when upstream has no visual-spatial score. */
  comprehensive_iq?: number | null
  /** Visual-spatial reasoning IQ. Null when upstream has no visual-spatial score. */
  visual_iq?: number | null
  passed: number
  valid_tasks: number
  average_price_usd: number | null
  price_samples: number | null
  /** Cost re-computed against the provider's official tariff, when upstream publishes one. */
  corrected_average_price_usd?: number | null
  corrected_cost_samples?: number | null
  average_minutes: number | null
  duration_samples: number | null
  incomplete_cost_samples: number | null
  total_runs: number | null
  latest_graded_at: number | null
  average_agent_steps: number | null
  agent_steps_samples: number | null
  average_total_tokens: number | null
  token_samples: number | null
  cache_hit_rate: number | null
  cache_token_samples: number | null
  combined_cost_index: number | null
}

export type ModelRadarHistoryPoint = {
  model: string
  effort: string
  iq: number
  passed: number
  valid_tasks: number
  average_price_usd: number | null
  average_minutes: number | null
  average_agent_steps: number | null
  average_total_tokens: number | null
  cache_hit_rate: number | null
}

export type ModelRadarHistoryFrame = {
  ts: number
  points: ModelRadarHistoryPoint[]
}

export type ModelRadarTrendPoint = {
  ts: number
  iq: number
  samples: number
}

export type ModelRadarDegradationAlert = {
  model: string
  effort: string
  iq: number
  /** Each decline window is null when upstream has too little history for it. */
  degradation_12h_iq: number | null
  degradation_24h_iq: number | null
  degradation_48h_iq: number | null
  average_iq_24h?: number | null
  average_iq_48h?: number | null
  /** Hourly upstream IQ readings, oldest first. Absent in older snapshots. */
  trend_48h?: ModelRadarTrendPoint[] | null
}

export type ModelRadarData = {
  settings?: ModelRadarSettings
  schema_version: number
  fetched_at: number
  source_updated_at: number
  alerts_updated_at: number
  stale: boolean
  source: ModelRadarSource
  model_count: number
  configuration_count: number
  configurations: ModelRadarConfiguration[]
  history: ModelRadarHistoryFrame[]
  degradation_alerts: ModelRadarDegradationAlert[]
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
  default_vendor: string
  show_degradation_alerts: boolean
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
