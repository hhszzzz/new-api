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
import type { Table as TanstackTable } from '@tanstack/react-table'
import type * as React from 'react'

import { isContentSizedColumn } from './content-sized-columns'

export function getTableSizeStyle<TData>(
  table: TanstackTable<TData>
): React.CSSProperties {
  if (hasResolvedColumnSizing(table)) {
    const width = getResolvedTableWidth(table)

    return {
      minWidth: `${width}px`,
      tableLayout: 'fixed',
      width: `${width}px`,
    }
  }

  const width = table
    .getVisibleLeafColumns()
    .filter((column) => !isContentSizedColumn(column.id))
    .reduce((total, column) => total + column.getSize(), 0)

  return {
    minWidth: `max(100%, ${width}px)`,
    tableLayout: 'auto',
    width: '100%',
  }
}

export function hasResolvedColumnSizing<TData>(
  table: TanstackTable<TData>
): boolean {
  if (table.options.enableColumnResizing !== true) {
    return false
  }

  const columnSizing = table.getState().columnSizing
  const visibleColumns = table.getVisibleLeafColumns()

  return (
    visibleColumns.length > 0 &&
    visibleColumns.every((column) => Number.isFinite(columnSizing[column.id]))
  )
}

function getResolvedTableWidth<TData>(table: TanstackTable<TData>): number {
  return table
    .getVisibleLeafColumns()
    .reduce((total, column) => total + column.getSize(), 0)
}
