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
import type { TFunction } from 'i18next'

import { getProtocolName } from '@/features/usage-logs/lib/protocol-conversion'
import { getDefaultTimeRange } from '@/features/usage-logs/lib/utils'
import dayjs from '@/lib/dayjs'

import { PROMPT_AUDIT_SCOPES } from './scopes'
import type {
  PromptAuditConfigUpdate,
  PromptAuditDeleteFilter,
  PromptAuditEndpoint,
  PromptAuditEndpointUpdate,
  PromptAuditEvent,
  PromptAuditFilters,
  PromptAuditScope,
} from './types'

export function promptAuditDetectorLabel(
  type: string | undefined,
  t: TFunction
): string {
  if (type === 'wordlist') return t('Wordlist')
  // The two probe gates are named for their judge: the phrase list blocks
  // locally, the semantic gate is a TypeSafe call. Legacy rows written before
  // the split carry the undifferentiated probe_block value.
  if (type === 'probe_phrase') return t('Wordlist probe blocked')
  if (type === 'probe_semantic') return t('JEV probe blocked')
  if (type === 'probe_block') return t('Probe blocked')
  if (type === 'probe_fast_pass') return t('Probe allowed')
  return t('Model audit')
}

/** The Badge variants the shared component defines, named for this use. */
export type PromptAuditOutcomeVariant =
  | 'destructive'
  | 'warning'
  | 'secondary'
  | 'outline'

export interface PromptAuditOutcome {
  /** A translation key that already exists in every locale. */
  key: string
  variant: PromptAuditOutcomeVariant
}

/**
 * What one audit row came to, as a single reading. A finished request is read by
 * its decision, which is the verdict an operator acts on; one still in flight is
 * read by its status, because it has no verdict yet. Rendering the two apart made
 * a row carry two badges whose combination was not itself a state — a "done"
 * paired with a "block" only restated the second one — while still hiding the
 * pending states behind a decision slot that was empty for them.
 */
export function promptAuditOutcome(event: {
  status: string
  decision: string
}): PromptAuditOutcome {
  switch (event.status) {
    case 'queued':
      return { key: 'queued', variant: 'outline' }
    case 'processing':
      return { key: 'processing', variant: 'warning' }
    case 'retry':
      return { key: 'retry', variant: 'warning' }
    case 'failed':
      // A failed request may still carry the decision reached before it failed;
      // when it does, that verdict is the more useful reading.
      if (event.decision) return { key: event.decision, variant: 'destructive' }
      return { key: 'failed', variant: 'destructive' }
    default:
      if (event.decision) {
        return {
          key: event.decision,
          variant: decisionOutcomeVariant(event.decision),
        }
      }
      return { key: 'pending', variant: 'outline' }
  }
}

function decisionOutcomeVariant(decision: string): PromptAuditOutcomeVariant {
  if (decision === 'block' || decision === 'unavailable') return 'destructive'
  if (decision === 'flag') return 'warning'
  if (decision === 'pass') return 'secondary'
  return 'outline'
}

export function promptAuditRequestKindLabel(
  kind: string | undefined,
  t: TFunction
): string {
  switch (kind) {
    case 'step':
      return t('Conversation step')
    case 'subagent':
      return t('Subagent')
    case 'side:safety':
      return t('Background: safety check')
    case 'side:web_search':
      return t('Background: web search')
    case 'side:web_summary':
      return t('Background: web summary')
    case 'side:status':
      return t('Background: status')
    case 'side:recap':
      return t('Background: recap')
    case 'side:summary':
      return t('Background: summary')
    case 'side:title':
      return t('Background: title')
    case 'side:memory':
      return t('Background: memory')
    default:
      return t('User question')
  }
}

/** One inspected source and the blocks the audit submitted from it. */
export type PromptAuditPayloadSource = {
  /** Stable identity of the tab that reads this source. */
  key: string
  scope?: PromptAuditScope
  /** The blocks of this source, in wire order. */
  blocks: string[]
}

