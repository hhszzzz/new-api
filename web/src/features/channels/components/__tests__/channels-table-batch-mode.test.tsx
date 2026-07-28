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
import type { Table } from '@tanstack/react-table'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type { Channel } from '../../types'
import { ChannelsTable } from '../channels-table'

const { getChannelsMock } = vi.hoisted(() => ({
  getChannelsMock: vi.fn(),
}))

vi.mock('@tanstack/react-router', () => ({
  getRouteApi: () => ({
    useNavigate: () => vi.fn(),
    useSearch: () => ({}),
  }),
}))

vi.mock('@/hooks', () => ({
  useMediaQuery: () => false,
}))

vi.mock('@/hooks/use-table-url-state', () => ({
  useTableUrlState: () => ({
    globalFilter: '',
    onGlobalFilterChange: vi.fn(),
    columnFilters: [],
    onColumnFiltersChange: vi.fn(),
    pagination: { pageIndex: 0, pageSize: 20 },
    onPaginationChange: vi.fn(),
    ensurePageInRange: vi.fn(),
  }),
}))

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: () => null,
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en', resolvedLanguage: 'en' },
  }),
}))

vi.mock('../../api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api')>()
  return {
    ...actual,
    getChannels: getChannelsMock,
    getGroups: vi.fn().mockResolvedValue({ success: true, data: [] }),
    searchChannels: getChannelsMock,
  }
})

vi.mock('../channels-provider', () => ({
  useChannels: () => ({
    batchMode: true,
    enableTagMode: false,
    idSort: false,
    sensitiveVisible: true,
    setSensitiveVisible: vi.fn(),
    setCurrentRow: vi.fn(),
    upstream: { openModal: vi.fn() },
  }),
}))

vi.mock('../dialogs/channel-batch-edit-dialog', () => ({
  ChannelBatchEditDialog: () => null,
}))

vi.mock('@/components/data-table', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('@/components/data-table')>()
  const reactTable = await import('@tanstack/react-table')

  return {
    ...actual,
    useDebouncedColumnFilter: () => ({
      value: '',
      inputValue: '',
      onChange: vi.fn(),
      onCompositionStart: vi.fn(),
      onCompositionEnd: vi.fn(),
      resetInput: vi.fn(),
    }),
    usePersistedTableSorting: () => [[], vi.fn()],
    DataTablePage: (props: {
      table: Pick<Table<Channel>, 'getRowModel' | 'getVisibleLeafColumns'>
      bulkActions?: ReactNode
    }) => (
      <>
        <output data-testid='column-order'>
          {props.table
            .getVisibleLeafColumns()
            .map((column) => column.id)
            .join(',')}
        </output>
        {props.table.getRowModel().rows.map((row) => {
          const selectCell = row
            .getVisibleCells()
            .find((cell) => cell.column.id === 'select')
          const nameCell = row
            .getVisibleCells()
            .find((cell) => cell.column.id === 'name')
          return (
            <div key={row.id}>
              {selectCell?.column.columnDef.cell
                ? reactTable.flexRender(
                    selectCell.column.columnDef.cell,
                    selectCell.getContext()
                  )
                : null}
              {nameCell?.column.columnDef.cell
                ? reactTable.flexRender(
                    nameCell.column.columnDef.cell,
                    nameCell.getContext()
                  )
                : null}
            </div>
          )
        })}
        {props.bulkActions}
      </>
    ),
  }
})

const aggregateChannel = (id: number): Channel =>
  ({
    id,
    type: 1,
    key: `key-${id}`,
    status: 1,
    name: `Channel ${id}`,
    aggregate_id: 9,
    aggregate_name: 'Shared aggregate',
    group: 'default',
    models: 'gpt-test',
    schedule: {
      timezone: 'Asia/Shanghai',
      starts_at: null,
      expires_at: null,
      paused_until: null,
      weekly_enabled: false,
      weekly_windows: {},
    },
  }) as Channel

describe('channels table batch mode', () => {
  beforeEach(() => {
    window.localStorage.clear()
    getChannelsMock.mockResolvedValue({
      success: true,
      data: {
        items: [aggregateChannel(1), aggregateChannel(2)],
        total: 1,
        type_counts: { 1: 2 },
      },
    })
  })

  test('keeps aggregates collapsed and selects every child from the parent checkbox', async () => {
    const user = userEvent.setup()
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <ChannelsTable />
      </QueryClientProvider>
    )

    const aggregateCheckbox = await screen.findByRole('checkbox', {
      name: 'Select all channels in this aggregate',
    })
    expect(screen.getByRole('button', { name: 'Expand' })).toBeVisible()
    expect(screen.queryByRole('checkbox', { name: 'Select row' })).toBeNull()

    await user.click(aggregateCheckbox)

    expect(
      screen.getByRole('toolbar', {
        name: 'Bulk actions for 2 selected channels',
      })
    ).toBeVisible()

    await user.click(screen.getByRole('button', { name: 'Expand' }))
    const childCheckboxes = screen.getAllByRole('checkbox', {
      name: 'Select row',
    })
    expect(childCheckboxes).toHaveLength(2)
    childCheckboxes.forEach((checkbox) => expect(checkbox).toBeChecked())
  })

  test('keeps the selection column before ID when the saved order predates batch selection', async () => {
    window.localStorage.setItem(
      'channels:admin:column-order',
      JSON.stringify(['id', 'name', 'status'])
    )
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <ChannelsTable />
      </QueryClientProvider>
    )

    await waitFor(() => {
      expect(screen.getByTestId('column-order')).toHaveTextContent(
        /^select,id,/
      )
    })
  })

  test('shows a distinct active style after the aggregate is expanded', async () => {
    const user = userEvent.setup()
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <ChannelsTable />
      </QueryClientProvider>
    )

    const aggregateName = await screen.findByText('Shared aggregate')
    const expandButton = screen.getByRole('button', { name: 'Expand' })

    expect(
      aggregateName.compareDocumentPosition(expandButton) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(expandButton).toHaveAttribute('data-state', 'closed')
    expect(expandButton).toHaveClass('border-border')

    await user.click(expandButton)
    const collapseButton = screen.getByRole('button', { name: 'Collapse' })
    expect(collapseButton).toHaveAttribute('data-state', 'open')
    expect(collapseButton).toHaveClass('bg-secondary')
    expect(screen.queryByText('2 child channels')).toBeNull()
    expect(screen.getAllByText('Shared aggregate')).toHaveLength(1)
  })
})
