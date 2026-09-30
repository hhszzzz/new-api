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
import type { OnChangeFn, PaginationState } from '@tanstack/react-table'

import { DataTablePagination, useDataTable } from '@/components/data-table'
import { useMediaQuery } from '@/hooks'

/** All three question readers use server totals, not the number of loaded rows. */
export function PromptAuditGroupPagination(props: {
  total: number
  pagination: PaginationState
  onChange: OnChangeFn<PaginationState>
}) {
  const compact = useMediaQuery('(max-width: 640px)')
  const { table } = useDataTable({
    data: [],
    columns: [],
    pagination: props.pagination,
    onPaginationChange: props.onChange,
    manualPagination: true,
    totalCount: props.total,
    enableRowSelection: false,
  })
  return <DataTablePagination table={table} compact={compact} />
}