/**
 * The sources the audit submitted, one entry per source. A source is inspected as
 * whichever content blocks the client sent, so an agent resending its transcript
 * yields several blocks of the same source — reading each block as its own entry
 * made the detail sheet show one tab per block, all carrying the same name. The
 * blocks are kept apart so the sheet can mark where one ends and the next begins.
 */
export function promptAuditPayloadSources(
  payload?: string
): PromptAuditPayloadSource[] {
  const sources = new Map<string, PromptAuditPayloadSource>()
  for (const block of promptAuditPayloadBlocks(payload)) {
    // A block whose scope is not one of the known sources gets a reading of its
    // own; the sheet names it without pretending to know where it came from.
    const key = block.scope ?? 'unknown'
    const source = sources.get(key)
    if (source) {
      source.blocks.push(block.text)
      continue
    }
    sources.set(key, { key, scope: block.scope, blocks: [block.text] })
  }
  return [...sources.values()]
}

/** The content blocks the stored payload carries, in wire order. */
function promptAuditPayloadBlocks(
  payload?: string
): { scope?: PromptAuditScope; text: string }[] {
  if (!payload) return []
  try {
    const parsed: unknown = JSON.parse(payload)
    if (!parsed || typeof parsed !== 'object') return [{ text: payload }]
    const envelope = parsed as Record<string, unknown>
    if (typeof envelope.output === 'string') {
      return [{ scope: 'assistant', text: envelope.output }]
    }
    if (!Array.isArray(envelope.segments)) return [{ text: payload }]
    return envelope.segments.flatMap((segment: unknown) => {
      if (!segment || typeof segment !== 'object') return []
      const part = segment as Record<string, unknown>
      if (typeof part.text !== 'string') return []
      const scope = PROMPT_AUDIT_SCOPES.find((scope) => scope === part.scope)
      return [{ scope, text: part.text }]
    })
  } catch {
    return [{ text: payload }]
  }
}

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
  detector: '',
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
  /** Merge requests for the same question across models into one row. */
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
    detector: filters.detector || undefined,
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

/**
 * What a deletion removes. Selected rows of the plain listing are requests and
 * go by id. A selected row of the collapsed listing stands for its whole group,
 * so it goes as a group the server expands to every request the current
 * filters match; its id alone would delete only the request representing it.
 */
