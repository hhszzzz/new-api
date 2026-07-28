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
import type { Row, Table } from '@tanstack/react-table'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { DataTableBulkActions } from '@/components/data-table'

import { DataTableBulkActions as ChannelBulkActions } from '../data-table-bulk-actions'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('../dialogs/channel-batch-edit-dialog', () => ({
  ChannelBatchEditDialog: (props: { open: boolean; selectedIds: number[] }) =>
    props.open ? (
      <output data-testid='selected-channel-ids'>
        {props.selectedIds.join(',')}
      </output>
    ) : null,
}))

vi.mock('../dialogs/channel-batch-merge-dialog', () => ({
  ChannelBatchMergeDialog: (props: {
    open: boolean
    selectedChannels: Array<{ id: number }>
  }) =>
    props.open ? (
      <output data-testid='merged-channel-ids'>
        {props.selectedChannels.map((channel) => channel.id).join(',')}
      </output>
    ) : null,
}))

vi.mock('../dialogs/channel-batch-detach-dialog', () => ({
  ChannelBatchDetachDialog: (props: {
    open: boolean
    selectedChannels: Array<{ id: number }>
  }) =>
    props.open ? (
      <output data-testid='detached-channel-ids'>
        {props.selectedChannels.map((channel) => channel.id).join(',')}
      </output>
    ) : null,
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

type TestRow = {
  id: number
}

describe('channel nested row selection', () => {
  test('shows bulk actions for a selected concrete child row', () => {
    const selectedChild = {
      getIsSelected: () => true,
      original: { id: 7 },
    } as Row<TestRow>
    const structuralParent = {
      getIsSelected: () => false,
      original: { id: -1 },
    } as Row<TestRow>
    const table = {
      getFilteredSelectedRowModel: () => ({
        rows: [],
        flatRows: [structuralParent, selectedChild],
      }),
      resetRowSelection: vi.fn(),
    } as unknown as Table<TestRow>

    render(
      <DataTableBulkActions table={table} entityName='channel'>
        <button type='button'>Edit selected channels</button>
      </DataTableBulkActions>
    )

    expect(
      screen.getByRole('toolbar', {
        name: 'Bulk actions for 1 selected channel',
      })
    ).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Edit selected channels' })
    ).toBeVisible()
  })

  test('passes only selected concrete child IDs to channel batch edit', async () => {
    const user = userEvent.setup()
    const selectedChild = {
      getIsSelected: () => true,
      original: { id: 7 },
    } as Row<TestRow>
    const structuralParent = {
      getIsSelected: () => false,
      original: { id: -1 },
    } as Row<TestRow>
    const table = {
      getFilteredSelectedRowModel: () => ({
        rows: [],
        flatRows: [structuralParent, selectedChild],
      }),
      resetRowSelection: vi.fn(),
    } as unknown as Table<TestRow>
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <ChannelBulkActions table={table} filter={{}} />
      </QueryClientProvider>
    )

    await user.click(
      screen.getByRole('button', {
        name: 'Edit selected or filtered channels',
      })
    )

    expect(screen.getByTestId('selected-channel-ids')).toHaveTextContent('7')
  })

  test('opens the bottom-toolbar merge dialog for selected concrete channels', async () => {
    const user = userEvent.setup()
    const selectedChild = {
      getIsSelected: () => true,
      original: { id: 7 },
    } as Row<TestRow>
    const structuralParent = {
      getIsSelected: () => false,
      original: { id: -1 },
    } as Row<TestRow>
    const table = {
      getFilteredSelectedRowModel: () => ({
        rows: [],
        flatRows: [structuralParent, selectedChild],
      }),
      resetRowSelection: vi.fn(),
    } as unknown as Table<TestRow>
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <ChannelBulkActions table={table} filter={{}} />
      </QueryClientProvider>
    )

    await user.click(
      screen.getByRole('button', { name: 'Merge selected channels' })
    )

    expect(screen.getByTestId('merged-channel-ids')).toHaveTextContent('7')
  })

  test('opens the detach dialog with only selected aggregated channels', async () => {
    const user = userEvent.setup()
    const aggregatedChild = {
      getIsSelected: () => true,
      original: { id: 7, name: 'Aggregated channel', aggregate_id: 3 },
    } as unknown as Row<TestRow>
    const standaloneChild = {
      getIsSelected: () => true,
      original: { id: 8, name: 'Standalone channel', aggregate_id: null },
    } as unknown as Row<TestRow>
    const table = {
      getFilteredSelectedRowModel: () => ({
        rows: [],
        flatRows: [aggregatedChild, standaloneChild],
      }),
      resetRowSelection: vi.fn(),
    } as unknown as Table<TestRow>
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <ChannelBulkActions table={table} filter={{}} />
      </QueryClientProvider>
    )

    await user.click(
      screen.getByRole('button', { name: 'Detach selected channels' })
    )

    expect(screen.getByTestId('detached-channel-ids')).toHaveTextContent('7')
  })
})
