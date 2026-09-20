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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState, type ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { ScopePoliciesSection } from '../components/scope-policies-section'
import { WordlistImportDialog } from '../components/wordlist-import-dialog'
import { WordlistTestCard } from '../components/wordlist-test-card'
import { defaultPromptScopePolicies } from '../scopes'
import { PromptAuditSettings } from '../settings'
import type {
  PromptAuditConfig,
  PromptScopePolicies,
  PromptWordlist,
} from '../types'
import { PromptWordlists } from '../wordlists'

const apiMock = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
}))
vi.mock('@/lib/api', () => ({ api: apiMock }))

const libraries: PromptWordlist[] = [
  {
    id: 'manual',
    name: 'Custom wordlist',
    source_url: '',
    enabled: true,
    auto_update: false,
    status: 'ready',
    word_count: 1,
    file_count: 0,
    content_hash: '',
    source_revision: '',
    last_success_at: 0,
    next_sync_at: 0,
    last_error: '',
    scopes: ['user', 'task'],
  },
  {
    id: '1',
    name: 'Library One',
    source_url: 'https://example.com/words.txt',
    enabled: true,
    auto_update: true,
    status: 'ready',
    word_count: 2,
    file_count: 1,
    content_hash: 'v1',
    source_revision: '',
    last_success_at: 0,
    next_sync_at: 0,
    last_error: '',
    scopes: ['system', 'user'],
  },
]
const config: PromptAuditConfig = {
  scope_policies: defaultPromptScopePolicies(),
  word_filter_enabled: true,
  mode: 'off',
  enabled_categories: [],
  all_groups: true,
  groups: [],
  endpoints: [],
  total_timeout_ms: 1000,
  chunk_overlap: 64,
  cache_ttl_seconds: 0,
  worker_count: 1,
  max_attempts: 3,
  retention_days: 30,
  global_concurrency: 2,
  endpoint_concurrency: 2,
  config_version: 'before',
}
const clients: QueryClient[] = []

function renderManagement(content: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  const root = createRootRoute({
    component: () => (
      <QueryClientProvider client={client}>{content}</QueryClientProvider>
    ),
  })
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  return render(<RouterProvider router={router} />)
}

beforeEach(() => {
  vi.clearAllMocks()
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
  apiMock.get.mockImplementation(async (url: string) => {
    const data: Record<string, unknown> = {
      '/api/prompt-audit/wordlists': libraries,
      '/api/prompt-audit/config': config,
      '/api/prompt-audit/categories': [],
      '/api/group/': [],
      '/api/prompt-audit/wordlists/manual/content': {
        words: 'private-custom-marker',
      },
    }
    if (!(url in data)) throw new Error(`Unexpected API request: ${url}`)
    return { data: { success: true, data: data[url] } }
  })
  apiMock.put.mockResolvedValue({ data: { success: true } })
})
afterEach(() => {
  for (const client of clients.splice(0)) client.clear()
  useAuthStore.getState().auth.setUser(null)
})

