/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { ColumnDef } from '@tanstack/react-table'
import { act, renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, test } from 'vitest'

import { useDataTable } from '../use-data-table'

type RowData = { id: number; name: string; status: string }

const columns: ColumnDef<RowData>[] = [
  { accessorKey: 'id', header: 'ID', size: 80 },
  { accessorKey: 'name', header: 'Name', size: 160, minSize: 100 },
  { accessorKey: 'status', header: 'Status', size: 120 },
]

const data: RowData[] = [{ id: 1, name: 'Alpha', status: 'enabled' }]

describe('shared data table persisted view', () => {
  beforeEach(() => {
    window.localStorage.clear()
  })

  test('restores valid column state, sorting, and page size while dropping removed columns', () => {
    window.localStorage.setItem(
      'demo:column-visibility',
      JSON.stringify({ name: false, removed: false })
    )
    window.localStorage.setItem(
      'demo:column-sizing',
      JSON.stringify({ name: 220, removed: 999 })
    )
    window.localStorage.setItem(
      'demo:column-order',
      JSON.stringify(['status', 'removed', 'name'])
    )
    window.localStorage.setItem(
      'demo:sorting',
      JSON.stringify([
        { id: 'name', desc: true },
        { id: 'removed', desc: false },
      ])
    )
    window.localStorage.setItem('demo:page-size', '50')

    const { result } = renderHook(() =>
      useDataTable({
        data,
        columns,
        tableStateStorageKey: 'demo',
        enableColumnResizing: false,
      })
    )

    expect(result.current.table.getState().columnVisibility).toEqual({
      name: false,
    })
    expect(result.current.table.getState().columnSizing).toEqual({ name: 220 })
    expect(result.current.table.getState().columnOrder).toEqual([
      'status',
      'name',
      'id',
    ])
    expect(result.current.table.getState().sorting).toEqual([
      { id: 'name', desc: true },
    ])
    expect(result.current.table.getState().pagination).toEqual({
      pageIndex: 0,
      pageSize: 50,
    })
  })

  test('does not restore stale search, filters, or page number and resets the saved view', () => {
    window.localStorage.setItem('demo:global-filter', 'stale search')
    window.localStorage.setItem(
      'demo:column-filters',
      JSON.stringify([{ id: 'status', value: ['disabled'] }])
    )
    window.localStorage.setItem(
      'demo:pagination',
      JSON.stringify({ pageIndex: 7, pageSize: 100 })
    )
    window.localStorage.setItem(
      'demo:column-visibility',
      JSON.stringify({ name: false })
    )
    window.localStorage.setItem('demo:page-size', '100')

    const { result } = renderHook(() =>
      useDataTable({
        data,
        columns,
        tableStateStorageKey: 'demo',
        enableColumnResizing: false,
        initialPagination: { pageIndex: 0, pageSize: 20 },
      })
    )

    expect(result.current.table.getState().globalFilter).toBeUndefined()
    expect(result.current.table.getState().columnFilters).toBeUndefined()
    expect(result.current.table.getState().pagination.pageIndex).toBe(0)
    expect(result.current.table.getState().pagination.pageSize).toBe(100)

    act(() => result.current.table.options.meta?.resetPersistedView?.())

    expect(
      JSON.parse(window.localStorage.getItem('demo:column-visibility') || '{}')
    ).toEqual({})
    expect(window.localStorage.getItem('demo:page-size')).toBe('20')
    expect(result.current.table.getState().columnVisibility).toEqual({})
    expect(result.current.table.getState().pagination).toEqual({
      pageIndex: 0,
      pageSize: 20,
    })
  })
})
