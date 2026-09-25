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
import { afterEach, describe, expect, test, vi } from 'vitest'

import {
  EMPTY_PROMPT_AUDIT_FILTERS,
  FULL_PROMPT_MAX_RUNES,
  FULL_PROMPT_MIN_RUNES,
  getDefaultPromptAuditFilters,
  getPromptAuditProtocolName,
  isMergedPromptAuditRow,
  PROMPT_AUDIT_PROTOCOLS,
  promptAuditDeleteFilter,
  promptAuditEndpointBaseURLUpdate,
  promptAuditEndpointDrafts,
  promptAuditEndpointUpdate,
  promptAuditFilterParams,
  promptAuditOutcome,
  promptAuditPayloadSources,
  type PromptAuditEndpointDraft,
  promptAuditRowID,
  readPromptAuditCollapseRepeats,
  validatePromptAuditConfig,
  validatePromptAuditFilters,
  writePromptAuditCollapseRepeats,
} from '../lib'
import type { PromptAuditConfigUpdate, PromptAuditEvent } from '../types'

const VALID_CONFIG: PromptAuditConfigUpdate = {
  mode: 'blocking',
  output_mode: 'off',
  blocking_latest_turn_only: true,
  manual_wordlist_action: 'block',
  enabled_categories: ['violent'],
  controversial_block_categories: [],
  review_enabled: false,
  review_prompt: '',
  all_groups: true,
  groups: [],
  endpoints: [
    {
      id: 'primary',
      name: 'Primary',
      base_url: 'https://guard.example.com/v1',
      model: 'qwen3guard',
      timeout_ms: 3000,
      input_limit: 4000,
      concurrency: 16,
      enabled: true,
      purpose: 'classify',
      directions: ['input', 'output'],
    },
  ],
  total_timeout_ms: 10000,
  chunk_overlap: 64,
  chunk_concurrency: 4,
  cache_ttl_seconds: 600,
  worker_count: 4,
  max_attempts: 4,
  retention_days: 30,
  global_concurrency: 64,
  endpoint_concurrency: 16,
  output_max_bytes: 8 * 1024 * 1024,
  output_memory_bytes: 1024 * 1024,
}