describe('wordlist management', () => {
  test('changing one source preserves the other sources and their model switches', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    const options = [
      ...libraries,
      { ...libraries[1], id: '2', name: 'Library Two' },
      { ...libraries[1], id: '3', name: 'Library Three' },
    ]
    const initial = defaultPromptScopePolicies()
    initial.system = { library_ids: ['1', '2'], model_audit: false }
    initial.user = { library_ids: ['1', '2', '3'], model_audit: true }
    function Policies() {
      const [policies, setPolicies] = useState<PromptScopePolicies>(initial)
      return (
        <ScopePoliciesSection
          policies={policies}
          libraries={options}
          wordFilterEnabled
          onWordFilterChange={vi.fn()}
          onChange={(next) => {
            setPolicies(next)
            onChange(next)
          }}
        />
      )
    }
    renderManagement(<Policies />)
    await user.click(
      await screen.findByRole('switch', { name: 'Model audit: User messages' })
    )
    expect(onChange.mock.lastCall?.[0].user).toEqual({
      library_ids: ['1', '2', '3'],
      model_audit: false,
    })
    expect(onChange.mock.lastCall?.[0].system).toEqual(initial.system)
    await user.click(
      screen.getByRole('combobox', { name: 'System instructions' })
    )
    await user.click(
      await screen.findByRole('option', { name: 'Library Three' })
    )
    expect(onChange.mock.lastCall?.[0].system.library_ids).toEqual([
      '1',
      '2',
      '3',
    ])
    expect(onChange.mock.lastCall?.[0].user).toEqual({
      library_ids: ['1', '2', '3'],
      model_audit: false,
    })
  })

  test('an import defaults to user and task text with daily updates', async () => {
    const user = userEvent.setup()
    const onOpenChange = vi.fn()
    apiMock.post.mockResolvedValue({
      data: { success: true, data: { id: '2' } },
    })
    renderManagement(<WordlistImportDialog open onOpenChange={onOpenChange} />)
    await user.type(
      await screen.findByRole('textbox', { name: 'Name' }),
      'Imported terms'
    )
    await user.type(
      screen.getByRole('textbox', { name: 'Source URL' }),
      'https://example.com/words.txt'
    )
    await user.click(screen.getByRole('button', { name: 'Import wordlist' }))
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(apiMock.post).toHaveBeenCalledWith('/api/prompt-audit/wordlists', {
      name: 'Imported terms',
      source_url: 'https://example.com/words.txt',
      scopes: ['user', 'task'],
      auto_update: true,
    })
  })

  test('a rejected import keeps the draft open and displays the server error', async () => {
    const user = userEvent.setup()
    const onOpenChange = vi.fn()
    apiMock.post.mockResolvedValue({
      data: { success: false, message: 'wordlist already exists' },
    })
    renderManagement(<WordlistImportDialog open onOpenChange={onOpenChange} />)
    await user.type(
      await screen.findByRole('textbox', { name: 'Name' }),
      'Imported terms'
    )
    await user.type(
      screen.getByRole('textbox', { name: 'Source URL' }),
      'https://example.com/words.txt'
    )
    await user.click(screen.getByRole('button', { name: 'Import wordlist' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'wordlist already exists'
    )
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue(
      'Imported terms'
    )
    expect(onOpenChange).not.toHaveBeenCalled()
  })

  test('disabling a library sends only its switch and loads custom words only after Edit', async () => {
    const user = userEvent.setup()
    renderManagement(<PromptWordlists />)
    await user.click(
      await screen.findByRole('switch', { name: 'Enabled: Library One' })
    )
    await waitFor(() =>
      expect(apiMock.put).toHaveBeenCalledWith(
        '/api/prompt-audit/wordlists/1',
        { id: '1', enabled: false }
      )
    )
    expect(apiMock.get).not.toHaveBeenCalledWith(
      '/api/prompt-audit/wordlists/manual/content'
    )
    expect(screen.queryByText('private-custom-marker')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Edit' }))
    expect(
      await screen.findByRole('textbox', { name: 'Blocked keywords' })
    ).toHaveValue('private-custom-marker')
  })

  test('changing test text or source clears the previous match', async () => {
    const user = userEvent.setup()
    apiMock.post.mockResolvedValue({
      data: {
        success: true,
        data: {
          match: { id: '1', name: 'Library One', version: 'v1', scope: 'user' },
          model_audit: false,
        },
      },
    })
    renderManagement(<WordlistTestCard />)
    const text = await screen.findByRole('textbox', { name: 'Test text' })
    await user.type(text, 'marker')
    await user.click(screen.getByRole('button', { name: 'Test rules' }))
    expect(
      await screen.findByText('Matched wordlist: Library One')
    ).toBeVisible()
    await user.type(text, ' changed')
    expect(
      screen.queryByText('Matched wordlist: Library One')
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Test rules' }))
    expect(
      await screen.findByText('Matched wordlist: Library One')
    ).toBeVisible()
    await user.selectOptions(
      screen.getByRole('combobox', { name: 'Text source' }),
      'system'
    )
    expect(
      screen.queryByText('Matched wordlist: Library One')
    ).not.toBeInTheDocument()
  })

  test('the page import action opens the source form', async () => {
    const user = userEvent.setup()
    renderManagement(<PromptWordlists />)
    await user.click(
      await screen.findByRole('button', { name: 'Import wordlist' })
    )
    expect(
      await screen.findByRole('textbox', { name: 'Source URL' })
    ).toBeVisible()
  })

  test('deleting a library requires confirmation before the request', async () => {
    const user = userEvent.setup()
    apiMock.delete.mockResolvedValue({ data: { success: true } })
    renderManagement(<PromptWordlists />)
    await user.click(await screen.findByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('alertdialog')
    expect(apiMock.delete).not.toHaveBeenCalled()
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() =>
      expect(apiMock.delete).toHaveBeenCalledWith(
        '/api/prompt-audit/wordlists/1'
      )
    )
  })

  test('saving settings keeps the form usable while its config is refreshed', async () => {
    const user = userEvent.setup()
    apiMock.put.mockImplementation(async () => {
      apiMock.get.mockImplementation(() => new Promise(() => {}))
      return {
        data: { success: true, data: { ...config, config_version: 'after' } },
      }
    })
    renderManagement(<PromptAuditSettings />)
    const save = await screen.findByRole('button', { name: 'Save settings' })
    await waitFor(() => expect(save).toBeEnabled())
    await user.click(save)
    await waitFor(() => expect(apiMock.put).toHaveBeenCalled())
    expect(
      screen.queryByText('Prompt audit settings are unavailable')
    ).not.toBeInTheDocument()
    expect(
      await screen.findByRole('button', { name: 'Save settings' })
    ).toBeEnabled()
  })
})
