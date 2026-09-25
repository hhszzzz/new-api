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
import { type QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import {
  act,
  getDefaultNormalizer,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { createAppQueryClient } from '@/lib/query-client'
import { useAuthStore } from '@/stores/auth-store'

import { PromptAuditRecords } from '../records'
import type { PromptAuditEvent } from '../types'

const apiMock = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  delete: vi.fn(),
}))
vi.mock('@/lib/api', () => ({ api: apiMock }))
vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

type Params = Record<string, string | number | undefined>

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
  endpoint_id: 'guard-primary',
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

// The row the collapsed listing returns for three requests that submitted the
// same text; expanding it reveals them.
const MERGED: PromptAuditEvent = {
  ...EVENT,
  repeat: {
    count: 3,
    first_at: 1_784_999_400,
    last_at: 1_785_000_600,
    worst_decision: 'block',
    blocks: 1,
    unavailable: 0,
  },
}
const MERGED_REQUESTS: PromptAuditEvent[] = [
  { ...EVENT, id: 19, redacted_preview: 'third-request' },
  { ...EVENT, id: 18, redacted_preview: 'second-request' },
  { ...EVENT, id: 17, redacted_preview: 'first-request' },
]

// What the server holds: the listing pages and the requests of each group.
// A group missing here is answered the way the server answers a deleted one.
const server = {
  pages: [] as PromptAuditEvent[][],
  total: 0,
  decisions: {} as Record<string, number>,
  groups: {} as Record<number, PromptAuditEvent[]>,
}
const clients: QueryClient[] = []

function renderRecords() {
  const client = createAppQueryClient()
  // A failed query reports at once instead of after the production backoff.
  client.setDefaultOptions({
    queries: { ...client.getDefaultOptions().queries, retry: false },
  })
  clients.push(client)
  const root = createRootRoute({
    component: () => (
      <QueryClientProvider client={client}>
        <PromptAuditRecords />
      </QueryClientProvider>
    ),
  })
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  return { ...render(<RouterProvider router={router} />), client }
}

function listingRequests(matches: (params: Params) => boolean = () => true) {
  return apiMock.get.mock.calls.filter(
    ([url, config]) =>
      url === '/api/prompt-audit/events' &&
      config?.params?.group_id === undefined &&
      matches(config?.params ?? {})
  )
}

function groupRequests(groupID: number) {
  return apiMock.get.mock.calls.filter(
    ([url, config]) =>
      url === '/api/prompt-audit/events' && config?.params?.group_id === groupID
  )
}

function statsRequests() {
  return apiMock.get.mock.calls.filter(
    ([url]) => url === '/api/prompt-audit/stats'
  )
}

async function expandGroup(user: ReturnType<typeof userEvent.setup>) {
  const table = await screen.findByRole('table')
  await user.click(await within(table).findByRole('button', { name: 'Expand' }))
  expect(await screen.findByText('second-request')).toBeVisible()
}

async function deleteSelectedRow(user: ReturnType<typeof userEvent.setup>) {
  await user.click(
    await screen.findByRole('checkbox', { name: 'Select audit record' })
  )
  await user.click(screen.getByRole('button', { name: 'Delete selected (1)' }))
}

beforeEach(() => {
  vi.clearAllMocks()
  window.localStorage.clear()
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
  server.pages = [[MERGED]]
  server.total = 1
  server.decisions = {}
  server.groups = { 17: MERGED_REQUESTS }
  apiMock.get.mockImplementation(
    async (url: string, config?: { params?: Params }) => {
      const params = config?.params ?? {}
      if (url === '/api/prompt-audit/categories') {
        return { data: { success: true, data: [] } }
      }
      if (url === '/api/prompt-audit/stats') {
        return {
          data: {
            success: true,
            data: {
              total: server.total,
              statuses: {},
              decisions: server.decisions,
              categories: {},
              unknown_categories: 0,
            },
          },
        }
      }
      if (url !== '/api/prompt-audit/events') {
        throw new Error(`Unexpected API request: ${url}`)
      }
      if (params.group_id !== undefined) {
        const items = server.groups[Number(params.group_id)]
        if (!items) {
          return { data: { success: false, message: 'prompt audit not found' } }
        }
        return {
          data: {
            success: true,
            data: {
              items,
              total: items.length,
              page: 1,
              page_size: items.length,
            },
          },
        }
      }
      const page = Number(params.page ?? 1)
      return {
        data: {
          success: true,
          data: {
            items: server.pages[page - 1] ?? [],
            total: server.total,
            page,
            page_size: Number(params.page_size ?? 20),
          },
        },
      }
    }
  )
  apiMock.post.mockResolvedValue({
    data: {
      success: true,
      data: { eligible_count: 3, active_count: 0, max_id: 19 },
    },
  })
})

