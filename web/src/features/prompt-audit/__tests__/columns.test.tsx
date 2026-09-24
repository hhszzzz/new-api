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
import { render, renderHook, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { DataTableView } from '@/components/data-table/core/data-table-view'
import { useDataTable } from '@/components/data-table/hooks/use-data-table'

import { usePromptAuditColumns } from '../components/prompt-audit-columns'
import { isMergedPromptAuditRow, promptAuditRowID } from '../lib'
import type { PromptAuditEvent, PromptAuditRepeat } from '../types'

// The interface language and the few translations a test needs; every other
// key reads as itself.
const i18nState = vi.hoisted(() => ({
  language: 'en',
  translations: {} as Record<string, string>,
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string | number>) =>
      Object.entries(values || {}).reduce(
        (result, [name, value]) => result.replace(`{{${name}}}`, String(value)),
        i18nState.translations[key] ?? key
      ),
    i18n: {
      language: i18nState.language,
      resolvedLanguage: i18nState.language,
    },
  }),
}))

afterEach(() => {
  i18nState.language = 'en'
  i18nState.translations = {}
})

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

// One audited text resent three times: an agent run submits the same text on
// every step, and the verdict cache only holds successes, so a timed-out attempt
// is retried and recorded as unavailable next to the block that followed it.
const REPEAT: PromptAuditRepeat = {
  count: 3,
  first_at: 1_784_999_400,
  last_at: 1_785_000_600,
  worst_decision: 'block',
  blocks: 1,
  unavailable: 1,
}

type ColumnOptions = {
  canDelete?: boolean
  collapsed?: boolean
  loadingGroupIDs?: readonly number[]
  groupTotals?: Readonly<Record<number, number>>
}