export function promptAuditDeleteFilter(
  filters: PromptAuditFilters,
  ids: number[] = [],
  options: { groups?: boolean } = {}
): PromptAuditDeleteFilter {
  if (ids.length === 0) {
    return promptAuditFilterParams(filters) as PromptAuditDeleteFilter
  }
  if (!options.groups) return { ids }
  return {
    ...(promptAuditFilterParams(filters) as PromptAuditDeleteFilter),
    group_ids: ids,
  }
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

/**
 * Values mirror setting/prompt_audit_setting/config.go; keep both sides in step.
 * The thresholds come from the TypeSafe Guardrails cookbook "strict" policy.
 */
export const TYPESAFE_DEFAULT_MODEL = 'jev-latest'
export const TYPESAFE_BASE_URL = 'https://api.typesafe.ai'
export const QWEN3GUARD_DEFAULT_MODEL = 'sileader/qwen3guard:0.6b'
export const TYPESAFE_MAX_INPUT_LIMIT = 16000
export const DEFAULT_BLOCK_THRESHOLD = 0.7
export const DEFAULT_REVIEW_THRESHOLD = 0.35
export const DEFAULT_PROBE_SEMANTIC_THRESHOLD = 0.85

/**
 * The three choices the audit model form offers. Purpose and protocol are two
 * server fields, but an operator picks one thing: a Qwen3Guard classifier, a
 * TypeSafe classifier, or the gray-area reviewer.
 */
export type PromptAuditNodeKind = 'qwen3guard' | 'typesafe' | 'review'

export function promptAuditNodeKind(
  endpoint: Pick<PromptAuditEndpoint, 'purpose' | 'protocol'>
): PromptAuditNodeKind {
  if (endpoint.purpose === 'review') return 'review'
  return endpoint.protocol === 'typesafe' ? 'typesafe' : 'qwen3guard'
}

/** A zero threshold means "use the protocol default", on both sides. */
export function promptAuditEndpointThresholds(
  endpoint: Pick<PromptAuditEndpoint, 'block_threshold' | 'review_threshold'>
): { block: number; review: number } {
  return {
    block:
      endpoint.block_threshold > 0
        ? endpoint.block_threshold
        : DEFAULT_BLOCK_THRESHOLD,
    review:
      endpoint.review_threshold > 0
        ? endpoint.review_threshold
        : DEFAULT_REVIEW_THRESHOLD,
  }
}

/**
 * The field changes one kind selection implies. Switching to TypeSafe fills the
 * TypeSafe defaults the way the server would, so the form never shows a
 * Qwen3Guard repository id or an empty base URL on a JEV node. A base URL the
 * operator typed is left alone; only an empty one is filled, and filling it goes
 * through promptAuditEndpointBaseURLUpdate so a saved token is still cleared
 * when the destination changes.
 */
export function promptAuditNodeKindUpdate(
  endpoint: PromptAuditEndpointDraft,
  kind: PromptAuditNodeKind
): Partial<PromptAuditEndpointDraft> {
  if (kind === 'review') {
    return { purpose: 'review' }
  }
  if (kind === 'qwen3guard') {
    const update: Partial<PromptAuditEndpointDraft> = {
      purpose: 'classify',
      protocol: 'qwen3guard',
    }
    if (
      endpoint.model.trim() === '' ||
      endpoint.model === TYPESAFE_DEFAULT_MODEL
    ) {
      update.model = QWEN3GUARD_DEFAULT_MODEL
    }
    return update
  }
  const thresholds = promptAuditEndpointThresholds(endpoint)
  const update: Partial<PromptAuditEndpointDraft> = {
    purpose: 'classify',
    protocol: 'typesafe',
    block_threshold: thresholds.block,
    review_threshold: thresholds.review,
  }
  if (
    endpoint.model.trim() === '' ||
    endpoint.model === QWEN3GUARD_DEFAULT_MODEL
  ) {
    update.model = TYPESAFE_DEFAULT_MODEL
  }
  if (endpoint.base_url.trim() === '') {
    Object.assign(
      update,
      promptAuditEndpointBaseURLUpdate(endpoint, TYPESAFE_BASE_URL)
    )
  }
  return update
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
    protocol: endpoint.protocol,
    directions: [...endpoint.directions],
    block_threshold: endpoint.block_threshold,
    review_threshold: endpoint.review_threshold,
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

/**
 * The retention limit on the whole request, in characters. These mirror
 * DefaultFullPromptMaxRunes, MinFullPromptMaxRunes and the unlimited sentinel in
 * setting/prompt_audit_setting/config.go; keep both sides in step.
 */
export const FULL_PROMPT_DEFAULT_RUNES = 65536
export const FULL_PROMPT_MIN_RUNES = 1024
export const FULL_PROMPT_MAX_RUNES = 1048576
/** Keeping the entire request. The backend reads 0 as "no truncation". */
export const FULL_PROMPT_NO_LIMIT = 0

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
  if (
    config.probe_semantic_enabled &&
    !config.endpoints.some(
      (model) =>
        model.enabled &&
        model.protocol === 'typesafe' &&
        model.purpose === 'classify'
    )
  ) {
    return 'Semantic probe detection requires an enabled TypeSafe classification node.'
  }
  if (
    config.probe_semantic_threshold !== undefined &&
    (!(config.probe_semantic_threshold > 0) ||
      config.probe_semantic_threshold > 1)
  ) {
    return 'The semantic probe threshold must be greater than 0 and at most 1.'
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
  // 0 is the explicit "keep the whole request" choice, so it is valid on its own
  // rather than part of a range. Anything between it and the floor is a typo that
  // would drop nearly all of the stored text. The number field turns a cleared
  // input into 0, which is why the unlimited mode is a separate switch instead of
  // being the value 0 typed into that field.
  if (
    config.full_prompt_max_runes !== undefined &&
    !(
      config.full_prompt_max_runes === FULL_PROMPT_NO_LIMIT ||
      (config.full_prompt_max_runes >= FULL_PROMPT_MIN_RUNES &&
        config.full_prompt_max_runes <= FULL_PROMPT_MAX_RUNES)
    )
  ) {
    return 'The stored prompt limit must be 0 or at least 1024 characters.'
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
    if (
      endpoint.protocol !== 'qwen3guard' &&
      endpoint.protocol !== 'typesafe'
    ) {
      return 'Select a valid audit node protocol.'
    }
    // A TypeSafe node answers probabilities per question and never returns a
    // review verdict, so it can only classify. The server refuses the pair too.
    if (endpoint.protocol === 'typesafe' && endpoint.purpose !== 'classify') {
      return 'TypeSafe nodes must be classification nodes.'
    }
    if (endpoint.protocol === 'typesafe') {
      const thresholds = promptAuditEndpointThresholds(endpoint)
      if (
        !(thresholds.review > 0) ||
        thresholds.review > thresholds.block ||
        thresholds.block > 1
      ) {
        return 'TypeSafe thresholds must satisfy 0 < review <= block <= 1.'
      }
      if (endpoint.input_limit > TYPESAFE_MAX_INPUT_LIMIT) {
        return 'TypeSafe node input limits must not exceed 16000 characters.'
      }
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

/**
 * The probabilities a TypeSafe node returned, highest first. The order is the
 * point: the category that decided the verdict leads, so an operator reads the
 * judgement before the rest of the distribution. Rows are [key, probability].
 */
export function promptAuditScoreRows(
  scores?: Record<string, number>
): Array<[string, number]> {
  if (!scores) return []
  return Object.entries(scores)
    .filter(([, score]) => Number.isFinite(score))
    .sort((left, right) => right[1] - left[1])
}

/**
 * The label key for one score. Category keys are their own translation keys;
 * the liveness probe is not a category and needs its own name.
 */
export function promptAuditScoreLabel(scoreKey: string): string {
  return scoreKey === 'probe' ? 'Liveness probe' : scoreKey
}

/**
 * The protocol values an audit row stores: the relay formats the gateway
 * inspects. The server matches the protocol filter against them exactly, so the
 * filter offers these values rather than free text.
 */
export const PROMPT_AUDIT_PROTOCOLS = [
  'openai',
  'claude',
  'gemini',
  'openai_responses',
  'openai_responses_compaction',
  'openai_alpha_search',
  'openai_audio',
  'openai_image',
  'openai_realtime',
  'rerank',
  'embedding',
  'task',
  'mj_proxy',
] as const

// Protocol formats the shared usage-log map does not name. Kept local rather
// than added to protocol-conversion.ts: that map also decides whether a usage
// log reports a native protocol flow, so extending it would change unrelated
// log badges. Unmapped values fall through to the shared names, and finally to
// the raw format, which is what getProtocolName itself does.
const PROMPT_AUDIT_PROTOCOL_NAMES: Record<string, string> = {
  openai_responses: 'OpenAI Responses',
  // The shared map reads compaction as plain Responses, which would give two
  // filter options the same name.
  openai_responses_compaction: 'OpenAI Responses Compaction',
  openai_alpha_search: 'OpenAI Alpha Search',
  openai_audio: 'OpenAI Audio',
  openai_image: 'OpenAI Images',
  openai_realtime: 'OpenAI Realtime',
  rerank: 'Rerank',
  embedding: 'Embeddings',
  task: 'Async Task',
  mj_proxy: 'Midjourney',
}

/**
 * The label key of a stored protocol value, which callers render through t().
 * Brand names translate to themselves; generic ones such as Async Task do not.
 */
export function getPromptAuditProtocolName(protocol: string): string {
  if (!protocol) return ''
  return PROMPT_AUDIT_PROTOCOL_NAMES[protocol] ?? getProtocolName(protocol)
}
