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
import type { TFunction } from 'i18next'
import { useState, type ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { PromptAuditNavigation } from '../components/prompt-audit-navigation'
import { ScopePoliciesSection } from '../components/scope-policies-section'
import { WordlistImportDialog } from '../components/wordlist-import-dialog'
import { WordlistTestCard } from '../components/wordlist-test-card'
import { defaultPromptScopePolicies, promptWordlistError } from '../scopes'
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
    action: 'block',
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
    action: 'review',
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
  output_mode: 'off',
  blocking_latest_turn_only: true,
  manual_wordlist_action: 'block',
  enabled_categories: [],
  controversial_block_categories: [],
  review_enabled: false,
  review_prompt: '',
  all_groups: true,
  groups: [],
  endpoints: [],
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
  test('uses one settings destination for prompt audit configuration', async () => {
    renderManagement(<PromptAuditNavigation />)

    expect(
      await screen.findByRole('tab', { name: 'Audit records' })
    ).toBeVisible()
    expect(screen.getByRole('tab', { name: 'Settings' })).toBeVisible()
    expect(screen.getByRole('tab', { name: 'Wordlists' })).toBeVisible()
    expect(
      screen.queryByRole('tab', { name: 'Inspection rules' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('tab', { name: 'Audit nodes' })
    ).not.toBeInTheDocument()
  })

  test('changing one source preserves the other sources and their model audit', async () => {
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
    // Model review is one of the choices the source control offers, not a
    // control of its own, so it is switched off by dropping it from the list —
    // and it never reaches the wordlist ids that are saved.
    await user.click(
      await screen.findByRole('combobox', { name: 'User messages' })
    )
    await user.click(await screen.findByRole('option', { name: 'Model audit' }))
    expect(onChange.mock.lastCall?.[0].user).toEqual({
      library_ids: ['1', '2', '3'],
      model_audit: false,
    })
    expect(onChange.mock.lastCall?.[0].system).toEqual(initial.system)
    // An open dropdown leaves the rest of the form out of the accessibility
    // tree, so it is closed before the next source is picked.
    await user.keyboard('{Escape}')
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

  test('turns model review on from the source control itself', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    const initial = defaultPromptScopePolicies()
    // This source is inspected by model review alone, and it is switched off.
    initial.system = { library_ids: [], model_audit: false }
    function Policies() {
      const [policies, setPolicies] = useState<PromptScopePolicies>(initial)
      return (
        <ScopePoliciesSection
          policies={policies}
          libraries={libraries}
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

    const control = await screen.findByRole('combobox', {
      name: 'System instructions',
    })
    // Model review reads as one of the choices of that control, not as a
    // control of its own beside it.
    expect(
      screen.queryByRole('switch', { name: /Model audit/ })
    ).not.toBeInTheDocument()
    expect(screen.getByPlaceholderText('No inspection selected')).toBeVisible()

    await user.click(control)
    await user.click(screen.getByRole('option', { name: 'Model audit' }))

    expect(onChange.mock.lastCall?.[0].system).toEqual({
      library_ids: [],
      model_audit: true,
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
      action: 'review',
    })
    expect(
      screen.getByText(
        'Import limits: each file up to 10 MiB; all downloaded source data up to 30 MiB; up to 100 files and 500,000 unique words.'
      )
    ).toBeVisible()
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

  test('editing a remote wordlist saves its name, source, and assignments', async () => {
    const user = userEvent.setup()
    renderManagement(<PromptWordlists />)

    await user.click(
      await screen.findByRole('button', { name: 'Edit: Library One' })
    )
    const name = await screen.findByRole('textbox', { name: 'Name' })
    const source = screen.getByRole('textbox', { name: 'Source URL' })
    expect(name).toHaveValue('Library One')
    expect(source).toHaveValue('https://example.com/words.txt')

    await user.clear(name)
    await user.type(name, 'Updated library')
    await user.clear(source)
    await user.type(source, 'https://example.com/replacement.txt')
    await user.click(screen.getByRole('combobox', { name: 'Apply to' }))
    await user.click(
      await screen.findByRole('option', { name: 'System instructions' })
    )
    await user.click(
      await screen.findByRole('option', { name: 'Task and standalone input' })
    )
    await user.keyboard('{Escape}')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() =>
      expect(apiMock.put).toHaveBeenCalledWith(
        '/api/prompt-audit/wordlists/1',
        {
          name: 'Updated library',
          source_url: 'https://example.com/replacement.txt',
          scopes: ['user', 'task'],
          auto_update: true,
          action: 'review',
        }
      )
    )
  })

  test('wordlist limit failures explain the exact exceeded limit', () => {
    const translate = ((key: string) => key) as TFunction
    expect(promptWordlistError('file_too_large', translate)).toBe(
      'Each wordlist file must not exceed 10 MiB.'
    )
    expect(promptWordlistError('source_too_large', translate)).toBe(
      'All data downloaded from one source must not exceed 30 MiB.'
    )
    expect(promptWordlistError('too_many_words', translate)).toBe(
      'A wordlist can contain at most 500,000 unique entries.'
    )
    expect(promptWordlistError('too_many_files', translate)).toBe(
      'A GitHub source can contain at most 100 supported wordlist files.'
    )
    expect(promptWordlistError('github_tree_too_large', translate)).toBe(
      'The GitHub directory is too large to scan. Select a smaller directory or a single file.'
    )
  })

  test('changing test text or source clears the previous match', async () => {
    const user = userEvent.setup()
    apiMock.post.mockResolvedValue({
      data: {
        success: true,
        data: {
          wordlist: {
            id: '1',
            name: 'Library One',
            version: 'v1',
            scope: 'user',
            action: 'review',
          },
          decision: 'pass',
          safety: 'Safe',
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
