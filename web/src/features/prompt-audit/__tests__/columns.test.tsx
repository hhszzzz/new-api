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
import {
  type ColumnDef,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { render, renderHook, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { usePromptAuditColumns } from '../components/prompt-audit-columns'
import type { PromptAuditEvent } from '../types'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string | number>) =>
      Object.entries(values || {}).reduce(
        (result, [name, value]) => result.replace(`{{${name}}}`, String(value)),
        key
      ),
    i18n: { language: 'en', resolvedLanguage: 'en' },
  }),
}))

// endpoint_id matches the real production shape: an opaque node id that used to be
// rendered into the Request column, where only the model name is readable.
const EVENT: PromptAuditEvent = {
  id: 17,
  request_id: 'req-prompt-audit',
  user_id: 9,
  token_id: 3,
  token_name: 'production',
  username: 'audit-user',
  group: 'default',
  protocol: 'openai_responses',
  model: 'gpt-test',
  stage: 'responses_websocket',
  direction: 'input',
  generation_id: '',
  delivery_status: 'not_applicable',
  coverage_complete: true,
  config_version: 'config-v1',
  execution_mode: 'blocking',
  status: 'done',
  prompt_hash: 'a'.repeat(64),
  prompt_length: 21,
  segment_count: 1,
  chunk_count: 1,
  full_prompt_available: false,
  full_prompt_truncated: false,
  redacted_preview: 'redacted-preview',
  safety: 'Safe',
  refusal: '',
  decision: 'pass',
  action: 'allow',
  would_action: 'pass',
  categories: [],
  unknown_categories: [],
  endpoint_id: 'endpoint-1789917593592',
  endpoint_model: 'qwen3guard-gen-0.6b',
  review_status: '',
  review_decision: '',
  review_codes: [],
  review_reason: '',
  reviewer_endpoint_id: '',
  human_review: '',
  human_review_reason: '',
  reviewed_by: 0,
  reviewer_name: '',
  reviewed_at: 0,
  latency_ms: 8,
  attempts: 1,
  max_attempts: 4,
  next_attempt_at: 0,
  error_code: '',
  ip: '192.0.2.1',
  user_agent: 'claude-code/2.0.30',
  method: 'POST',
  request_path: '/v1/responses',
  origin: 'https://console.example.com',
  referer: 'https://console.example.com/keys',
  created_at: 1_785_000_000,
  updated_at: 1_785_000_000,
  completed_at: 1_785_000_001,
}

function columns(): ColumnDef<PromptAuditEvent>[] {
  return renderHook(() =>
    usePromptAuditColumns({
      canDelete: false,
      onOpen: () => {},
    })
  ).result.current
}

// Each rendered line of a multi-line cell is a direct child element, so the
// children of the cell are the lines an operator sees.
function lines(cell: HTMLElement): (string | null)[] {
  const stack = cell.firstElementChild
  if (!stack) return []
  return [...stack.children].map((line) => line.textContent)
}

function renderCell(
  columnID: string,
  event: PromptAuditEvent = EVENT
): HTMLElement {
  function Harness({
    defs,
    row,
  }: {
    defs: ColumnDef<PromptAuditEvent>[]
    row: PromptAuditEvent
  }) {
    // eslint-disable-next-line react/incompatible-library -- the test renders one static row and never memoizes its options.
    const table = useReactTable({
      data: [row],
      columns: defs,
      getCoreRowModel: getCoreRowModel(),
    })
    const cell = table
      .getRowModel()
      .rows[0].getVisibleCells()
      .find((item) => item.column.id === columnID)
    if (!cell?.column.columnDef.cell) return null
    return (
      <div data-testid='cell'>
        {flexRender(cell.column.columnDef.cell, cell.getContext())}
      </div>
    )
  }

  render(<Harness defs={columns()} row={event} />)
  return screen.getByTestId('cell')
}

describe('prompt audit records table', () => {
  test('reads the identity column as username, group and key', () => {
    const cell = renderCell('identity')

    expect(lines(cell)).toEqual([
      'audit-user',
      'Group: default',
      'Key: production',
    ])
  })

  test('falls back to the user id when the username is unknown', () => {
    const cell = renderCell('identity', { ...EVENT, username: '' })

    expect(lines(cell)[0]).toBe('#9')
  })

  test('reads the request column as model, protocol and audit model', () => {
    const cell = renderCell('request')

    expect(lines(cell)).toEqual([
      'gpt-test',
      'Protocol: OpenAI Responses',
      'Audit model: qwen3guard-gen-0.6b',
    ])
  })

  test('names a protocol the shared map already knows', () => {
    const cell = renderCell('request', { ...EVENT, protocol: 'claude' })

    expect(lines(cell)).toContain('Protocol: Anthropic Messages')
  })

  test('never shows the node id or the delivery status in the row', () => {
    const cell = renderCell('request')

    expect(cell.textContent).not.toContain('endpoint-1789917593592')
    expect(cell.textContent).not.toContain('not_applicable')
  })

  test('keeps the record id, request id and prompt hash out of the row', () => {
    const cell = renderCell('request')

    expect(cell.textContent).not.toContain('#17')
    expect(cell.textContent).not.toContain('req-prompt-audit')
    expect(cell.textContent).not.toContain('a'.repeat(64))
  })

  test('renders the time column as the timestamp alone', () => {
    const cell = renderCell('created_at')

    expect(cell.children).toHaveLength(1)
    expect(cell.textContent).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/)
    expect(cell.textContent).not.toContain('#17')
  })

  test('renders the prompt column as the preview alone', () => {
    const cell = renderCell('prompt', {
      ...EVENT,
      full_prompt_available: true,
    })

    expect(cell.textContent).toBe('redacted-preview')
    expect(cell.textContent).not.toContain('Full prompt')
  })

  test('labels the last column as details', () => {
    const column = columns().find((def) => def.id === 'actions')
    const meta = column?.meta as { label?: string } | undefined

    expect(column?.header).toBe('Details')
    expect(meta?.label).toBe('Details')
  })

  test('renders the client ip and user agent on separate lines', () => {
    const cell = renderCell('client')

    expect(lines(cell)).toEqual(['192.0.2.1', 'claude-code/2.0.30'])
  })

  test('leaves the client column hideable and off the compact card', () => {
    const column = columns().find((def) => def.id === 'client')
    const meta = column?.meta as
      | { label?: string; mobileHidden?: boolean; mobileOrder?: number }
      | undefined

    expect(column).toBeDefined()
    expect(column?.enableHiding).not.toBe(false)
    expect(meta?.label).toBe('Client')
    expect(meta?.mobileHidden).toBe(true)
  })
})
