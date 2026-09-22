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
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { defaultPromptScopePolicies } from '../scopes'
import { PromptAuditSettings } from '../settings'
import type { PromptAuditConfig } from '../types'

const apiMock = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
}))
vi.mock('@/lib/api', () => ({ api: apiMock }))

// The toaster is not mounted in this suite, so the toast calls are observable
// only through the module.
vi.mock('sonner', async (importOriginal) => {
  const actual = await importOriginal<typeof import('sonner')>()
  return { ...actual, toast: { error: vi.fn(), success: vi.fn() } }
})

const config: PromptAuditConfig = {
  scope_policies: defaultPromptScopePolicies(),
  word_filter_enabled: true,
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
      id: 'guard-a',
      name: 'Primary',
      base_url: 'https://guard-a.example.com/v1',
      model: 'guard-a-model',
      timeout_ms: 3000,
      input_limit: 4000,
      concurrency: 4,
      enabled: true,
      has_token: true,
      purpose: 'classify',
      directions: ['input', 'output'],
    },
    {
      id: 'guard-b',
      name: 'Secondary',
      base_url: 'https://guard-b.example.com/v1',
      model: 'guard-b-model',
      timeout_ms: 2000,
      input_limit: 8000,
      concurrency: 2,
      enabled: false,
      has_token: false,
      purpose: 'classify',
      directions: ['input'],
    },
  ],
  total_timeout_ms: 1000,
  chunk_overlap: 64,
  chunk_concurrency: 4,
  cache_ttl_seconds: 0,
  worker_count: 1,
  max_attempts: 3,
  retention_days: 30,
  global_concurrency: 2,
  endpoint_concurrency: 2,
  output_max_bytes: 8 * 1024 * 1024,
  output_memory_bytes: 1024 * 1024,
  config_version: 'before',
}

const categories = [
  {
    id: 'violent',
    label: 'Violent content',
    label_zh: '暴力内容',
    description: 'Requests for violent content.',
  },
  {
    id: 'pii',
    label: 'Personal data',
    label_zh: '个人数据',
    description: 'Requests for personal data.',
  },
]

const clients: QueryClient[] = []

function renderSettings() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  const root = createRootRoute({
    component: () => (
      <QueryClientProvider client={client}>
        <PromptAuditSettings />
      </QueryClientProvider>
    ),
  })
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({
      initialEntries: ['/prompt-audit/settings'],
    }),
  })
  const view = render(<RouterProvider router={router} />)
  return { ...view, queryClient: client }
}

const nodeDetail = (view: ReturnType<typeof render>, id: string) =>
  view.container.querySelector(`#${id}`)

beforeEach(() => {
  vi.clearAllMocks()
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
  apiMock.get.mockImplementation(async (url: string) => {
    const data: Record<string, unknown> = {
      '/api/prompt-audit/config': config,
      '/api/prompt-audit/categories': categories,
      '/api/prompt-audit/wordlists': [],
      '/api/group/': [],
    }
    if (!(url in data)) throw new Error(`Unexpected API request: ${url}`)
    return { data: { success: true, data: data[url] } }
  })
  apiMock.put.mockResolvedValue({ data: { success: true } })
  apiMock.post.mockResolvedValue({ data: { success: true } })
})
afterEach(() => {
  for (const client of clients.splice(0)) client.clear()
  useAuthStore.getState().auth.setUser(null)
})

