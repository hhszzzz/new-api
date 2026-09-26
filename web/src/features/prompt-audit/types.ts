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
export type PromptWordlistAction = 'block' | 'review'
export type PromptAuditDirection = 'input' | 'output'
export type PromptAuditScope =
  | 'system'
  | 'developer'
  | 'user'
  | 'assistant'
  | 'tool_call'
  | 'tool_result'
  | 'task'
  | 'agent_context'
  | 'skill'
  | 'mcp'
export interface PromptScopePolicy {
  library_ids: string[]
  model_audit: boolean
}
export type PromptScopePolicies = Record<PromptAuditScope, PromptScopePolicy>
export interface PromptWordlist {
  id: string
  name: string
  source_url: string
  enabled: boolean
  action: PromptWordlistAction
  auto_update: boolean
  status: 'pending' | 'updating' | 'ready' | 'failed'
  word_count: number
  file_count: number
  content_hash: string
  source_revision: string
  last_success_at: number
  next_sync_at: number
  last_error: string
  scopes: PromptAuditScope[]
}
export interface PromptWordlistMatch {
  id: string
  name: string
  version: string
  scope: PromptAuditScope
  action: PromptWordlistAction
}
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

/**
 * How an audit node is called. 'qwen3guard' is an OpenAI-compatible guard model
 * that answers with a safety label; 'typesafe' is a TypeSafe/JEV node that
 * answers with a probability per question.
 */
export type PromptAuditNodeProtocol = 'qwen3guard' | 'typesafe'
export type PromptAuditEndpointPurpose = 'classify' | 'review'

export interface PromptAuditEndpoint {
  id: string
  protocol: PromptAuditNodeProtocol
  name: string
  base_url: string
  model: string
  timeout_ms: number
  input_limit: number
  concurrency: number
  enabled: boolean
  has_token: boolean
  purpose: PromptAuditEndpointPurpose
  directions: PromptAuditDirection[]
  /**
   * Only meaningful for a TypeSafe node. A question at or above the block
   * threshold is Unsafe, at or above the review threshold is Controversial.
   * A zero value means "use the protocol default" on the server.
   */
  block_threshold: number
  review_threshold: number
}

export interface PromptAuditEndpointUpdate extends Omit<
  PromptAuditEndpoint,
  'has_token'
> {
  original_id?: string
  token?: string
}

export interface PromptAuditConfig {
  scope_policies?: PromptScopePolicies
  word_filter_enabled?: boolean
  mode: PromptAuditMode
  output_mode: PromptAuditMode
  blocking_latest_turn_only: boolean
  probe_block_enabled?: boolean
  probe_phrases?: string[]
  probe_semantic_enabled?: boolean
  probe_semantic_threshold?: number
  probe_include_admins?: boolean
  expand_base64?: boolean
  manual_wordlist_action: PromptWordlistAction
  enabled_categories: string[]
  controversial_block_categories: string[]
  review_enabled: boolean
  review_prompt: string
  all_groups: boolean
  groups: string[]
  endpoints: PromptAuditEndpoint[]
  total_timeout_ms: number
  chunk_overlap: number
  chunk_concurrency: number
  cache_ttl_seconds: number
  worker_count: number
  max_attempts: number
  retention_days: number
  global_concurrency: number
  endpoint_concurrency: number
  output_max_bytes: number
  output_memory_bytes: number
  /**
   * How many characters of the whole request are kept on each record. A value at
   * or above FULL_PROMPT_MAX_RUNES_LIMIT keeps the entire request.
   */
  full_prompt_max_runes?: number
  config_version: string
}

export type PromptAuditConfigUpdate = Omit<
  PromptAuditConfig,
  'endpoints' | 'config_version'
> & {
  endpoints: PromptAuditEndpointUpdate[]
}

export interface PromptAuditRepeat {
  count: number
  first_at: number
  last_at: number
  /**
   * The worst decision any request of the group reached. Empty while a request
   * is still pending and nothing worse was decided.
   */
  worst_decision: PromptAuditDecision
  blocks: number
  unavailable: number
}

export interface PromptAuditEvent {
  inspection_type?:
    | 'wordlist'
    | 'model'
    | 'wordlist_model'
    | 'probe_block'
    | 'probe_phrase'
    | 'probe_semantic'
    | 'probe_fast_pass'
  group_key?: string
  session_key?: string
  request_kind?: string
  scan_payload?: string
  scan_payload_truncated?: boolean
  wordlist_id?: string
  wordlist_name?: string
  wordlist_version?: string
  matched_scope?: PromptAuditScope
  inspected_scopes?: PromptAuditScope[]
  id: number
  request_id: string
  user_id: number
  token_id: number
  token_name: string
  username: string
  group: string
  protocol: string
  model: string
  stage: string
  direction: PromptAuditDirection
  generation_id: string
  delivery_status: string
  coverage_complete: boolean
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
  refusal: string
  decision: PromptAuditDecision
  action: string
  would_action: string
  categories: string[]
  unknown_categories: string[]
  endpoint_id: string
  endpoint_model: string
  /**
   * The raw probabilities a TypeSafe node returned, keyed by category. Absent
   * for a label-based verdict, which has none.
   */
  scores?: Record<string, number>
  review_status: string
  review_decision: PromptAuditDecision
  review_codes: string[]
  review_reason: string
  reviewer_endpoint_id: string
  human_review: string
  human_review_reason: string
  reviewed_by: number
  reviewer_name: string
  reviewed_at: number
  latency_ms: number
  attempts: number
  max_attempts: number
  next_attempt_at: number
  error_code: string
  ip: string
  user_agent: string
  method: string
  request_path: string
  origin: string
  referer: string
  created_at: number
  updated_at: number
  completed_at: number
  /**
   * Present only in the collapsed listing: the requests this row stands for,
   * and what the whole group decided. Absent on a single event.
   */
  repeat?: PromptAuditRepeat
}

export interface PromptAuditFilters {
  status: string
  decision: string
  category: string
  username: string
  group: string
  protocol: string
  model: string
  request_id: string
  direction: string
  detector: string
  start_time: string
  end_time: string
}

export interface PromptAuditListData {
  items: PromptAuditEvent[]
  /**
   * Rows matching the filters: groups in the collapsed listing, and every
   * request of the group when one group is expanded, which can exceed the
   * requests returned.
   */
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
  /**
   * Rows of the collapsed listing; each stands for every request of its group
   * that the other filters match.
   */
  group_ids?: number[]
  status?: string
  decision?: string
  category?: string
  username?: string
  group?: string
  protocol?: string
  model?: string
  request_id?: string
  direction?: string
  detector?: string
  start_time?: number
  end_time?: number
}

export interface PromptAuditDeletePreview {
  eligible_count: number
  active_count: number
  max_id: number
}