function columns(options: ColumnOptions = {}): ColumnDef<PromptAuditEvent>[] {
  return renderHook(() =>
    usePromptAuditColumns({
      canDelete: options.canDelete ?? false,
      onOpen: () => {},
      collapsed: options.collapsed,
      loadingGroupIDs: options.loadingGroupIDs,
      groupTotals: options.groupTotals,
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
  event: PromptAuditEvent = EVENT,
  options: ColumnOptions = {}
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

  render(<Harness defs={columns(options)} row={event} />)
  return screen.getByTestId('cell')
}

/** A collapsed row plus the requests the table renders under it. */
type PromptAuditListRow = PromptAuditEvent & { children?: PromptAuditEvent[] }

// The records table renders a collapsed row's requests as child rows, so the
// harness drives the real table options instead of a hand-built row model.
function renderCollapsedRow(
  event: PromptAuditListRow,
  options: Omit<ColumnOptions, 'collapsed'> = {}
) {
  // The columns come from a hook, so they are resolved before the table mounts
  // rather than from inside its render.
  const defs = columns({ ...options, collapsed: true })

  function Harness() {
    const { table } = useDataTable<PromptAuditListRow>({
      data: [event],
      columns: defs,
      getSubRows: (row) => row.children,
      // The page hands the table the same rule: a row holds its requests only
      // after it was opened, so it cannot be told apart from any other row by
      // the data alone.
      getRowCanExpand: (row) => isMergedPromptAuditRow(row.original),
      getRowId: promptAuditRowID,
      // Only the collapsed rows stand for a group, as on the page.
      enableRowSelection: (row) => row.depth === 0,
      withExpandedRowModel: true,
    })

    return (
      <>
        <div data-testid='row-ids'>
          {table
            .getRowModel()
            .rows.map((row) => row.id)
            .join(',')}
        </div>
        <DataTableView table={table} />
      </>
    )
  }

  render(<Harness />)
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

  test('counts the requests a merged row stands for', () => {
    const cell = renderCell('result', {
      ...EVENT,
      decision: 'block',
      repeat: REPEAT,
    })

    expect(cell.textContent).toContain('×3')
    // The count is the accessible name of the badge, not just its glyph.
    expect(
      screen.getByRole('img', {
        name: '3 requests belong to the same question',
      })
    ).toBeVisible()
  })

  test('shows no count on a request that stands alone', () => {
    const cell = renderCell('result')

    expect(cell.textContent).not.toContain('×')
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  })

  test('reads a group of one as the plain request it is', () => {
    const cell = renderCell(
      'result',
      { ...EVENT, repeat: { ...REPEAT, count: 1 } },
      { collapsed: true }
    )

    // Nothing is hidden behind a single request, so it carries neither a count
    // nor a toggle for a group it does not have.
    expect(cell.textContent).not.toContain('×')
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Expand' })
    ).not.toBeInTheDocument()
  })

  test('reads a merged row as the span its requests cover', () => {
    const cell = renderCell('created_at', { ...EVENT, repeat: REPEAT })

    // The day the requests started is named once, and both ends of the span sit
    // together under it as clock readings.
    expect(cell.textContent?.match(/\d{4}-\d{2}-\d{2}/g)).toHaveLength(1)
    const times = cell.textContent?.match(/\d{2}:\d{2}:\d{2}/g)
    expect(times).toHaveLength(2)
    expect(times?.[0]).not.toBe(times?.[1])
  })

  test('marks a revealed request as one of the run its group forms', async () => {
    const user = userEvent.setup()
    renderCollapsedRow({
      ...EVENT,
      repeat: REPEAT,
      children: [{ ...EVENT, redacted_preview: 'first-request' }],
    })

    await user.click(screen.getByRole('button', { name: 'Expand' }))

    const rows = document.querySelectorAll('tbody tr')
    expect(rows).toHaveLength(2)
    // Only the requests under the group hang off a rule; the group's own row
    // carries the count instead.
    expect(rows[0].querySelector('.prompt-audit-timeline')).toBeNull()
    expect(rows[1].querySelector('.prompt-audit-timeline')).not.toBeNull()
  })

  test('leaves a group of one with a single timestamp', () => {
    const cell = renderCell('created_at', {
      ...EVENT,
      repeat: { ...REPEAT, count: 1 },
    })

    const stamps = cell.textContent?.match(
      /\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}/g
    )
    expect(stamps).toHaveLength(1)
  })

  test('offers the group toggle only while the listing is collapsed', () => {
    renderCell('result', { ...EVENT, repeat: REPEAT })

    expect(
      screen.queryByRole('button', { name: 'Expand' })
    ).not.toBeInTheDocument()
  })

  test('keeps the group toggle out of a row the listing did not merge', () => {
    renderCell('result', EVENT, { collapsed: true })

    expect(
      screen.queryByRole('button', { name: 'Expand' })
    ).not.toBeInTheDocument()
  })

  test('toggles a collapsed row open and closed', async () => {
    const user = userEvent.setup()
    renderCollapsedRow({
      ...EVENT,
      repeat: REPEAT,
      children: [{ ...EVENT, redacted_preview: 'first-request' }],
    })

    const expand = screen.getByRole('button', { name: 'Expand' })
    expect(expand).toHaveAttribute('aria-expanded', 'false')
    expect(screen.getByTestId('row-ids')).toHaveTextContent('17')

    await user.click(expand)

    const collapse = screen.getByRole('button', { name: 'Collapse' })
    expect(collapse).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByTestId('row-ids')).toHaveTextContent('17,17:17')
  })

  test('opens a merged row whose requests have not arrived', async () => {
    const user = userEvent.setup()
    // The group is fetched on the click that opens the row, so at the moment it
    // is rendered the row carries the count but none of the requests.
    renderCollapsedRow({ ...EVENT, repeat: REPEAT })

    await user.click(screen.getByRole('button', { name: 'Expand' }))

    expect(screen.getByRole('button', { name: 'Collapse' })).toHaveAttribute(
      'aria-expanded',
      'true'
    )
  })

  test('expands a collapsed row into the requests it counted', async () => {
    const user = userEvent.setup()
    renderCollapsedRow({
      ...EVENT,
      repeat: REPEAT,
      children: [
        { ...EVENT, redacted_preview: 'first-request' },
        { ...EVENT, id: 18, redacted_preview: 'second-request' },
      ],
    })

    expect(screen.queryByText('second-request')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Expand' }))

    expect(screen.getByText('first-request')).toBeVisible()
    expect(screen.getByText('second-request')).toBeVisible()
    // The group's first request is one of its children, so the two rows must not
    // share an id.
    expect(screen.getByTestId('row-ids')).toHaveTextContent('17,17:17,17:18')
  })

  test('names the protocol in the interface language', () => {
    i18nState.translations = { 'Async Task': 'Tâche asynchrone' }

    const cell = renderCell('request', { ...EVENT, protocol: 'task' })

    expect(lines(cell)).toContain('Protocol: Tâche asynchrone')
  })

  test('marks the group toggle busy while its requests are loading', () => {
    renderCell(
      'result',
      { ...EVENT, repeat: REPEAT },
      { collapsed: true, loadingGroupIDs: [17] }
    )

    expect(screen.getByRole('button', { name: 'Expand' })).toHaveAttribute(
      'aria-busy',
      'true'
    )
  })

  test('keeps the toggle of a group that is not loading idle', () => {
    renderCell(
      'result',
      { ...EVENT, repeat: REPEAT },
      { collapsed: true, loadingGroupIDs: [42] }
    )

    expect(screen.getByRole('button', { name: 'Expand' })).not.toHaveAttribute(
      'aria-busy',
      'true'
    )
  })

  test('says when an expanded group holds more requests than it revealed', async () => {
    const user = userEvent.setup()
    // An expansion returns only the newest requests of a group, while the group
    // reports how many it holds in all.
    renderCollapsedRow(
      {
        ...EVENT,
        repeat: { ...REPEAT, count: 1234 },
        children: [
          { ...EVENT, id: 18 },
          { ...EVENT, id: 19 },
        ],
      },
      { groupTotals: { 17: 1234 } }
    )

    expect(screen.queryByText(/^Showing the newest/)).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Expand' }))

    expect(
      screen.getByText('Showing the newest 2 of 1,234 requests')
    ).toBeVisible()
  })

  test('adds no note to a group whose requests all arrived', async () => {
    const user = userEvent.setup()
    renderCollapsedRow(
      {
        ...EVENT,
        repeat: { ...REPEAT, count: 2 },
        children: [
          { ...EVENT, id: 18 },
          { ...EVENT, id: 19 },
        ],
      },
      { groupTotals: { 17: 2 } }
    )

    await user.click(screen.getByRole('button', { name: 'Expand' }))

    expect(screen.queryByText(/^Showing the newest/)).not.toBeInTheDocument()
  })

  test('gives the requests of an expanded group no checkbox of their own', async () => {
    const user = userEvent.setup()
    renderCollapsedRow(
      {
        ...EVENT,
        repeat: REPEAT,
        children: [{ ...EVENT, id: 18, redacted_preview: 'second-request' }],
      },
      { canDelete: true }
    )

    await user.click(screen.getByRole('button', { name: 'Expand' }))

    const rows = document.querySelectorAll<HTMLElement>('tbody tr')
    expect(rows).toHaveLength(2)
    expect(
      within(rows[0]).getByRole('checkbox', { name: 'Select audit record' })
    ).toBeVisible()
    expect(within(rows[1]).queryByRole('checkbox')).not.toBeInTheDocument()
  })

  test.each([
    { language: 'en', count: '1,234' },
    { language: 'zhCN', count: '1,234' },
    { language: 'zhTW', count: '1,234' },
    { language: 'fr', count: '1\u202f234' },
    { language: 'ru', count: '1\u00a0234' },
    { language: 'ja', count: '1,234' },
    { language: 'vi', count: '1.234' },
    // An interface language Intl cannot read falls back to the runtime default
    // instead of breaking the row.
    { language: 'not a language', count: (1234).toLocaleString() },
  ])('formats the repeat counts for $language', ({ language, count }) => {
    i18nState.language = language

    renderCell('result', { ...EVENT, repeat: { ...REPEAT, count: 1234 } })

    const badge = screen.getByRole('img', {
      name: `${count} requests belong to the same question`,
    })
    expect(badge.textContent).toBe(`×${count}`)
  })

  test('re-formats the repeat count when the interface language changes', () => {
    const event = { ...EVENT, repeat: { ...REPEAT, count: 1234 } }
    function ResultCell() {
      const defs = usePromptAuditColumns({ canDelete: false, onOpen: () => {} })
      // eslint-disable-next-line react/incompatible-library -- the test renders one static row and never memoizes its options.
      const table = useReactTable({
        data: [event],
        columns: defs,
        getCoreRowModel: getCoreRowModel(),
      })
      const cell = table
        .getRowModel()
        .rows[0].getVisibleCells()
        .find((item) => item.column.id === 'result')
      if (!cell?.column.columnDef.cell) return null
      return flexRender(cell.column.columnDef.cell, cell.getContext())
    }
    const view = render(<ResultCell />)
    expect(screen.getByRole('img').textContent).toBe('×1,234')

    i18nState.language = 'fr'
    view.rerender(<ResultCell />)

    expect(screen.getByRole('img').textContent).toBe('×1\u202f234')
  })
})
