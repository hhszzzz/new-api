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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test } from 'vitest'

import { useDataTable } from '../../hooks/use-data-table'
import { DataTableView } from '../data-table-view'

type ExpandableRow = {
  id: number
  name: string
  children?: ExpandableRow[]
}

const expandableRows: ExpandableRow[] = [
  {
    id: 1,
    name: 'Aggregate',
    children: [{ id: 2, name: 'Child channel' }],
  },
]

const expandableColumns: ColumnDef<ExpandableRow>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
    cell: ({ row }) => {
      if (row.depth > 0) return row.original.name

      const isExpanded = row.getIsExpanded()
      return (
        <button
          type='button'
          data-state={isExpanded ? 'open' : 'closed'}
          aria-expanded={isExpanded}
          onClick={row.getToggleExpandedHandler()}
        >
          {isExpanded ? 'Collapse aggregate' : 'Expand aggregate'}
        </button>
      )
    },
  },
]

function ExpandableTable() {
  const { table } = useDataTable({
    data: expandableRows,
    columns: expandableColumns,
    getSubRows: (row) => row.children,
    withExpandedRowModel: true,
  })

  return <DataTableView table={table} />
}

describe('data table row expansion state', () => {
  test('re-renders the parent row when its expanded state changes', async () => {
    const user = userEvent.setup()
    render(<ExpandableTable />)

    const expandButton = screen.getByRole('button', {
      name: 'Expand aggregate',
    })
    expect(expandButton).toHaveAttribute('data-state', 'closed')

    await user.click(expandButton)

    expect(
      screen.getByRole('button', { name: 'Collapse aggregate' })
    ).toHaveAttribute('data-state', 'open')
    expect(screen.getByText('Child channel')).toBeVisible()
  })
})
