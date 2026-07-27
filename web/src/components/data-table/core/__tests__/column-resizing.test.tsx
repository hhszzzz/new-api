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
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { useDataTable } from '../../hooks/use-data-table'
import { DataTableView } from '../data-table-view'

type RowData = { name: string }

type MultiColumnRowData = {
  name: string
  status: string
}

const columns: ColumnDef<RowData>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
    size: 120,
    minSize: 100,
    maxSize: 180,
  },
]

function ResizableTable() {
  const { table } = useDataTable({
    data: [{ name: 'A long channel name' }],
    columns,
    initialColumnSizing: { name: 120 },
    enableColumnResizing: true,
  })

  return (
    <>
      <output data-testid='column-size'>
        {table.getColumn('name')?.getSize()}
      </output>
      <DataTableView table={table} applyHeaderSize />
    </>
  )
}

const multiColumnColumns: ColumnDef<MultiColumnRowData>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
    size: 120,
  },
  {
    accessorKey: 'status',
    header: 'Status',
    size: 140,
  },
]

function MultiColumnResizableTable() {
  const { table } = useDataTable({
    data: [{ name: 'Primary channel', status: 'Enabled' }],
    columns: multiColumnColumns,
    initialColumnSizing: { name: 120, status: 140 },
    enableColumnResizing: true,
  })

  return (
    <>
      <output data-testid='name-column-size'>
        {table.getColumn('name')?.getSize()}
      </output>
      <output data-testid='status-column-size'>
        {table.getColumn('status')?.getSize()}
      </output>
      <DataTableView table={table} applyHeaderSize />
    </>
  )
}

describe('data table column resizing', () => {
  test('supports keyboard resizing and clamps the result to column bounds', () => {
    render(<ResizableTable />)
    const separator = screen.getByRole('separator', { name: 'Resize column' })

    fireEvent.keyDown(separator, { key: 'ArrowRight' })
    expect(screen.getByTestId('column-size')).toHaveTextContent('130')

    fireEvent.keyDown(separator, { key: 'ArrowLeft', shiftKey: true })
    expect(screen.getByTestId('column-size')).toHaveTextContent('100')
  })

  test('double click auto-sizes content and respects the maximum width', () => {
    vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockReturnValue(240)
    render(<ResizableTable />)

    fireEvent.doubleClick(
      screen.getByRole('separator', { name: 'Resize column' })
    )

    expect(screen.getByTestId('column-size')).toHaveTextContent('180')
  })

  test('dragging one resize handle keeps the neighboring column width stable', () => {
    render(<MultiColumnResizableTable />)
    const table = screen.getByRole('table')
    const columns = table.querySelectorAll('col')
    const [nameResizer] = screen.getAllByRole('separator', {
      name: 'Resize column',
    })

    expect(table).toHaveStyle({ tableLayout: 'fixed' })
    expect(columns[0]).toHaveStyle({ width: '120px' })
    expect(columns[1]).toHaveStyle({ width: '140px' })

    fireEvent.mouseDown(nameResizer, { clientX: 120 })
    fireEvent.mouseMove(document, { clientX: 150 })
    fireEvent.mouseUp(document, { clientX: 150 })

    expect(screen.getByTestId('name-column-size')).toHaveTextContent('150')
    expect(screen.getByTestId('status-column-size')).toHaveTextContent('140')
    expect(columns[0]).toHaveStyle({ width: '150px' })
    expect(columns[1]).toHaveStyle({ width: '140px' })
  })
})