describe('prompt audit management helpers', () => {
  afterEach(() => vi.useRealTimers())

  test('defaults to the common log time range for today', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 7, 6, 10, 30))

    expect(getDefaultPromptAuditFilters()).toMatchObject({
      start_time: '2026-08-06T00:00',
      end_time: '2026-08-06T11:30',
    })
  })

  test('serializes only active filters and converts browser times to epoch seconds', () => {
    const params = promptAuditFilterParams({
      ...EMPTY_PROMPT_AUDIT_FILTERS,
      status: 'failed',
      username: ' uidemo ',
      request_id: ' req-123 ',
      start_time: '2026-08-06T10:30',
    })

    expect(params.status).toBe('failed')
    expect(params.username).toBe('uidemo')
    expect(params.request_id).toBe('req-123')
    expect(params.start_time).toBe(
      Math.floor(new Date('2026-08-06T10:30').getTime() / 1000)
    )
    expect(params.model).toBeUndefined()
  })

  test('uses selected IDs instead of broad filters for batch deletion', () => {
    const filter = promptAuditDeleteFilter(
      { ...EMPTY_PROMPT_AUDIT_FILTERS, status: 'failed' },
      [9, 3]
    )

    expect(filter).toEqual({ ids: [9, 3] })
  })

  test('deletes selected merged rows as whole groups within the current filters', () => {
    const filter = promptAuditDeleteFilter(
      { ...EMPTY_PROMPT_AUDIT_FILTERS, status: 'failed', detector: 'model' },
      [17, 42],
      { groups: true }
    )

    // Unset filters stay undefined, which the JSON body drops: the request
    // carries the listing filters and the groups, never bare ids or view flags.
    expect(filter).toEqual({
      status: 'failed',
      detector: 'model',
      group_ids: [17, 42],
    })
  })

  test('sends the detector filter only while one is chosen', () => {
    expect(
      promptAuditFilterParams(EMPTY_PROMPT_AUDIT_FILTERS).detector
    ).toBeUndefined()
    expect(
      promptAuditFilterParams({
        ...EMPTY_PROMPT_AUDIT_FILTERS,
        detector: 'wordlist',
      }).detector
    ).toBe('wordlist')
  })

  test('gives every stored protocol a label of its own', () => {
    const labels = PROMPT_AUDIT_PROTOCOLS.map((protocol) =>
      getPromptAuditProtocolName(protocol)
    )

    // The compaction endpoint used to borrow the Responses name, so the filter
    // offered two options that read the same.
    expect(getPromptAuditProtocolName('openai_responses_compaction')).toBe(
      'OpenAI Responses Compaction'
    )
    expect(new Set(labels).size).toBe(PROMPT_AUDIT_PROTOCOLS.length)
  })

  test('asks for the merged listing only while it is switched on', () => {
    const filters = { ...EMPTY_PROMPT_AUDIT_FILTERS, status: 'failed' }

    expect(promptAuditFilterParams(filters).collapse_repeats).toBeUndefined()
    expect(
      promptAuditFilterParams(filters, { collapseRepeats: true })
        .collapse_repeats
    ).toBe('true')
    // Merging is a way of reading the same filters, never a filter of its own.
    expect(
      promptAuditFilterParams(filters, { collapseRepeats: true }).status
    ).toBe('failed')
  })

  test('asks for one merged row only with the id of its representative', () => {
    const filters = { ...EMPTY_PROMPT_AUDIT_FILTERS }

    expect(promptAuditFilterParams(filters).group_id).toBeUndefined()
    expect(
      promptAuditFilterParams(filters, { groupID: 0 }).group_id
    ).toBeUndefined()
    expect(promptAuditFilterParams(filters, { groupID: 42 }).group_id).toBe(42)
  })

  test('opens the listing merged and keeps whatever the operator chose instead', () => {
    window.localStorage.clear()

    // A first visit reads the listing merged: that is what an agent run looks
    // like, and the switch is still there to read it request by request.
    expect(readPromptAuditCollapseRepeats()).toBe(true)

    writePromptAuditCollapseRepeats(false)
    expect(readPromptAuditCollapseRepeats()).toBe(false)

    writePromptAuditCollapseRepeats(true)
    expect(readPromptAuditCollapseRepeats()).toBe(true)
  })

  test('keeps a merged row and the requests it reveals apart', () => {
    // The request that speaks for its group is also one of its children, so a
    // child's id carries its parent's and the two rows stay distinguishable.
    expect(promptAuditRowID({ id: 17 } as PromptAuditEvent, 0)).toBe('17')
    expect(
      promptAuditRowID({ id: 17 } as PromptAuditEvent, 0, { id: '17' })
    ).toBe('17:17')
    expect(
      promptAuditRowID({ id: 18 } as PromptAuditEvent, 1, { id: '17' })
    ).toBe('17:18')
  })

  test('counts only the groups that hold more than one request', () => {
    const repeat = (count: number) => ({ count }) as PromptAuditEvent['repeat']

    expect(isMergedPromptAuditRow({ id: 17 } as PromptAuditEvent)).toBe(false)
    expect(
      isMergedPromptAuditRow({
        id: 17,
        repeat: repeat(1),
      } as PromptAuditEvent)
    ).toBe(false)
    expect(
      isMergedPromptAuditRow({
        id: 17,
        repeat: repeat(2),
      } as PromptAuditEvent)
    ).toBe(true)
  })

  test('does not resend a stored token until the administrator changes it', () => {
    const endpoint: PromptAuditEndpointDraft = {
      ...VALID_CONFIG.endpoints[0],
      client_key: 'primary',
      original_id: 'primary',
      original_base_url: 'https://guard.example.com/v1',
      has_token: true,
      token: '',
      token_changed: false,
    }
    expect(promptAuditEndpointUpdate(endpoint)).toMatchObject({
      id: 'primary',
      original_id: 'primary',
    })
    expect(promptAuditEndpointUpdate(endpoint)).not.toHaveProperty('token')

    const renamed = promptAuditEndpointUpdate({
      ...endpoint,
      id: 'renamed',
    })
    expect(renamed).toMatchObject({ id: 'renamed', original_id: 'primary' })
    expect(renamed).not.toHaveProperty('token')

    expect(
      promptAuditEndpointUpdate({ ...endpoint, token_changed: true })
    ).toHaveProperty('token', '')
  })

  test('clears write-only browser secrets after save and when the node URL changes', () => {
    const [saved] = promptAuditEndpointDrafts([
      {
        ...VALID_CONFIG.endpoints[0],
        has_token: true,
      },
    ])
    expect(saved.token).toBe('')
    expect(saved.token_changed).toBe(false)

    expect(
      promptAuditEndpointBaseURLUpdate(saved, 'https://guard.example.com/v1/')
    ).toEqual({ base_url: 'https://guard.example.com/v1/' })
    expect(
      promptAuditEndpointBaseURLUpdate(
        saved,
        'https://different.example.com/v1'
      )
    ).toMatchObject({
      base_url: 'https://different.example.com/v1',
      token: '',
      token_changed: true,
    })
  })

  test('accepts a complete blocking configuration', () => {
    expect(validatePromptAuditConfig(VALID_CONFIG)).toBeNull()
  })

  test('rejects enabled gray review when all nodes have been removed', () => {
    expect(
      validatePromptAuditConfig({
        ...VALID_CONFIG,
        mode: 'off',
        output_mode: 'off',
        review_enabled: true,
        endpoints: [],
      })
    ).toBe('At least one enabled gray-area reviewer node is required.')
  })

  test('allows administrators to disable every known blocking category', () => {
    expect(
      validatePromptAuditConfig({
        ...VALID_CONFIG,
        enabled_categories: [],
      })
    ).toBeNull()
  })

  test('rejects URL credentials, query strings, and fragment-bearing nodes', () => {
    for (const base_url of [
      'https://user:secret@guard.example.com/v1',
      'https://guard.example.com/v1?token=secret',
      'https://guard.example.com/v1#secret',
    ]) {
      expect(
        validatePromptAuditConfig({
          ...VALID_CONFIG,
          endpoints: [{ ...VALID_CONFIG.endpoints[0], base_url }],
        })
      ).toMatch(/without credentials/)
    }
  })

  test('rejects an overlap that reaches the smallest enabled node limit', () => {
    expect(
      validatePromptAuditConfig({
        ...VALID_CONFIG,
        chunk_overlap: 256,
        endpoints: [{ ...VALID_CONFIG.endpoints[0], input_limit: 256 }],
      })
    ).toMatch(/smaller/)
  })

  test('rejects non-integer and out-of-range numeric settings', () => {
    expect(
      validatePromptAuditConfig({ ...VALID_CONFIG, max_attempts: 5 })
    ).toMatch(/whole numbers/)
    expect(
      validatePromptAuditConfig({ ...VALID_CONFIG, worker_count: 1.5 })
    ).toMatch(/whole numbers/)
    expect(
      validatePromptAuditConfig({
        ...VALID_CONFIG,
        endpoints: [{ ...VALID_CONFIG.endpoints[0], input_limit: 255 }],
      })
    ).toMatch(/node numeric values/)
  })

  test('accepts a stored prompt limit at the floor, the default, and the ceiling', () => {
    for (const limit of [FULL_PROMPT_MIN_RUNES, 65536, FULL_PROMPT_MAX_RUNES]) {
      expect(
        validatePromptAuditConfig({
          ...VALID_CONFIG,
          full_prompt_max_runes: limit,
        })
      ).toBeNull()
    }
  })

  test('accepts zero, which is how keeping the whole request is stored', () => {
    expect(
      validatePromptAuditConfig({ ...VALID_CONFIG, full_prompt_max_runes: 0 })
    ).toBeNull()
  })

  test('rejects a stored prompt limit above the ceiling or just below the floor', () => {
    // The values between 0 and the floor are the dangerous ones: they keep so
    // little of the request that the record is useless, and one of them is what
    // a mistyped number field produces.
    for (const limit of [1, FULL_PROMPT_MIN_RUNES - 1]) {
      expect(
        validatePromptAuditConfig({
          ...VALID_CONFIG,
          full_prompt_max_runes: limit,
        })
      ).toMatch(/stored prompt limit/)
    }
    expect(
      validatePromptAuditConfig({
        ...VALID_CONFIG,
        full_prompt_max_runes: FULL_PROMPT_MAX_RUNES + 1,
      })
    ).toMatch(/stored prompt limit/)
  })

  test('reads every block of one source as that source, in wire order', () => {
    const sources = promptAuditPayloadSources(
      JSON.stringify({
        segments: [
          { scope: 'system', text: 'You are an agent' },
          { scope: 'user', text: 'First block' },
          { scope: 'tool_result', text: 'Extracted tool output' },
          { scope: 'user', text: 'Second block' },
          { text: 'Unstamped block' },
        ],
      })
    )

    expect(sources.map((source) => source.key)).toEqual([
      'system',
      'user',
      'tool_result',
      'unknown',
    ])
    expect(sources[1]).toEqual({
      key: 'user',
      scope: 'user',
      text: 'First block\n\nSecond block',
    })
  })

  test('reads a generated output as the assistant source', () => {
    expect(
      promptAuditPayloadSources(JSON.stringify({ output: 'Generated text' }))
    ).toEqual([{ key: 'assistant', scope: 'assistant', text: 'Generated text' }])
  })

  test('reads a payload without a readable envelope as one unknown source', () => {
    expect(promptAuditPayloadSources('plain text')).toEqual([
      { key: 'unknown', scope: undefined, text: 'plain text' },
    ])
  })

  test('rejects an inverted time range', () => {
    expect(
      validatePromptAuditFilters({
        ...EMPTY_PROMPT_AUDIT_FILTERS,
        start_time: '2026-08-07T12:00',
        end_time: '2026-08-07T11:00',
      })
    ).toMatch(/Start time/)
  })

  test('reads a finished record by its verdict and an unfinished one by its status', () => {
    expect(promptAuditOutcome({ status: 'done', decision: 'pass' })).toEqual({
      key: 'pass',
      variant: 'secondary',
    })
    expect(promptAuditOutcome({ status: 'done', decision: 'block' })).toEqual({
      key: 'block',
      variant: 'destructive',
    })
    expect(promptAuditOutcome({ status: 'queued', decision: '' })).toEqual({
      key: 'queued',
      variant: 'outline',
    })
    expect(promptAuditOutcome({ status: 'processing', decision: '' })).toEqual({
      key: 'processing',
      variant: 'warning',
    })
  })

  test('keeps a failed record explicit while naming the verdict it did reach', () => {
    expect(promptAuditOutcome({ status: 'failed', decision: '' })).toEqual({
      key: 'failed',
      variant: 'destructive',
    })
    expect(
      promptAuditOutcome({ status: 'failed', decision: 'unavailable' })
    ).toEqual({ key: 'unavailable', variant: 'destructive' })
  })

  test('never leaves a record without a reading', () => {
    // Still waiting means no verdict and no terminal status, which is the state
    // the two separate badges used to show as an empty decision slot.
    expect(promptAuditOutcome({ status: '', decision: '' })).toEqual({
      key: 'pending',
      variant: 'outline',
    })
  })
})
