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

import { ChannelBatchEditDialog } from '../dialogs/channel-batch-edit-dialog'

const {
  batchUpdateChannelsMock,
  getAllModelsMock,
  getGroupsMock,
  previewChannelBatchMock,
} = vi.hoisted(() => ({
  batchUpdateChannelsMock: vi.fn(),
  getAllModelsMock: vi.fn(),
  getGroupsMock: vi.fn(),
  previewChannelBatchMock: vi.fn(),
}))

vi.mock('../../api', () => ({
  batchUpdateChannels: batchUpdateChannelsMock,
  getAllModels: getAllModelsMock,
  getGroups: getGroupsMock,
  previewChannelBatch: previewChannelBatchMock,
}))

vi.mock('@/components/multi-select', () => ({
  MultiSelect: (props: {
    id?: string
    options: Array<{ label: string; value: string }>
    allowCreate?: boolean
  }) => (
    <output
      data-testid={props.id}
      data-options={props.options.map((option) => option.value).join(',')}
      data-allow-create={String(props.allowCreate === true)}
    />
  ),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string | number>) =>
      Object.entries(values || {}).reduce(
        (result, [name, value]) => result.replace(`{{${name}}}`, String(value)),
        key
      ),
  }),
}))

vi.mock('sonner', () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
    warning: vi.fn(),
  },
}))

describe('channel batch edit dialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getGroupsMock.mockResolvedValue({
      success: true,
      data: ['default', 'premium'],
    })
    getAllModelsMock.mockResolvedValue({
      success: true,
      data: [{ id: 'gpt-4o' }, { id: 'claude-sonnet-4' }],
    })
  })

  test('previews and submits filtered channels without a row selection', async () => {
    const user = userEvent.setup()
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    previewChannelBatchMock.mockResolvedValue({
      success: true,
      data: { count: 3, fingerprint: 'filtered-fingerprint' },
    })
    batchUpdateChannelsMock.mockResolvedValue({
      success: true,
      data: { updated: 3 },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <ChannelBatchEditDialog
          open
          onOpenChange={vi.fn()}
          selectedIds={[]}
          filter={{ keyword: 'needle' }}
          onSuccess={vi.fn()}
        />
      </QueryClientProvider>
    )

    await waitFor(() => {
      expect(previewChannelBatchMock).toHaveBeenCalledWith({
        keyword: 'needle',
      })
    })
    expect(screen.getByRole('button', { name: 'Selected (0)' })).toBeDisabled()
    expect(
      await screen.findByText('3 channels match the current filters')
    ).toBeVisible()

    await user.click(screen.getByRole('checkbox', { name: 'Priority' }))
    const priorityInput = screen.getByRole('spinbutton')
    await user.clear(priorityInput)
    await user.type(priorityInput, '7')
    await user.click(screen.getByRole('button', { name: 'Apply Changes' }))

    await waitFor(() => {
      expect(batchUpdateChannelsMock).toHaveBeenCalledWith(
        {
          mode: 'filtered',
          filter: { keyword: 'needle' },
          fingerprint: 'filtered-fingerprint',
        },
        { priority: { value: 7 } }
      )
    })
  })

  test('does not submit when the filtered preview has no channels', async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    previewChannelBatchMock.mockResolvedValue({
      success: true,
      data: { count: 0, fingerprint: 'empty-fingerprint' },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <ChannelBatchEditDialog
          open
          onOpenChange={vi.fn()}
          selectedIds={[]}
          filter={{ keyword: 'missing' }}
          onSuccess={vi.fn()}
        />
      </QueryClientProvider>
    )

    expect(
      await screen.findByText('0 channels match the current filters')
    ).toBeVisible()
    expect(screen.getByRole('button', { name: 'Apply Changes' })).toBeDisabled()
    expect(batchUpdateChannelsMock).not.toHaveBeenCalled()
  })

  test('separates client policy and upstream detection without a fixed timezone label', () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <ChannelBatchEditDialog
          open
          onOpenChange={vi.fn()}
          selectedIds={[1]}
          filter={{}}
          onSuccess={vi.fn()}
        />
      </QueryClientProvider>
    )

    expect(screen.getAllByText('Client access policy').length).toBeGreaterThan(
      0
    )
    expect(
      screen.getAllByText('Upstream Model Detection Settings').length
    ).toBeGreaterThan(0)
    expect(screen.queryByText('Beijing time (UTC+8)')).toBeNull()
  })

  test('offers existing groups and searchable model suggestions', async () => {
    const user = userEvent.setup()
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <ChannelBatchEditDialog
          open
          onOpenChange={vi.fn()}
          selectedIds={[1, 2]}
          filter={{}}
          onSuccess={vi.fn()}
        />
      </QueryClientProvider>
    )

    await user.click(screen.getByRole('checkbox', { name: 'Group' }))
    await user.click(screen.getByRole('checkbox', { name: 'Models' }))

    await waitFor(() => {
      expect(screen.getByTestId('batch-group-values')).toHaveAttribute(
        'data-options',
        'default,premium'
      )
      expect(screen.getByTestId('batch-model-values')).toHaveAttribute(
        'data-options',
        'gpt-4o,claude-sonnet-4'
      )
    })
    expect(screen.getByTestId('batch-group-values')).toHaveAttribute(
      'data-allow-create',
      'false'
    )
    expect(screen.getByTestId('batch-model-values')).toHaveAttribute(
      'data-allow-create',
      'true'
    )
  })
})