afterEach(async () => {
  for (const client of clients.splice(0)) client.clear()
  useAuthStore.getState().auth.setUser(null)
  await i18next.changeLanguage('en')
  i18next.removeResourceBundle('fr', 'translation')
})

describe('prompt audit records', () => {
  test('keeps the header free of controls the filter bar already provides', async () => {
    renderRecords()
    await screen.findByRole('checkbox', { name: 'Select audit record' })

    // Refreshing and clearing belong to the search controls, and a bulk delete
    // that ignores the row selection is what the selection exists to avoid.
    expect(
      screen.queryByRole('button', { name: 'Refresh' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Delete filtered' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /Delete selected/ })
    ).not.toBeInTheDocument()
  })

  test('asks the server again when the same filters are searched', async () => {
    const user = userEvent.setup()
    renderRecords()
    await screen.findByRole('checkbox', { name: 'Select audit record' })
    const listingsBefore = listingRequests().length
    const statsBefore = statsRequests().length

    // Searching is also how an operator asks for the newest rows, and unchanged
    // filters produce the same query key: the listing has to reach the server
    // instead of answering from what it already held.
    await user.click(screen.getByRole('button', { name: 'Search' }))

    await waitFor(() =>
      expect(listingRequests().length).toBeGreaterThan(listingsBefore)
    )
    expect(statsRequests().length).toBeGreaterThan(statsBefore)
  })

  test('offers the batch delete only once rows are selected', async () => {
    const user = userEvent.setup()
    renderRecords()

    await user.click(
      await screen.findByRole('checkbox', { name: 'Select audit record' })
    )

    expect(
      screen.getByRole('button', { name: 'Delete selected (1)' })
    ).toBeVisible()
  })

  test('deletes every request a selected merged row stands for', async () => {
    const user = userEvent.setup()
    renderRecords()

    await deleteSelectedRow(user)

    await waitFor(() => expect(apiMock.post).toHaveBeenCalledOnce())
    const [url, body] = apiMock.post.mock.calls[0]
    expect(url).toBe('/api/prompt-audit/events/delete-preview')
    // The row stands for three requests; its id alone would delete only the
    // first of them. The group is bounded by the filters the listing used.
    expect(body.filter).toMatchObject({
      group_ids: [17],
      start_time: expect.any(Number),
      end_time: expect.any(Number),
    })
    expect(body.filter.ids).toBeUndefined()
  })

  test('deletes the selected requests by id while the listing is split', async () => {
    window.localStorage.setItem('prompt-audit:collapse-repeats', 'false')
    server.pages = [[EVENT]]
    const user = userEvent.setup()
    renderRecords()

    await deleteSelectedRow(user)

    await waitFor(() => expect(apiMock.post).toHaveBeenCalledOnce())
    expect(apiMock.post.mock.calls[0][1]).toEqual({ filter: { ids: [17] } })
  })

  test('closes a deleted group without asking the server for it again', async () => {
    apiMock.delete.mockImplementation(async () => {
      server.pages = [[]]
      server.total = 0
      server.groups = {}
      return { data: { success: true, data: { deleted_count: 3 } } }
    })
    const user = userEvent.setup()
    renderRecords()
    await expandGroup(user)

    await deleteSelectedRow(user)
    const confirm = await screen.findByRole('button', {
      name: 'Delete permanently',
    })
    await waitFor(() => expect(confirm).toBeEnabled())
    await user.click(confirm)

    expect(
      await screen.findByText('No prompt audit records found')
    ).toBeVisible()
    expect(groupRequests(17)).toHaveLength(1)
    expect(toast.error).not.toHaveBeenCalled()
  })

  test('stops fetching an expanded group once its page is left', async () => {
    server.pages = [
      [MERGED],
      [{ ...MERGED, id: 50, redacted_preview: 'next-page-request' }],
    ]
    server.total = 40
    const user = userEvent.setup()
    const renderResult = renderRecords()
    await expandGroup(user)

    await user.click(screen.getByRole('button', { name: 'Go to next page' }))
    expect(await screen.findByText('next-page-request')).toBeVisible()
    // The screen has no refresh control of its own; a reload reaches the mounted
    // queries the same way the rest of the app triggers one.
    await act(async () => {
      await renderResult.client.invalidateQueries({
        queryKey: ['prompt-audit'],
      })
    })

    await waitFor(() =>
      expect(listingRequests((params) => params.page === 2)).toHaveLength(2)
    )
    expect(groupRequests(17)).toHaveLength(1)
  })

  test('closes the expanded groups when the page changes', async () => {
    server.pages = [
      [MERGED],
      [{ ...MERGED, id: 50, redacted_preview: 'next-page-request' }],
    ]
    server.total = 40
    const user = userEvent.setup()
    renderRecords()
    await expandGroup(user)

    await user.click(screen.getByRole('button', { name: 'Go to next page' }))
    expect(await screen.findByText('next-page-request')).toBeVisible()
    await user.click(
      screen.getByRole('button', { name: 'Go to previous page' })
    )

    expect(await screen.findByText('redacted-preview')).toBeVisible()
    expect(
      within(screen.getByRole('table')).getByRole('button', { name: 'Expand' })
    ).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('second-request')).not.toBeInTheDocument()
  })

  test('keeps the statistics while the listing is merged or split', async () => {
    const user = userEvent.setup()
    renderRecords()
    expect(await screen.findByText('redacted-preview')).toBeVisible()
    expect(statsRequests()).toHaveLength(1)

    await user.click(
      screen.getByRole('button', { name: 'Collapse repeated audits' })
    )

    await waitFor(() =>
      expect(
        listingRequests((params) => params.collapse_repeats === undefined)
      ).toHaveLength(1)
    )
    expect(statsRequests()).toHaveLength(1)
  })

  test('filters by the stored protocol value and by detector', async () => {
    server.pages = [[EVENT]]
    const user = userEvent.setup()
    renderRecords()
    expect(await screen.findByText('redacted-preview')).toBeVisible()

    // The screen names a protocol, while the server matches the value it
    // stored, so the filter picks from the names and sends the value.
    await user.click(screen.getByRole('button', { name: 'Expand' }))
    await user.click(screen.getByRole('combobox', { name: 'Protocol' }))
    await user.click(
      await screen.findByRole('option', {
        name: 'OpenAI Responses Compaction',
      })
    )
    await user.click(screen.getByRole('combobox', { name: 'Detector' }))
    await user.click(await screen.findByRole('option', { name: 'Model audit' }))
    await user.click(screen.getByRole('button', { name: 'Search' }))

    await waitFor(() =>
      expect(
        listingRequests(
          (params) =>
            params.protocol === 'openai_responses_compaction' &&
            params.detector === 'model'
        )
      ).toHaveLength(1)
    )
    expect(statsRequests().at(-1)?.[1]).toEqual({
      params: expect.objectContaining({
        protocol: 'openai_responses_compaction',
        detector: 'model',
      }),
    })
  })

  test('shows the statistics in the interface language', async () => {
    server.decisions = { block: 1234 }
    renderRecords()
    expect(await screen.findByText('1,234')).toBeVisible()

    i18next.addResourceBundle('fr', 'translation', { Blocked: 'Bloqués' })
    await act(async () => {
      await i18next.changeLanguage('fr')
    })

    expect(
      await screen.findByText('1\u202f234', {
        normalizer: getDefaultNormalizer({ collapseWhitespace: false }),
      })
    ).toBeVisible()
  })
})
