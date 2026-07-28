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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { ChannelBatchMergeDialog } from '../dialogs/channel-batch-merge-dialog'

const { getChannelAggregatesMock, mergeChannelsIntoAggregateMock } = vi.hoisted(
  () => ({
    getChannelAggregatesMock: vi.fn(),
    mergeChannelsIntoAggregateMock: vi.fn(),
  })
)

vi.mock('../../api', () => ({
  getChannelAggregates: getChannelAggregatesMock,
  mergeChannelsIntoAggregate: mergeChannelsIntoAggregateMock,
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string | number>) =>
      Object.entries(values || {}).reduce(
        (result, [name, value]) => result.replace(`{{${name}}}`, String(value)),
        key
      ),
    i18n: { language: 'zhCN', resolvedLanguage: 'zhCN' },
  }),
}))

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

vi.mock('@/lib/admin-permissions', () => ({
  ADMIN_PERMISSION_ACTIONS: { SENSITIVE_WRITE: 'sensitive_write' },
  ADMIN_PERMISSION_RESOURCES: { CHANNEL: 'channel' },
  hasPermission: () => true,
}))

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (state: { auth: { user: object } }) => unknown) =>
    selector({ auth: { user: {} } }),
}))

function renderDialog() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  const onOpenChange = vi.fn()
  const onSuccess = vi.fn()

  render(
    <QueryClientProvider client={queryClient}>
      <ChannelBatchMergeDialog
        open
        onOpenChange={onOpenChange}
        selectedChannels={[
          { id: 9, name: 'Channel 9' },
          { id: 7, name: 'Channel 7' },
        ]}
        onSuccess={onSuccess}
      />
    </QueryClientProvider>
  )

  return { onOpenChange, onSuccess }
}

describe('channel batch merge dialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    })
    getChannelAggregatesMock.mockResolvedValue({
      success: true,
      data: [
        {
          id: 3,
          name: 'Existing parent',
          base_url: 'https://existing.example.com',
          remark: '',
          created_at: 1,
          updated_at: 1,
          child_count: 2,
        },
      ],
    })
    mergeChannelsIntoAggregateMock.mockResolvedValue({
      success: true,
      data: {
        aggregate: {
          id: 4,
          name: 'New parent',
          base_url: 'https://new.example.com',
          remark: '',
          created_at: 1,
          updated_at: 1,
          child_count: 2,
        },
        updated: 2,
      },
    })
  })

  test('creates a new aggregate and merges the selected channels atomically', async () => {
    const user = userEvent.setup()
    const callbacks = renderDialog()
    const selectedNames = new Intl.ListFormat('zh-CN', {
      style: 'short',
      type: 'conjunction',
    }).format(['Channel 7', 'Channel 9'])

    expect(
      screen.getByText(`Selected ${selectedNames}, 2 channels total`)
    ).toBeVisible()
    expect(
      screen.queryByText(
        'Channels already assigned to another aggregate will be moved to the selected target.'
      )
    ).toBeNull()

    await user.type(screen.getByLabelText('Aggregate name'), 'New parent')
    await user.type(
      screen.getByLabelText('Shared base URL (optional)'),
      'https://new.example.com'
    )
    await user.click(
      screen.getByRole('switch', { name: 'Inherit aggregate base URL' })
    )
    await user.click(screen.getByRole('button', { name: 'Merge channels' }))

    await waitFor(() => {
      expect(mergeChannelsIntoAggregateMock).toHaveBeenCalledWith({
        ids: [7, 9],
        new_aggregate: {
          name: 'New parent',
          base_url: 'https://new.example.com',
          remark: '',
        },
        inherit_aggregate_base_url: true,
      })
    })
    expect(callbacks.onSuccess).toHaveBeenCalledOnce()
    expect(callbacks.onOpenChange).toHaveBeenCalledWith(false)
  })

  test('moves selected channels into the chosen existing aggregate', async () => {
    const user = userEvent.setup()
    renderDialog()

    const existingTarget = screen.getByRole('radio', {
      name: /Existing aggregate/,
    })
    await waitFor(() => expect(existingTarget).toBeEnabled())
    await user.click(existingTarget)
    await user.click(screen.getByRole('button', { name: 'Merge channels' }))

    await waitFor(() => {
      expect(mergeChannelsIntoAggregateMock).toHaveBeenCalledWith({
        ids: [7, 9],
        aggregate_id: 3,
        inherit_aggregate_base_url: false,
      })
    })
  })
})
