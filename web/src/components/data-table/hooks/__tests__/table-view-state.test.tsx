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
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { useDataTable, usePersistedTableSorting } from '../use-data-table'

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

  test('uses the configured initial column order when no saved order exists', () => {
    const initialColumnOrder = ['status', 'name', 'id']

    const { result } = renderHook(() =>
      useDataTable({
        data,
        columns,
        tableStateStorageKey: 'demo',
        initialColumnOrder,
        enableColumnResizing: false,
      })
    )

    expect(result.current.table.getState().columnOrder).toEqual(
      initialColumnOrder
    )
  })

  test('reset restores the configured initial column order', () => {
    window.localStorage.setItem(
      'demo:column-order',
      JSON.stringify(['name', 'id', 'status'])
    )
    const initialColumnOrder = ['status', 'name', 'id']

    const { result } = renderHook(() =>
      useDataTable({
        data,
        columns,
        tableStateStorageKey: 'demo',
        initialColumnOrder,
        enableColumnResizing: false,
      })
    )

    expect(result.current.table.getState().columnOrder).toEqual([
      'name',
      'id',
      'status',
    ])

    act(() => result.current.table.options.meta?.resetPersistedView?.())

    expect(result.current.table.getState().columnOrder).toEqual(
      initialColumnOrder
    )
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

  test('does not hydrate controlled sorting or pagination through change callbacks', () => {
    window.localStorage.setItem(
      'demo:sorting',
      JSON.stringify([{ id: 'name', desc: true }])
    )
    window.localStorage.setItem('demo:page-size', '50')
    const onSortingChange = vi.fn()
    const onPaginationChange = vi.fn()

    const { result } = renderHook(() =>
      useDataTable({
        data,
        columns,
        tableStateStorageKey: 'demo',
        sorting: [],
        pagination: { pageIndex: 0, pageSize: 20 },
        initialPagination: { pageIndex: 0, pageSize: 30 },
        onSortingChange,
        onPaginationChange,
        enableColumnResizing: false,
      })
    )

    expect(result.current.table.getState().sorting).toEqual([])
    expect(result.current.table.getState().pagination).toEqual({
      pageIndex: 0,
      pageSize: 20,
    })
    expect(onSortingChange).not.toHaveBeenCalled()
    expect(onPaginationChange).not.toHaveBeenCalled()

    act(() => result.current.table.options.meta?.resetPersistedView?.())

    expect(onPaginationChange).toHaveBeenCalledTimes(1)
    const resetPagination = onPaginationChange.mock.calls[0]?.[0]
    expect(
      typeof resetPagination === 'function'
        ? resetPagination({ pageIndex: 2, pageSize: 50 })
        : resetPagination
    ).toEqual({ pageIndex: 0, pageSize: 30 })
  })

  test('keeps manual server sorting to one column', () => {
    const { result } = renderHook(() =>
      useDataTable({
        data,
        columns,
        manualSorting: true,
        enableColumnResizing: false,
      })
    )

    act(() => result.current.table.getColumn('name')?.toggleSorting(false))
    act(() =>
      result.current.table.getColumn('status')?.toggleSorting(false, true)
    )

    expect(result.current.table.getState().sorting).toEqual([
      { id: 'status', desc: false },
    ])
  })

  test('resets the live table when browser storage removal is unavailable', () => {
    window.localStorage.setItem(
      'demo:column-visibility',
      JSON.stringify({ name: false })
    )
    const { result } = renderHook(() =>
      useDataTable({
        data,
        columns,
        tableStateStorageKey: 'demo',
        enableColumnResizing: false,
      })
    )
    const removeItem = vi
      .spyOn(Storage.prototype, 'removeItem')
      .mockImplementation(() => {
        throw new DOMException('Storage is disabled', 'SecurityError')
      })

    try {
      expect(() => {
        act(() => result.current.table.options.meta?.resetPersistedView?.())
      }).not.toThrow()
    } finally {
      removeItem.mockRestore()
    }

    expect(result.current.table.getState().columnVisibility).toEqual({})
  })

  test('restores controlled sorting synchronously for the caller', () => {
    window.localStorage.setItem(
      'demo:sorting',
      JSON.stringify([
        { id: 'name', desc: true },
        { id: 'removed', desc: false },
      ])
    )

    const { result } = renderHook(() =>
      usePersistedTableSorting('demo', new Set(['id', 'name', 'status']))
    )

    expect(result.current[0]).toEqual([{ id: 'name', desc: true }])
  })

  test('ignores a persisted page size that the pagination control cannot select', () => {
    window.localStorage.setItem('demo:page-size', '25')

    const { result } = renderHook(() =>
      useDataTable({
        data,
        columns,
        tableStateStorageKey: 'demo',
        initialPagination: { pageIndex: 0, pageSize: 20 },
        enableColumnResizing: false,
      })
    )

    expect(result.current.table.getState().pagination).toEqual({
      pageIndex: 0,
      pageSize: 20,
    })
  })

  test('rehydrates uncontrolled sorting and pagination when the table key changes', () => {
    window.localStorage.setItem(
      'first:sorting',
      JSON.stringify([{ id: 'name', desc: true }])
    )
    window.localStorage.setItem('first:page-size', '50')
    window.localStorage.setItem(
      'second:sorting',
      JSON.stringify([{ id: 'status', desc: false }])
    )
    window.localStorage.setItem('second:page-size', '30')

    const { result, rerender } = renderHook(
      ({ storageKey }) =>
        useDataTable({
          data,
          columns,
          tableStateStorageKey: storageKey,
          initialPagination: { pageIndex: 0, pageSize: 20 },
          enableColumnResizing: false,
        }),
      { initialProps: { storageKey: 'first' } }
    )

    expect(result.current.table.getState().sorting).toEqual([
      { id: 'name', desc: true },
    ])
    expect(result.current.table.getState().pagination.pageSize).toBe(50)

    rerender({ storageKey: 'second' })

    expect(result.current.table.getState().sorting).toEqual([
      { id: 'status', desc: false },
    ])
    expect(result.current.table.getState().pagination).toEqual({
      pageIndex: 0,
      pageSize: 30,
    })
  })
})
