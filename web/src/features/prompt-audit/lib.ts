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
  PromptAuditConfigUpdate,
  PromptAuditDeleteFilter,
  PromptAuditEndpoint,
  PromptAuditEndpointUpdate,
  PromptAuditFilters,
} from './types'

export const EMPTY_PROMPT_AUDIT_FILTERS: PromptAuditFilters = {
  status: '',
  decision: '',
  category: '',
  user_id: '',
  group: '',
  protocol: '',
  model: '',
  endpoint_id: '',
  prompt_hash: '',
  request_id: '',
  start_time: '',
  end_time: '',
}

export function promptAuditFilterParams(
  filters: PromptAuditFilters
): Record<string, string | number | undefined> {
  const userID = Number.parseInt(filters.user_id, 10)
  const startTime = filters.start_time
    ? Math.floor(new Date(filters.start_time).getTime() / 1000)
    : undefined
  const endTime = filters.end_time
    ? Math.floor(new Date(filters.end_time).getTime() / 1000)
    : undefined

  return {
    status: filters.status || undefined,
    decision: filters.decision || undefined,
    category: filters.category || undefined,
    user_id: Number.isFinite(userID) && userID > 0 ? userID : undefined,
    group: filters.group.trim() || undefined,
    protocol: filters.protocol.trim() || undefined,
    model: filters.model.trim() || undefined,
    endpoint_id: filters.endpoint_id.trim() || undefined,
    prompt_hash: filters.prompt_hash.trim() || undefined,
    request_id: filters.request_id.trim() || undefined,
    start_time:
      startTime !== undefined && Number.isFinite(startTime)
        ? startTime
        : undefined,
    end_time:
      endTime !== undefined && Number.isFinite(endTime) ? endTime : undefined,
  }
}

export function promptAuditDeleteFilter(
  filters: PromptAuditFilters,
  ids: number[] = []
): PromptAuditDeleteFilter {
  if (ids.length > 0) return { ids }
  return promptAuditFilterParams(filters) as PromptAuditDeleteFilter
}

export function validatePromptAuditFilters(
  filters: PromptAuditFilters
): string | null {
  if (!filters.start_time || !filters.end_time) return null
  const start = new Date(filters.start_time).getTime()
  const end = new Date(filters.end_time).getTime()
  if (Number.isFinite(start) && Number.isFinite(end) && start > end) {
    return 'Start time must not be later than end time.'
  }
  return null
}

export type PromptAuditEndpointDraft = PromptAuditEndpoint & {
  client_key: string
  original_id: string
  original_base_url: string
  token: string
  token_changed: boolean
}

export function promptAuditEndpointDrafts(
  endpoints: PromptAuditEndpoint[]
): PromptAuditEndpointDraft[] {
  return endpoints.map((endpoint) => ({
    ...endpoint,
    client_key: endpoint.id,
    original_id: endpoint.id,
    original_base_url: endpoint.base_url,
    token: '',
    token_changed: false,
  }))
}

export function promptAuditEndpointBaseURLUpdate(
  endpoint: PromptAuditEndpointDraft,
  baseURL: string
): Partial<PromptAuditEndpointDraft> {
  const update: Partial<PromptAuditEndpointDraft> = { base_url: baseURL }
  const normalize = (value: string) => value.trim().replace(/\/+$/, '')
  if (
    endpoint.has_token &&
    !endpoint.token_changed &&
    normalize(baseURL) !== normalize(endpoint.original_base_url)
  ) {
    update.token = ''
    update.token_changed = true
  }
  return update
}

export function promptAuditEndpointUpdate(
  endpoint: PromptAuditEndpointDraft
): PromptAuditEndpointUpdate {
  const update: PromptAuditEndpointUpdate = {
    id: endpoint.id.trim(),
    name: endpoint.name.trim(),
    base_url: endpoint.base_url.trim(),
    model: endpoint.model.trim(),
    timeout_ms: endpoint.timeout_ms,
    input_limit: endpoint.input_limit,
    concurrency: endpoint.concurrency,
    enabled: endpoint.enabled,
  }
  if (endpoint.original_id) update.original_id = endpoint.original_id
  if (endpoint.token_changed) update.token = endpoint.token
  return update
}

export function validatePromptAuditConfig(
  config: PromptAuditConfigUpdate
): string | null {
  if (config.mode !== 'off' && !config.endpoints.some((node) => node.enabled)) {
    return 'At least one enabled audit node is required.'
  }
  if (
    !config.all_groups &&
    config.groups.length === 0 &&
    config.mode !== 'off'
  ) {
    return 'Select at least one group or enable all groups.'
  }
  const numericRanges: Array<[number, number, number]> = [
    [config.total_timeout_ms, 100, 120000],
    [config.chunk_overlap, 0, 512],
    [config.cache_ttl_seconds, 0, 86400],
    [config.worker_count, 1, 64],
    [config.max_attempts, 1, 4],
    [config.retention_days, 0, 3650],
    [config.global_concurrency, 1, 1024],
    [config.endpoint_concurrency, 1, 256],
  ]
  if (
    numericRanges.some(
      ([value, minimum, maximum]) =>
        !Number.isInteger(value) || value < minimum || value > maximum
    )
  ) {
    return 'Numeric settings must be whole numbers within the displayed ranges.'
  }
  const ids = new Set<string>()
  let minimumInputLimit = Number.POSITIVE_INFINITY
  for (const endpoint of config.endpoints) {
    if (!endpoint.id || ids.has(endpoint.id)) {
      return 'Audit node IDs must be non-empty and unique.'
    }
    ids.add(endpoint.id)
    if (!endpoint.model) return 'Audit node models are required.'
    if (
      !Number.isInteger(endpoint.timeout_ms) ||
      endpoint.timeout_ms < 100 ||
      endpoint.timeout_ms > 120000 ||
      !Number.isInteger(endpoint.input_limit) ||
      endpoint.input_limit < 256 ||
      endpoint.input_limit > 1048576 ||
      !Number.isInteger(endpoint.concurrency) ||
      endpoint.concurrency < 1 ||
      endpoint.concurrency > 256
    ) {
      return 'Audit node numeric values must be whole numbers within the displayed ranges.'
    }
    try {
      const url = new URL(endpoint.base_url)
      if (
        (url.protocol !== 'http:' && url.protocol !== 'https:') ||
        url.username ||
        url.password ||
        url.search ||
        url.hash
      ) {
        return 'Audit node URLs must be HTTP(S) URLs without credentials, query strings, or fragments.'
      }
    } catch {
      return 'Audit node URLs must be valid absolute HTTP(S) URLs.'
    }
    if (endpoint.enabled) {
      minimumInputLimit = Math.min(minimumInputLimit, endpoint.input_limit)
    }
  }
  if (config.chunk_overlap >= minimumInputLimit) {
    return 'Chunk overlap must be smaller than every enabled node input limit.'
  }
  return null
}
