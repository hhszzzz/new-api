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
import { getProtocolName } from '@/features/usage-logs/lib/protocol-conversion'
import { getDefaultTimeRange } from '@/features/usage-logs/lib/utils'
import dayjs from '@/lib/dayjs'

import type {
  PromptAuditConfigUpdate,
  PromptAuditDeleteFilter,
  PromptAuditEndpoint,
  PromptAuditEndpointUpdate,
  PromptAuditEvent,
  PromptAuditFilters,
} from './types'

export const EMPTY_PROMPT_AUDIT_FILTERS: PromptAuditFilters = {
  status: '',
  decision: '',
  category: '',
  username: '',
  group: '',
  protocol: '',
  model: '',
  request_id: '',
  direction: '',
  start_time: '',
  end_time: '',
}

export function getDefaultPromptAuditFilters(): PromptAuditFilters {
  const { start, end } = getDefaultTimeRange()
  return {
    ...EMPTY_PROMPT_AUDIT_FILTERS,
    start_time: dayjs(start).format('YYYY-MM-DDTHH:mm'),
    end_time: dayjs(end).format('YYYY-MM-DDTHH:mm'),
  }
}

/**
 * The records screen's listing mode. Collapsing and expanding are ways of
 * reading the same filters, so neither is part of PromptAuditFilters: the
 * delete and statistics paths must keep seeing the filters alone.
 */
export interface PromptAuditListingView {
  /** Merge the requests that submitted the same audited text into one row. */
  collapseRepeats?: boolean
  /** Return the requests behind one collapsed row instead of the listing. */
  groupID?: number
}

export function promptAuditFilterParams(
  filters: PromptAuditFilters,
  view: PromptAuditListingView = {}
): Record<string, string | number | undefined> {
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
    username: filters.username.trim() || undefined,
    group: filters.group.trim() || undefined,
    protocol: filters.protocol.trim() || undefined,
    model: filters.model.trim() || undefined,
    request_id: filters.request_id.trim() || undefined,
    direction: filters.direction || undefined,
    start_time:
      startTime !== undefined && Number.isFinite(startTime)
        ? startTime
        : undefined,
    end_time:
      endTime !== undefined && Number.isFinite(endTime) ? endTime : undefined,
    collapse_repeats: view.collapseRepeats ? 'true' : undefined,
    group_id:
      view.groupID !== undefined && view.groupID > 0 ? view.groupID : undefined,
  }
}

const COLLAPSE_REPEATS_STORAGE_KEY = 'prompt-audit:collapse-repeats'

/**
 * Whether the records listing opens merged. An agent run submits the same text
 * once per step, so the merged reading is the one that shows what happened; the
 * choice is remembered because a mode that resets on every visit reads as a
 * broken feature rather than as a default.
 */
export function readPromptAuditCollapseRepeats(): boolean {
  if (typeof window === 'undefined') return true
  try {
    const stored = window.localStorage.getItem(COLLAPSE_REPEATS_STORAGE_KEY)
    return stored === null ? true : stored === 'true'
  } catch {
    return true
  }
}

export function writePromptAuditCollapseRepeats(collapsed: boolean): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(COLLAPSE_REPEATS_STORAGE_KEY, String(collapsed))
  } catch {
    // Storage can be refused or full; the listing still works, it only forgets
    // the choice.
  }
}

/**
 * The row id the records table gives one audit. A collapsed row renders the
 * requests it merged as child rows, and the request that represents the group is
 * one of them, so a child's id is prefixed with its parent's: otherwise the two
 * rows would share an id and TanStack Table could not tell them apart. Top-level
 * ids stay bare because selection and the detail sheet are keyed by them.
 */
export function promptAuditRowID(
  event: PromptAuditEvent,
  _index: number,
  parent?: { id: string }
): string {
  return parent ? `${parent.id}:${event.id}` : String(event.id)
}

/**
 * Whether a listing row stands for more than the one request it shows. The
 * collapsed listing hands every row a repeat summary, including the groups that
 * turned out to hold a single request; only a merged one has requests of its own
 * to reveal, so only it can be opened and only it shows a count.
 */
export function isMergedPromptAuditRow(event: PromptAuditEvent): boolean {
  return event.repeat !== undefined && event.repeat.count > 1
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
    purpose: endpoint.purpose,
    directions: [...endpoint.directions],
  }
  if (endpoint.original_id) update.original_id = endpoint.original_id
  if (endpoint.token_changed) update.token = endpoint.token
  return update
}

