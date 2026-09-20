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
  RouterContextProvider,
} from '@tanstack/react-router'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type { Channel } from '../../types'
import { ChannelMutateDrawer } from '../drawers/channel-mutate-drawer'

const { getChannelMock, translateMock } = vi.hoisted(() => ({
  getChannelMock: vi.fn(),
  translateMock: (key: string, values?: Record<string, string | number>) =>
    Object.entries(values || {}).reduce(
      (result, [name, value]) => result.replace(`{{${name}}}`, String(value)),
      key
    ),
}))

vi.mock('../../api', () => ({
  fetchModels: vi.fn().mockResolvedValue({ success: true, data: [] }),
  getAllModels: vi.fn().mockResolvedValue({ success: true, data: [] }),
  getChannel: getChannelMock,
  getChannelAggregates: vi.fn().mockResolvedValue({ success: true, data: [] }),
  getChannelOps: vi.fn().mockResolvedValue({ success: true, data: {} }),
  getGroups: vi.fn().mockResolvedValue({ success: true, data: ['default'] }),
  getPrefillGroups: vi.fn().mockResolvedValue({ success: true, data: [] }),
  getTaskPluginOptions: vi.fn().mockResolvedValue([]),
  refreshCodexCredential: vi.fn(),
}))

vi.mock('../channels-provider', () => ({
  useChannels: () => ({ setOpen: vi.fn() }),
}))

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ auth: { user: { role: 100 } } }),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: translateMock,
    i18n: { language: 'en', resolvedLanguage: 'en' },
  }),
}))

vi.mock('sonner', () => ({
  toast: {
    error: vi.fn(),
    info: vi.fn(),
    success: vi.fn(),
    warning: vi.fn(),
  },
}))

function channelWith(isMultiKey: boolean): Channel {
  return {
    id: 7,
    name: 'channel',
    type: 1,
    key: 'secret',
    status: 1,
    models: 'gpt-4o',
    group: 'default',
    setting: '',
    settings: '{}',
    channel_info: {
      is_multi_key: isMultiKey,
      multi_key_mode: 'random',
    },
  } as unknown as Channel
}

async function renderDrawer(isMultiKey: boolean) {
  getChannelMock.mockResolvedValue({
    success: true,
    data: channelWith(isMultiKey),
  })
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <RouterContextProvider
        router={createRouter({
          routeTree: createRootRoute(),
          history: createMemoryHistory({ initialEntries: ['/'] }),
        })}
      >
        <ChannelMutateDrawer
          open
          onOpenChange={vi.fn()}
          currentRow={{ id: 7 } as Channel}
        />
      </RouterContextProvider>
    </QueryClientProvider>
  )
  fireEvent.click(await screen.findByRole('tab', { name: /Routing & Mapping/ }))
  return screen.findByRole('switch', { name: 'Disable Model On Error' })
}

describe('channel drawer disable model on error', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  test('enables the toggle for a single-key channel', async () => {
    const toggle = await renderDrawer(false)

    await waitFor(() =>
      expect(toggle).not.toHaveAttribute('aria-disabled', 'true')
    )
    expect(
      screen.queryByText(
        'Automatic model-level disabling is unavailable for multi-key channels. Use Manage disabled models to disable a model manually.'
      )
    ).toBeNull()
  })

  test('disables the toggle and explains why for a multi-key channel', async () => {
    const toggle = await renderDrawer(true)

    // Base UI's Switch exposes the disabled state through aria-disabled.
    await waitFor(() => expect(toggle).toHaveAttribute('aria-disabled', 'true'))
    expect(
      screen.getByText(
        'Automatic model-level disabling is unavailable for multi-key channels. Use Manage disabled models to disable a model manually.'
      )
    ).toBeVisible()
  })
})
