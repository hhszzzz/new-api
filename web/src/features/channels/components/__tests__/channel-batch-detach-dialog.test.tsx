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

import { ChannelBatchDetachDialog } from '../dialogs/channel-batch-detach-dialog'

const { detachChannelsFromAggregatesMock } = vi.hoisted(() => ({
  detachChannelsFromAggregatesMock: vi.fn(),
}))

vi.mock('../../api', () => ({
  detachChannelsFromAggregates: detachChannelsFromAggregatesMock,
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

describe('channel batch detach dialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    detachChannelsFromAggregatesMock.mockResolvedValue({
      success: true,
      data: { updated: 2 },
    })
  })

  test('detaches the selected channels in one request', async () => {
    const user = userEvent.setup()
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
        <ChannelBatchDetachDialog
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
    const selectedNames = new Intl.ListFormat('zh-CN', {
      style: 'short',
      type: 'conjunction',
    }).format(['Channel 7', 'Channel 9'])

    expect(
      screen.getByText(`Selected ${selectedNames}, 2 channels total`)
    ).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Detach' }))

    await waitFor(() => {
      expect(detachChannelsFromAggregatesMock).toHaveBeenCalledWith({
        ids: [7, 9],
      })
    })
    expect(onSuccess).toHaveBeenCalledOnce()
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })
})
