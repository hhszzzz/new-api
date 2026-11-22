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
export type DistributionRiskMode = 'disabled' | 'shadow' | 'enforce'
export type DistributionRiskStatus = 'pending' | 'confirmed' | 'false_positive'
export type DistributionRiskEventQuery = {
  status: DistributionRiskStatus | 'all'
  level: 'medium' | 'high' | 'all'
  scope_kind: 'user' | 'token' | 'all'
  user_id?: number
  start_timestamp?: number
  end_timestamp?: number
}

export interface DistributionRiskConfig {
  enabled: boolean
  mode: DistributionRiskMode
  started_at: number
  shadow_days: number
  ip_floor: number
  user_agent_floor: number
  session_floor: number
  requests_per_minute_floor: number
  concurrency_floor: number
  min_requests: number
  min_active_days: number
}

export type DistributionRiskConfigUpdate = Omit<
  DistributionRiskConfig,
  'mode' | 'started_at' | 'shadow_days'
>

export interface DistributionRiskEvidence {
  ips?: string[] | null
  user_agents?: string[] | null
  session_hashes?: string[] | null
}

export interface DistributionRiskEvent {
  id: number
  user_id: number
  scope_kind: 'user' | 'token'
  scope_id: number
  token_ids: number[]
  score: number
  level: 'medium' | 'high'
  category_scores: Record<string, number> | null
  status: DistributionRiskStatus
  created_at: number
  reviewed_at: number
  reviewer_id: number
  review_reason: string
  evidence?: DistributionRiskEvidence
}

export interface DistributionRiskEventList {
  items: DistributionRiskEvent[]
  total: number
  pending_count: number
  page: number
  page_size: number
}

export interface ApiResponse<T> {
  success: boolean
  message?: string
  data?: T
}
