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
import { describe, expect, test } from 'vitest'

import {
  EMPTY_PROMPT_AUDIT_FILTERS,
  promptAuditDeleteFilter,
  promptAuditEndpointBaseURLUpdate,
  promptAuditEndpointDrafts,
  promptAuditEndpointUpdate,
  promptAuditFilterParams,
  type PromptAuditEndpointDraft,
  validatePromptAuditConfig,
  validatePromptAuditFilters,
} from '../lib'
import type { PromptAuditConfigUpdate } from '../types'

const VALID_CONFIG: PromptAuditConfigUpdate = {
  mode: 'blocking',
  enabled_categories: ['violent'],
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
    },
  ],
  total_timeout_ms: 10000,
  chunk_overlap: 64,
  cache_ttl_seconds: 600,
  worker_count: 4,
  max_attempts: 4,
  retention_days: 30,
  global_concurrency: 64,
  endpoint_concurrency: 16,
}

describe('prompt audit management helpers', () => {
  test('serializes only active filters and converts browser times to epoch seconds', () => {
    const params = promptAuditFilterParams({
      ...EMPTY_PROMPT_AUDIT_FILTERS,
      status: 'failed',
      user_id: '42',
      request_id: ' req-123 ',
      start_time: '2026-08-06T10:30',
    })

    expect(params.status).toBe('failed')
    expect(params.user_id).toBe(42)
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

  test('rejects an inverted time range', () => {
    expect(
      validatePromptAuditFilters({
        ...EMPTY_PROMPT_AUDIT_FILTERS,
        start_time: '2026-08-07T12:00',
        end_time: '2026-08-07T11:00',
      })
    ).toMatch(/Start time/)
  })
})
