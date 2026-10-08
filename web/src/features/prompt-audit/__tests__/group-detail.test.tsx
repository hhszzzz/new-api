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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { PromptAuditGroupDetailSheet } from '../components/prompt-audit-group-detail-sheet'
import type {
  PromptAuditContentVersion,
  PromptAuditEvent,
  PromptAuditGroupTarget,
  PromptAuditRepeat,
} from '../types'

const apiMock = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/lib/api', () => ({ api: apiMock }))

type Params = Record<string, string | number | undefined>
const summary: PromptAuditRepeat = {
  count: 227,
  first_at: 100,
  last_at: 500,
  worst_decision: 'unavailable',
  blocks: 0,
  unavailable: 1,
  outcome_counts: { pass: 226, unavailable: 1 },
}
const main: PromptAuditContentVersion = {
  id: 17,
  kind: 'main',
  direction: 'input',
  count: 220,
  first_at: 100,
  last_at: 500,
  redacted_preview: 'main-preview',
}
const branch: PromptAuditContentVersion = {
  ...main,
  id: 18,
  kind: 'side:summary',
  count: 7,
  redacted_preview: 'branch-preview',
}
const event: PromptAuditEvent = {
  id: 17,
  request_id: 'req-17',
  user_id: 1,
  token_id: 1,
  token_name: 'key',
  username: 'operator',
  group: 'default',
  protocol: 'openai',
  model: 'test',
  stage: '',
  direction: 'input',
  generation_id: '',
  delivery_status: '',
  coverage_complete: true,
  config_version: '',
  execution_mode: 'blocking',
  status: 'done',
  prompt_hash: 'hash',
  prompt_length: 8,
  segment_count: 1,
  chunk_count: 1,
  full_prompt_available: true,
  full_prompt_truncated: false,
  full_prompt: 'raw-request-only',
  redacted_preview: 'request-preview',
  safety: '',
  refusal: '',
  decision: 'unavailable',
  action: 'allow',
  would_action: '',
  categories: [],
  unknown_categories: [],
  endpoint_id: '',
  endpoint_model: '',
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
  latency_ms: 0,
  attempts: 1,
  max_attempts: 1,
  next_attempt_at: 0,
  error_code: 'audit_timeout',
  ip: '',
  user_agent: '',
  method: '',
  request_path: '',
  origin: '',
  referer: '',
  created_at: 100,
  updated_at: 100,
  completed_at: 100,
  request_kind: 'prompt',
  scan_payload: JSON.stringify({
    segments: [
      { scope: 'user', text: 'continue' },
      { scope: 'system', text: 'system text' },
      { scope: 'user', text: 'continue' },
    ],
  }),
}
const target: PromptAuditGroupTarget = {
  id: 17,
  scope: 'filtered',
  filters: {
    start_time: 10,
    end_time: 999,
    decision: 'unavailable',
    model: 'filtered-model',
  },
}
const server = {
  event,
  maxID: 999,
  versions: [main, branch],
  hasSession: true,
  payload: event.scan_payload,
  summary,
  sessionKind: 'side:summary',
}
const clients: QueryClient[] = []

function renderGroup(canViewFullPrompt = true) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const onOpenRecord = vi.fn()
  function Harness() {
    const [selected] = useState(target)
    return (
      <PromptAuditGroupDetailSheet
        target={selected}
        canViewFullPrompt={canViewFullPrompt}
        onOpenChange={vi.fn()}
        onOpenRecord={onOpenRecord}
      />
    )
  }
  return {
    ...render(
      <QueryClientProvider client={client}>
        <Harness />
      </QueryClientProvider>
    ),
    client,
    onOpenRecord,
  }
}
function requests(suffix: string) {
  return apiMock.get.mock.calls.filter(([url]) => String(url).endsWith(suffix))
}
function plaintextRequests() {
  return apiMock.get.mock.calls.filter(([url]) =>
    /\/events\/\d+$/.test(String(url))
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  server.maxID = 999
  server.versions = [main, branch]
  server.hasSession = true
  server.payload = event.scan_payload
  server.event = event
  server.summary = summary
  server.sessionKind = 'side:summary'
  apiMock.get.mockImplementation(
    async (url: string, config?: { params: Params }) => {
      const params = config?.params ?? {}
      const page = Number(params.page ?? 1)
      const page_size = Number(params.page_size ?? 20)
      const max_id = Number(params.max_id ?? server.maxID)
      if (url.endsWith('/group-content')) {
        return {
          data: {
            success: true,
            data: {
              items:
                page === 1
                  ? server.versions
                  : [{ ...main, id: 30, redacted_preview: 'next-version' }],
              total: 21,
              page,
              page_size,
              max_id,
              summary: server.summary,
            },
          },
        }
      }
      if (url.endsWith('/session-questions')) {
        return {
          data: {
            success: true,
            data: {
              items: server.hasSession
                ? [
                    {
                      ...event,
                      id: 40,
                      redacted_preview: 'next-question',
                      repeat: summary,
                    },
                    {
                      ...event,
                      id: 41,
                      request_kind: server.sessionKind,
                      redacted_preview: 'branch-only-group',
                      repeat: summary,
                    },
                  ]
                : [],
              total: server.hasSession ? 2 : 0,
              page,
              page_size,
              max_id,
              has_session: server.hasSession,
            },
          },
        }
      }
      if (/\/events\/\d+$/.test(url)) {
        return {
          data: {
            success: true,
            data: { ...server.event, scan_payload: server.payload },
          },
        }
      }
      throw new Error(`Unexpected request ${url}`)
    }
  )
})
afterEach(() => {
  for (const client of clients.splice(0)) client.clear()
})

