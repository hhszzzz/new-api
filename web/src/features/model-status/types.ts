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
export type ModelHealthStatus =
  | 'no_data'
  | 'operational'
  | 'degraded'
  | 'failed'

export type ModelStatusTimelinePoint = {
  ts: number
  status: ModelHealthStatus
  request_count: number | null
  success_count: number | null
  success_rate: number | null
  avg_ttft_ms: number | null
  avg_latency_ms: number | null
  avg_tps: number | null
}

export type ModelStatusModel = {
  model_name: string
  vendor: string
  icon: string
  request_count: number | null
  success_count: number | null
  success_rate: number | null
  avg_ttft_ms: number | null
  avg_latency_ms: number | null
  avg_tps: number | null
  status: ModelHealthStatus
  timeline: ModelStatusTimelinePoint[]
}

export type ModelStatusSnapshot = {
  generated_at: number
  window_hours: 24
  models: ModelStatusModel[]
}

export type ModelStatusResponse = {
  success: boolean
  message?: string
  data: ModelStatusSnapshot
}