const BYTES_PER_MB = 1024 * 1024

// Display helpers for the two byte-valued output limits: the settings UI edits
// them in whole megabytes. Both clamp at 1 MB so a persisted value always
// satisfies the numeric ranges enforced in validatePromptAuditConfig() below.
export function bytesToMB(bytes: number): number {
  return Math.max(1, Math.round(bytes / BYTES_PER_MB))
}

export function mbToBytes(mb: number): number {
  return Math.max(BYTES_PER_MB, Math.round(mb * BYTES_PER_MB))
}

export function validatePromptAuditConfig(
  config: PromptAuditConfigUpdate
): string | null {
  if (
    config.mode !== 'off' &&
    !config.endpoints.some(
      (model) =>
        model.enabled &&
        model.purpose === 'classify' &&
        model.directions.includes('input')
    )
  ) {
    return 'At least one enabled audit node is required.'
  }
  if (
    config.output_mode !== 'off' &&
    !config.endpoints.some(
      (model) =>
        model.enabled &&
        model.purpose === 'classify' &&
        model.directions.includes('output')
    )
  ) {
    return 'At least one enabled output audit node is required.'
  }
  if (
    !config.all_groups &&
    config.groups.length === 0 &&
    (config.mode !== 'off' || config.output_mode !== 'off')
  ) {
    return 'Select at least one group or enable all groups.'
  }
  if (
    config.review_enabled &&
    !config.endpoints.some(
      (model) => model.enabled && model.purpose === 'review'
    )
  ) {
    return 'At least one enabled gray-area reviewer node is required.'
  }
  const numericRanges: Array<[number, number, number]> = [
    [config.total_timeout_ms, 100, 120000],
    [config.chunk_overlap, 0, 512],
    [config.chunk_concurrency, 1, 16],
    [config.cache_ttl_seconds, 0, 86400],
    [config.worker_count, 1, 64],
    [config.max_attempts, 1, 4],
    [config.retention_days, 0, 3650],
    [config.global_concurrency, 1, 1024],
    [config.endpoint_concurrency, 1, 256],
    [config.output_max_bytes, 1024, 64 * 1024 * 1024],
    [config.output_memory_bytes, 1024, config.output_max_bytes],
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
    // Empty IDs are generated server-side from the model name; only
    // duplicates among explicitly provided IDs matter here.
    if (endpoint.id) {
      if (ids.has(endpoint.id)) {
        return 'Audit model IDs must be unique.'
      }
      ids.add(endpoint.id)
    }
    if (!endpoint.model) return 'Audit node models are required.'
    if (endpoint.purpose !== 'classify' && endpoint.purpose !== 'review') {
      return 'Select a valid audit node purpose.'
    }
    if (endpoint.purpose === 'classify' && endpoint.directions.length === 0) {
      return 'Select at least one audit direction for every classification node.'
    }
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
    if (endpoint.enabled && endpoint.purpose === 'classify') {
      minimumInputLimit = Math.min(minimumInputLimit, endpoint.input_limit)
    }
  }
  if (config.chunk_overlap >= minimumInputLimit) {
    return 'Chunk overlap must be smaller than every enabled node input limit.'
  }
  return null
}

// Protocol formats the shared usage-log map does not name. Kept local rather
// than added to protocol-conversion.ts: that map also decides whether a usage
// log reports a native protocol flow, so extending it would change unrelated
// log badges. Unmapped values fall through to the shared names, and finally to
// the raw format, which is what getProtocolName itself does.
const PROMPT_AUDIT_PROTOCOL_NAMES: Record<string, string> = {
  openai_responses: 'OpenAI Responses',
  openai_alpha_search: 'OpenAI Alpha Search',
  openai_audio: 'OpenAI Audio',
  openai_image: 'OpenAI Images',
  openai_realtime: 'OpenAI Realtime',
  rerank: 'Rerank',
  embedding: 'Embeddings',
  task: 'Async Task',
  mj_proxy: 'Midjourney',
}

export function getPromptAuditProtocolName(protocol: string): string {
  if (!protocol) return ''
  return PROMPT_AUDIT_PROTOCOL_NAMES[protocol] ?? getProtocolName(protocol)
}
