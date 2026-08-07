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
export type PromptAuditMode = 'off' | 'async_audit' | 'blocking'
export type PromptAuditStatus =
  | 'queued'
  | 'processing'
  | 'retry'
  | 'done'
  | 'failed'
export type PromptAuditDecision = '' | 'pass' | 'flag' | 'block' | 'unavailable'

export interface ApiResponse<T> {
  success: boolean
  message?: string
  data?: T
}

export interface PromptAuditCategory {
  id: string
  label: string
  label_zh: string
  description: string
}

export interface PromptAuditEndpoint {
  id: string
  name: string
  base_url: string
  model: string
  timeout_ms: number
  input_limit: number
  concurrency: number
  enabled: boolean
  has_token: boolean
}

export interface PromptAuditEndpointUpdate extends Omit<
  PromptAuditEndpoint,
  'has_token'
> {
  original_id?: string
  token?: string
}

export interface PromptAuditConfig {
  mode: PromptAuditMode
  enabled_categories: string[]
  all_groups: boolean
  groups: string[]
  endpoints: PromptAuditEndpoint[]
  total_timeout_ms: number
  chunk_overlap: number
  cache_ttl_seconds: number
  worker_count: number
  max_attempts: number
  retention_days: number
  global_concurrency: number
  endpoint_concurrency: number
  config_version: string
}

export type PromptAuditConfigUpdate = Omit<
  PromptAuditConfig,
  'endpoints' | 'config_version'
> & {
  endpoints: PromptAuditEndpointUpdate[]
}

export interface PromptAuditEvent {
  id: number
  request_id: string
  user_id: number
  token_id: number
  token_name: string
  group: string
  protocol: string
  model: string
  stage: string
  config_version: string
  execution_mode: PromptAuditMode
  status: PromptAuditStatus
  prompt_hash: string
  prompt_length: number
  segment_count: number
  chunk_count: number
  full_prompt?: string
  full_prompt_available: boolean
  full_prompt_truncated: boolean
  redacted_preview: string
  safety: string
  decision: PromptAuditDecision
  would_action: string
  categories: string[]
  unknown_categories: string[]
  endpoint_id: string
  latency_ms: number
  attempts: number
  max_attempts: number
  next_attempt_at: number
  error_code: string
  created_at: number
  updated_at: number
  completed_at: number
}

export interface PromptAuditFilters {
  status: string
  decision: string
  category: string
  user_id: string
  group: string
  protocol: string
  model: string
  endpoint_id: string
  prompt_hash: string
  request_id: string
  start_time: string
  end_time: string
}

export interface PromptAuditListData {
  items: PromptAuditEvent[]
  total: number
  page: number
  page_size: number
}

export interface PromptAuditStats {
  total: number
  statuses: Record<string, number>
  decisions: Record<string, number>
  categories: Record<string, number>
  unknown_categories: number
}

export interface PromptAuditDeleteFilter {
  ids?: number[]
  status?: string
  decision?: string
  category?: string
  user_id?: number
  group?: string
  protocol?: string
  model?: string
  endpoint_id?: string
  prompt_hash?: string
  request_id?: string
  start_time?: number
  end_time?: number
}

export interface PromptAuditDeletePreview {
  eligible_count: number
  active_count: number
  max_id: number
}