describe('prompt audit settings page', () => {
  test('renders four sections in order with the node list and advanced limits collapsed', async () => {
    const view = renderSettings()

    const enforcement = await screen.findByText('Enforcement policy')
    const models = screen.getByText('Audit models')
    const scopes = await screen.findByText('Inspection rules by source')
    const advanced = screen.getByText('Advanced parameters')

    for (const [earlier, later] of [
      [enforcement, models],
      [models, scopes],
      [scopes, advanced],
    ]) {
      expect(
        earlier.compareDocumentPosition(later) &
          Node.DOCUMENT_POSITION_FOLLOWING
      ).toBeTruthy()
    }

    // Section ④ is collapsed: none of its eleven numeric fields are mounted.
    expect(nodeDetail(view, 'prompt-audit-total-timeout')).toBeNull()
    // Section ② keeps only the summary row of the non-first node.
    expect(nodeDetail(view, 'prompt-audit-node-model-0')).not.toBeNull()
    expect(nodeDetail(view, 'prompt-audit-node-model-1')).toBeNull()
    expect(screen.getAllByRole('button', { name: /guard-/ })).toHaveLength(2)
  })

  test('expanding one node collapses the other', async () => {
    const user = userEvent.setup()
    const view = renderSettings()
    await screen.findByText('Enforcement policy')

    await user.click(screen.getByRole('button', { name: /guard-b/ }))

    expect(nodeDetail(view, 'prompt-audit-node-model-1')).not.toBeNull()
    expect(nodeDetail(view, 'prompt-audit-node-model-0')).toBeNull()

    await user.click(screen.getByRole('button', { name: /guard-a/ }))

    expect(nodeDetail(view, 'prompt-audit-node-model-0')).not.toBeNull()
    expect(nodeDetail(view, 'prompt-audit-node-model-1')).toBeNull()
  })

  test('expands the advanced limits into three labelled groups of numeric fields', async () => {
    const user = userEvent.setup()
    const view = renderSettings()
    await screen.findByText('Enforcement policy')

    await user.click(
      screen.getByRole('button', { name: 'Advanced parameters' })
    )

    for (const id of [
      'prompt-audit-total-timeout',
      'prompt-audit-overlap',
      'prompt-audit-chunk-concurrency',
      'prompt-audit-cache-ttl',
      'prompt-audit-output-memory',
      'prompt-audit-output-limit',
      'prompt-audit-retention',
      'prompt-audit-workers',
      'prompt-audit-attempts',
      'prompt-audit-global-concurrency',
      'prompt-audit-node-concurrency',
    ]) {
      expect(nodeDetail(view, id), id).not.toBeNull()
    }
    expect(screen.getByText('Request timeouts and cache')).toBeVisible()
    expect(screen.getByText('Output buffering and retention')).toBeVisible()
    expect(screen.getByText('Queue workers and concurrency')).toBeVisible()
  })

  test('row controls reorder and remove nodes without toggling the row', async () => {
    const user = userEvent.setup()
    const view = renderSettings()
    await screen.findByText('Enforcement policy')

    // The expanded row stays expanded when its own controls are used; the
    // expansion follows the node, so after the swap it is the second row that
    // shows its detail, not the first.
    await user.click(
      screen.getAllByRole('button', { name: 'Move audit model down' })[0]
    )
    expect(nodeDetail(view, 'prompt-audit-node-model-1')).not.toBeNull()
    expect(nodeDetail(view, 'prompt-audit-node-model-0')).toBeNull()
    expect(screen.getByRole('button', { name: /guard-b/ })).toBeInTheDocument()
    // Row order really changed: guard-b moved to the first row, so its own
    // down/up buttons are enabled/disabled and the moved row has the opposite.
    expect(
      screen.getAllByRole('button', { name: 'Move audit model down' })[0]
    ).toBeEnabled()
    expect(
      screen.getAllByRole('button', { name: 'Move audit model up' })[0]
    ).toBeDisabled()
    expect(
      screen.getAllByRole('button', { name: 'Move audit model down' })[1]
    ).toBeDisabled()
    expect(
      screen.getAllByRole('button', { name: 'Move audit model up' })[1]
    ).toBeEnabled()

    // Removing row 0 drops guard-b, so the moved row is the only one left and
    // it adopts index 0 with the expansion still following it.
    await user.click(
      screen.getAllByRole('button', { name: 'Remove audit model' })[0]
    )
    await waitFor(() =>
      expect(
        screen.getAllByRole('button', { name: 'Remove audit model' })
      ).toHaveLength(1)
    )
    expect(screen.getByRole('button', { name: /guard-a/ })).toBeInTheDocument()
    expect(nodeDetail(view, 'prompt-audit-node-model-0')).not.toBeNull()
  })

  test('adding a node opens it and keeps the write-only token hidden until toggled', async () => {
    const user = userEvent.setup()
    const view = renderSettings()
    await screen.findByText('Enforcement policy')

    expect(
      await screen.findByRole('button', { name: 'Test saved audit model' })
    ).toBeEnabled()
    expect(
      screen.getByRole('button', { name: 'Clear saved token' })
    ).toBeInTheDocument()

    const token = screen.getByLabelText('API token')
    expect(token).toHaveAttribute('type', 'password')
    // Every expanded node contributes one identically labelled toggle, so the
    // lookup has to accept a list.
    await user.click(
      screen.getAllByRole('button', { name: 'Toggle password visibility' })[0]
    )
    expect(screen.getByLabelText('API token')).toHaveAttribute('type', 'text')

    await user.click(screen.getByRole('button', { name: 'Add audit model' }))
    await waitFor(() =>
      expect(
        screen.getAllByRole('button', { name: 'Remove audit model' })
      ).toHaveLength(3)
    )
    expect(nodeDetail(view, 'prompt-audit-node-model-2')).not.toBeNull()
    expect(nodeDetail(view, 'prompt-audit-node-model-0')).toBeNull()
    expect(
      await screen.findByRole('button', { name: 'Save before testing' })
    ).toBeDisabled()
  })

  // The three multi-value pickers used to be hand-rolled checkbox grids; they
  // now share the chips picker used for wordlist assignment. Guard the round
  // trip, since a broken chip control silently drops audit directions.
  test('audit directions render as removable chips and toggle through the dropdown', async () => {
    const user = userEvent.setup()
    renderSettings()
    await screen.findByText('Enforcement policy')

    const chipLabels = () => {
      const chips = screen
        .getByLabelText('Audit directions')
        .closest('[data-slot="combobox-chips"]') as HTMLElement
      return [...chips.querySelectorAll('[data-slot="combobox-chip"]')].map(
        (chip) => chip.textContent
      )
    }

    // The first node is expanded by default and starts with both directions.
    expect(chipLabels()).toEqual(['Request input', 'Generated output'])

    // Picking an already-selected option deselects it.
    await user.click(screen.getByLabelText('Audit directions'))
    await user.click(screen.getByRole('option', { name: 'Request input' }))

    expect(chipLabels()).toEqual(['Generated output'])

    // Backspace on the empty input drops the remaining chip through the same
    // onValueChange path the chip's own remove button uses.
    await user.keyboard('{Backspace}')

    expect(chipLabels()).toEqual([])
    expect(screen.getByPlaceholderText('No directions selected')).toBeVisible()
  })

  // Every failure kind but two used to collapse into the same generic
  // sentence, so a rejected token, a redirecting base URL, and a model that
  // ignores assistant messages looked identical without the network tab.
  test('a failed model test reports the cause the backend measured', async () => {
    const user = userEvent.setup()
    renderSettings()
    await screen.findByText('Enforcement policy')
    apiMock.post.mockResolvedValue({
      data: {
        success: false,
        message: 'prompt audit node test failed',
        data: {
          endpoint_id: 'guard-a',
          purpose: 'classify',
          latency_ms: 251,
          safety: '',
          decision: 'pass',
          error_code: 'endpoint_http_401',
          direction: 'output',
        },
      },
    })

    await user.click(
      await screen.findByRole('button', { name: 'Test saved audit model' })
    )

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        'The audit model returned HTTP 401 during the Generated output check.'
      )
    )
  })
})