describe('question detail reading', () => {
  test('loads metadata and full summary without loading the session index', async () => {
    renderGroup()
    expect(
      await screen.findByRole('tab', { name: 'User messages' })
    ).toBeVisible()
    expect(screen.getByText('Passed 226')).toBeVisible()
    expect(screen.getByText('unavailable 1')).toBeVisible()
    expect(requests('/session-questions')).toHaveLength(0)
    const navigation = screen.getByRole('tablist', { name: 'Question details' })
    expect(within(navigation).getAllByRole('tab')).toHaveLength(2)
    expect(screen.queryByText('main-preview')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /Refresh/ })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('tab', { name: 'Inspected content' })
    ).toHaveAttribute('aria-selected', 'true')
    expect(requests('/group-content')[0][1].params).toMatchObject(
      target.filters
    )
    expect(requests('/group-content')[0][1].params.max_id).toBeUndefined()
  })

  test('keeps the summary and navigation outside the scrolling panel', async () => {
    renderGroup()
    await screen.findByRole('tab', { name: 'User messages' })
    const panel = screen.getByRole('tabpanel', { name: 'Inspected content' })
    expect(panel).toHaveClass(
      'min-h-0',
      'overflow-y-auto',
      'overscroll-contain'
    )
    const result = screen.getByRole('region', { name: 'Result' })
    const tabs = screen.getByRole('tablist', { name: 'Question details' })
    expect(panel).not.toContainElement(result)
    expect(panel).not.toContainElement(tabs)
    expect(result).toHaveClass('shrink-0')
    expect(tabs.parentElement).toHaveClass('shrink-0', 'overflow-x-auto')
  })

  test('reads the group summary as outcome badges plus one compact meta line', async () => {
    renderGroup()
    await screen.findByRole('tab', { name: 'User messages' })
    const result = screen.getByRole('region', { name: 'Result' })
    expect(within(result).getByText('Passed 226')).toBeVisible()
    expect(result).toHaveTextContent('Request count 227')
    expect(result).not.toHaveTextContent('Block actions 0')
    // A same-day range names its date only once, rather than wrapping it twice.
    const dates = result.textContent?.match(/\d{4}-\d{2}-\d{2}/g)
    expect(dates).toHaveLength(1)
  })

  test.each([
    { lastAt: 100, dates: 1, range: false },
    { lastAt: 90000, dates: 2, range: true },
  ])(
    'shows $dates date labels when the range ends at $lastAt',
    async ({ lastAt, dates, range }) => {
      server.summary = { ...summary, last_at: lastAt }
      renderGroup(false)
      const result = await screen.findByRole('region', { name: 'Result' })
      expect(result.textContent?.match(/\d{4}-\d{2}-\d{2}/g)).toHaveLength(
        dates
      )
      expect(result.textContent?.includes('→')).toBe(range)
    }
  )

  test('keeps nonzero actual blocks distinct from verdict counts', async () => {
    server.summary = { ...summary, blocks: 2 }
    renderGroup(false)
    const result = await screen.findByRole('region', { name: 'Result' })
    expect(within(result).getByText('Block actions 2')).toBeVisible()
  })

  test('keeps a version occurrence count on its meta row', async () => {
    renderGroup()
    await screen.findByRole('tab', { name: 'User messages' })
    const source = screen.getByRole('region', { name: 'Source record #17' })
    within(source).getByText(/Appears in 220 requests/)
  })

  test('separates multiple content cards and keeps record links outside wrapping metadata', async () => {
    server.versions = [main, { ...main, id: 19 }]
    renderGroup(false)
    const first = await screen.findByRole('region', {
      name: 'Source record #17',
    })
    expect(first.parentElement).toHaveClass('flex', 'flex-col', 'gap-4')
    const entry = within(first).getByRole('button', {
      name: 'Source record #17',
    })
    expect(entry).toHaveClass('shrink-0')
    expect(entry.parentElement).toHaveClass('flex', 'items-start')
    expect(entry.parentElement).not.toHaveClass('flex-wrap')
  })

  test('keeps repeated blocks, supports keyboard tabs and copies only the selected inspected source', async () => {
    const user = userEvent.setup()
    renderGroup()
    await screen.findByRole('tab', { name: 'User messages' })
    const source = screen.getByRole('region', { name: 'Source record #17' })
    const panel = await within(source).findByRole('tabpanel')
    expect(within(panel).getAllByText('continue')).toHaveLength(2)
    expect(within(panel).getAllByRole('separator')).toHaveLength(1)
    expect(screen.queryByText('raw-request-only')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Copy' }))
    expect(await navigator.clipboard.readText()).toBe('continue\n\ncontinue')
    screen.getByRole('tab', { name: 'User messages' }).focus()
    await user.keyboard('{ArrowRight}{Enter}')
    expect(await screen.findByText('system text')).toBeVisible()
    expect(
      screen.getByRole('tab', { name: 'System instructions' })
    ).toHaveAttribute('aria-selected', 'true')
  })

  test('keeps background user-role payload apart and does not call it a human message', async () => {
    const user = userEvent.setup()
    renderGroup()
    await screen.findByRole('tab', { name: 'User messages' })
    const toggle = screen.getByRole('button', {
      name: 'Subagent and background content',
    })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await user.click(toggle)
    const branchRegion = screen.getByRole('region', {
      name: 'Source record #18',
    })
    expect(within(branchRegion).getByText('Background: summary')).toBeVisible()
    expect(
      await within(branchRegion).findByRole('tab', {
        name: 'User-role content',
      })
    ).toBeVisible()
    expect(
      within(branchRegion).queryByRole('tab', { name: 'User messages' })
    ).not.toBeInTheDocument()
  })

  test.each([
    ['continuation', 'Summary continuation'],
    ['continuation:unresolved', 'Continuation linkage unresolved'],
  ])(
    'shows %s directly without presenting its payload as a human message',
    async (kind, label) => {
      server.versions = [{ ...main, kind }, branch]
      renderGroup()

      const source = await screen.findByRole('region', {
        name: 'Source record #17',
      })
      expect(within(source).getByText(label)).toBeVisible()
      expect(
        await within(source).findByRole('tab', { name: 'User-role content' })
      ).toBeVisible()
      expect(
        within(source).queryByRole('tab', { name: 'User messages' })
      ).not.toBeInTheDocument()
      expect(
        screen.queryByText('No main request content on this page')
      ).not.toBeInTheDocument()
      expect(
        screen.getByRole('button', { name: 'Subagent and background content' })
      ).toHaveAttribute('aria-expanded', 'false')
    }
  )

  test('never requests plaintext without permission even after browsing session questions', async () => {
    const user = userEvent.setup()
    renderGroup(false)
    await screen.findByText('main-preview')
    expect(
      screen.queryByRole('tab', { name: 'User messages' })
    ).not.toBeInTheDocument()
    expect(
      screen.getByText('Your permission only allows the redacted preview.')
    ).toBeVisible()
    await user.click(screen.getByRole('tab', { name: 'Session questions' }))
    expect(await screen.findByText('next-question')).toBeVisible()
    expect(plaintextRequests()).toHaveLength(0)
  })

  test('shows a concise missing-content state and keeps the source record accessible', async () => {
    server.payload = undefined
    renderGroup()
    expect(
      await screen.findByText('No inspected content retained')
    ).toBeVisible()
    expect(
      screen.getAllByRole('button', { name: /Source record #/ })[0]
    ).toBeEnabled()
  })

  test('opens grouped output on the reply and keeps the request sources available', async () => {
    server.event = { ...event, direction: 'output' }
    server.versions = [{ ...main, direction: 'output' }]
    server.payload = JSON.stringify({
      segments: [{ scope: 'user', text: 'Retained question' }],
      output: 'Retained generated reply',
    })
    renderGroup()
    expect(await screen.findByText('Retained generated reply')).toBeVisible()
    expect(
      screen.getByRole('tab', { name: 'Generated output' })
    ).toHaveAttribute('aria-selected', 'true')
    await userEvent.click(screen.getByRole('tab', { name: 'User messages' }))
    expect(screen.getByText('Retained question')).toBeVisible()
  })

  test.each([
    {
      payload: undefined,
      truncated: false,
      message: 'No content snapshot retained',
    },
    {
      payload: JSON.stringify({ output: 'Retained reply' }),
      truncated: true,
      message: 'Content snapshot was truncated to the retention limit.',
    },
  ])(
    'describes grouped stored content without claiming inspection ($truncated)',
    async ({ payload, truncated, message }) => {
      server.event = {
        ...event,
        status: 'stored',
        inspection_type: 'stored',
        scan_payload_truncated: truncated,
      }
      server.payload = payload
      renderGroup()
      expect(await screen.findByText(message)).toBeVisible()
      expect(
        screen.queryByText('No inspected content retained')
      ).not.toBeInTheDocument()
      expect(
        screen.queryByText(
          'Inspected content was truncated to the retention limit.'
        )
      ).not.toBeInTheDocument()
    }
  )

  test('paginates 21 content versions and opens a record from a source id', async () => {
    const user = userEvent.setup()
    const { onOpenRecord } = renderGroup()
    await screen.findByRole('tab', { name: 'User messages' })
    const versions = screen.getByRole('region', { name: 'Content versions' })
    await user.click(
      within(versions).getByRole('button', { name: 'Go to next page' })
    )
    expect(
      await screen.findByRole('tab', { name: 'User messages' })
    ).toBeVisible()
    expect(requests('/group-content').at(-1)?.[1].params).toMatchObject({
      page: 2,
      page_size: 20,
      max_id: 999,
    })
    await user.click(screen.getByRole('button', { name: 'Source record #30' }))
    expect(onOpenRecord).toHaveBeenCalledWith(30)
  })

  test('uses the initial database bound on later content pages', async () => {
    const user = userEvent.setup()
    renderGroup()
    await screen.findByRole('tab', { name: 'User messages' })
    const versions = screen.getByRole('region', { name: 'Content versions' })
    await user.click(
      within(versions).getByRole('button', { name: 'Go to next page' })
    )
    expect(
      await screen.findByRole('tab', { name: 'User messages' })
    ).toBeVisible()
    expect(requests('/group-content').at(-1)?.[1].params).toMatchObject({
      page: 2,
      max_id: 999,
    })
  })

  test('shows a read-only session index without jump controls', async () => {
    const user = userEvent.setup()
    renderGroup()
    await screen.findByRole('tab', { name: 'User messages' })
    await user.click(
      screen.getByRole('tab', {
        name: 'Session questions',
      })
    )
    const question = await screen.findByText('next-question')
    expect(question).toBeVisible()
    expect(
      screen.queryByRole('button', { name: /next-question/ })
    ).not.toBeInTheDocument()
    expect(requests('/session-questions')[0][1].params).toEqual({
      start_time: 10,
      end_time: 999,
      page: 1,
      page_size: 20,
      max_id: 999,
    })
  })

  test('labels branch-only questions instead of presenting them as human questions', async () => {
    const user = userEvent.setup()
    renderGroup()
    await screen.findByRole('tab', { name: 'User messages' })
    await user.click(
      screen.getByRole('tab', {
        name: 'Session questions',
      })
    )
    const question = await screen.findByText('branch-only-group')
    expect(question).toBeVisible()
    expect(screen.getByText(/Branch-only question group/)).toBeVisible()
    expect(screen.getByText(/Background: summary/)).toBeVisible()
  })

  test.each([
    ['continuation', 'Summary continuation'],
    ['continuation:unresolved', 'Continuation linkage unresolved'],
  ])(
    'labels %s session entries without calling them branch-only groups',
    async (kind, label) => {
      server.sessionKind = kind
      const user = userEvent.setup()
      renderGroup()
      await screen.findByRole('tab', { name: 'User messages' })
      await user.click(screen.getByRole('tab', { name: 'Session questions' }))

      expect(await screen.findByText(label)).toBeVisible()
      expect(
        screen.queryByText(/Branch-only question group/)
      ).not.toBeInTheDocument()
    }
  )

  test('explains that records without a session cannot be linked', async () => {
    server.hasSession = false
    const user = userEvent.setup()
    renderGroup()
    await screen.findByRole('tab', { name: 'User messages' })
    await user.click(
      screen.getByRole('tab', {
        name: 'Session questions',
      })
    )
    expect(
      await screen.findByText(
        'No session is recorded; related questions cannot be linked.'
      )
    ).toBeVisible()
  })

  test('keeps a bounded summary consistent when review invalidates audit queries', async () => {
    const { client } = renderGroup()
    await screen.findByRole('tab', { name: 'User messages' })
    await act(async () => {
      await client.invalidateQueries({ queryKey: ['prompt-audit'] })
    })
    expect(requests('/group-content').at(-1)?.[1].params.max_id).toBe(999)
  })

  test('shows loading and a retryable metadata failure without stale content', async () => {
    let reject: (error: Error) => void = () => {}
    apiMock.get.mockImplementationOnce(
      () =>
        new Promise((_resolve, fail) => {
          reject = fail
        })
    )
    const user = userEvent.setup()
    renderGroup()
    expect(screen.getByText('Loading audit details...')).toBeVisible()
    await act(async () => reject(new Error('metadata unavailable')))
    expect(await screen.findByText('metadata unavailable')).toBeVisible()
    expect(screen.queryByText('main-preview')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Retry' }))
    expect(
      await screen.findByRole('tab', { name: 'User messages' })
    ).toBeVisible()
  })

  test('uses compact pagination and constrains long inspected text on a narrow screen', async () => {
    const matchMedia = window.matchMedia
    vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
      ...matchMedia(query),
      matches: query === '(max-width: 640px)' || matchMedia(query).matches,
    }))
    const longText = 'long-payload-without-spaces'.repeat(300)
    server.payload = JSON.stringify({
      segments: [{ scope: 'user', text: longText }],
    })
    renderGroup()
    const text = await screen.findByText(longText)
    expect(text).toHaveClass('font-mono', 'break-words', 'whitespace-pre-wrap')
    expect(text.closest('[role="tabpanel"]')).toHaveClass(
      'max-h-80',
      'overflow-y-auto'
    )
    expect(text.closest('[role="tabpanel"]')).toHaveAttribute('tabindex', '0')
    expect(screen.getByRole('dialog')).toHaveClass('w-full')
    const content = screen.getByRole('region', { name: 'Content versions' })
    expect(
      within(content).getByRole('navigation', { name: 'Page' })
    ).toBeVisible()
    expect(
      within(content).getByRole('button', { name: 'Go to next page' })
    ).toHaveClass('size-11')
    expect(
      within(content).queryByRole('button', { name: 'Go to last page' })
    ).not.toBeInTheDocument()
  })

  test.each([
    {
      suffix: '/session-questions',
      trigger: 'Session questions',
      success: 'next-question',
    },
  ])(
    'shows a retryable error when $suffix fails without discarding the question summary',
    async ({ suffix, trigger, success }) => {
      const request = apiMock.get.getMockImplementation()
      let fail = true
      apiMock.get.mockImplementation(
        (url: string, config?: { params: Params }) => {
          if (url.endsWith(suffix) && fail) {
            return Promise.resolve({
              data: { success: false, message: 'reader unavailable' },
            })
          }
          return request?.(url, config)
        }
      )
      const user = userEvent.setup()
      renderGroup()
      await screen.findByRole('tab', { name: 'User messages' })
      await user.click(screen.getByRole('tab', { name: trigger }))
      expect(await screen.findByText('reader unavailable')).toBeVisible()
      expect(screen.getByText('Passed 226')).toBeVisible()
      fail = false
      await user.click(screen.getByRole('button', { name: 'Retry' }))
      expect(await screen.findByText(success)).toBeVisible()
    }
  )

  test('shows an empty content scope without inventing a prompt', async () => {
    server.versions = []
    renderGroup(false)
    expect(
      await screen.findByText('No content versions in this scope')
    ).toBeVisible()
    expect(plaintextRequests()).toHaveLength(0)
  })
})
