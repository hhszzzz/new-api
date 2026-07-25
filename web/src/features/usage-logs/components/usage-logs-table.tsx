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
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import type { ColumnDef, OnChangeFn, SortingState } from '@tanstack/react-table'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  DataTablePage,
  DataTableRow,
  useDataTable,
} from '@/components/data-table'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { cn } from '@/lib/utils'

import {
  DEFAULT_LOGS_DATA,
  LOG_TYPE_ALL_VALUE,
  LOG_TYPE_ENUM,
} from '../constants'
import { useColumnsByCategory } from '../lib/columns'
import { parseLogOther } from '../lib/format'
import { fetchLogsByCategory } from '../lib/utils'
import type { LogCategory, LogSortOrder, UsageLogSortBy } from '../types'
import { CommonLogsFilterBar } from './common-logs-filter-bar'
import { TaskLogsFilterBar } from './task-logs-filter-bar'
import { UsageLogsMobileList } from './usage-logs-mobile-card'
import { useLogsViewScope } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

const logTypeRowTint: Record<number, string> = {
  [LOG_TYPE_ENUM.ERROR]: 'bg-rose-50/40 dark:bg-rose-950/20',
  [LOG_TYPE_ENUM.REFUND]: 'bg-blue-50/30 dark:bg-blue-950/15',
}

// Warning tint for logs where a quota conversion saturated (admin-only marker).
// Takes precedence over the per-type tint since it flags a billing anomaly.
const quotaSaturationRowTint = 'bg-amber-50/60 dark:bg-amber-950/25'

const LOG_SORTABLE_COLUMNS: Record<LogCategory, Set<UsageLogSortBy>> = {
  common: new Set([
    'created_at',
    'channel',
    'user',
    'token_name',
    'model_name',
    'is_stream',
    'prompt_tokens',
    'quota',
    'use_time',
  ]),
  drawing: new Set([
    'submit_time',
    'channel_id',
    'action',
    'mj_id',
    'code',
    'progress',
  ]),
  task: new Set([
    'submit_time',
    'channel_id',
    'user',
    'task_id',
    'status',
    'progress',
  ]),
}

function getColumnVisibilityStorageKey(
  logCategory: LogCategory,
  isAdminView: boolean
): string {
  return `usage-logs:${logCategory}:${isAdminView ? 'admin' : 'user'}:column-visibility`
}

function deserializeLogTypeFilter(value: unknown): unknown[] {
  let values: unknown[] = []
  if (Array.isArray(value)) {
    values = value
  } else if (value) {
    values = [value]
  }
  return values.filter((item) => String(item) !== LOG_TYPE_ALL_VALUE)
}

interface UsageLogsTableProps {
  logCategory: LogCategory
}

export function UsageLogsTable({ logCategory }: UsageLogsTableProps) {
  const { t } = useTranslation()
  const { isAdminView, canViewModelRoute } = useLogsViewScope()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const searchParams = route.useSearch()
  const [sorting, setSorting] = useState<SortingState>([])

  const {
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: {
      defaultPage: 1,
      defaultPageSize: isMobile ? 20 : 100,
      pageSizeStorageKey: `usage-logs:${logCategory}:${isAdminView ? 'admin' : 'user'}:page-size`,
    },
    globalFilter: { enabled: false },
    columnFilters: [
      {
        columnId: 'created_at',
        searchKey: 'type',
        type: 'array' as const,
        deserialize: deserializeLogTypeFilter,
      },
      { columnId: 'model_name', searchKey: 'model', type: 'string' as const },
      { columnId: 'token_name', searchKey: 'token', type: 'string' as const },
      { columnId: 'group', searchKey: 'group', type: 'string' as const },
      ...(isAdminView
        ? [
            {
              columnId: 'channel',
              searchKey: 'channel',
              type: 'string' as const,
            },
            {
              columnId: 'username',
              searchKey: 'username',
              type: 'string' as const,
            },
          ]
        : []),
    ],
  })

  const activeSort = sorting[0]
  const sortBy =
    activeSort &&
    LOG_SORTABLE_COLUMNS[logCategory].has(activeSort.id as UsageLogSortBy)
      ? (activeSort.id as UsageLogSortBy)
      : undefined
  let sortOrder: LogSortOrder | undefined
  if (sortBy) {
    sortOrder = activeSort?.desc ? 'desc' : 'asc'
  }

  const handleSortingChange: OnChangeFn<SortingState> = (updater) => {
    setSorting(updater)
    if (pagination.pageIndex > 0) {
      onPaginationChange({ ...pagination, pageIndex: 0 })
    }
  }

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'logs',
      logCategory,
      isAdminView,
      canViewModelRoute,
      pagination.pageIndex + 1,
      pagination.pageSize,
      columnFilters,
      sorting,
      searchParams,
      t,
    ],
    queryFn: async () => {
      const result = await fetchLogsByCategory({
        logCategory,
        isAdmin: isAdminView,
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
        searchParams,
        columnFilters,
        sortBy,
        sortOrder,
      })

      if (!result?.success) {
        toast.error(result?.message || t('Failed to load logs'))
        return DEFAULT_LOGS_DATA
      }

      return result.data || DEFAULT_LOGS_DATA
    },
    placeholderData: (previousData, previousQuery) => {
      if (previousQuery?.queryKey[1] === logCategory) {
        return previousData
      }
      return undefined
    },
  })

  const logs = data?.items || []
  const rawColumns = useColumnsByCategory(
    logCategory,
    isAdminView,
    canViewModelRoute
  )
  const columns = useMemo(
    () =>
      rawColumns.map((column) => {
        let columnId = column.id
        if (!columnId && 'accessorKey' in column) {
          const accessorKey = column.accessorKey
          if (typeof accessorKey === 'string') columnId = accessorKey
        }
        return {
          ...column,
          enableSorting: Boolean(
            columnId &&
            LOG_SORTABLE_COLUMNS[logCategory].has(columnId as UsageLogSortBy)
          ),
        }
      }),
    [logCategory, rawColumns]
  )
  const isLoadingData = isLoading || (isFetching && !data)

  const { table } = useDataTable({
    data: logs as Record<string, unknown>[],
    columns: columns as ColumnDef<Record<string, unknown>>[],
    tableStateStorageKey: `usage-logs:${logCategory}:${isAdminView ? 'admin' : 'user'}`,
    columnFilters,
    columnVisibilityStorageKey: getColumnVisibilityStorageKey(
      logCategory,
      isAdminView
    ),
    initialColumnVisibility:
      logCategory === 'common'
        ? { client: false, protocol: false, ip: false }
        : undefined,
    pagination,
    sorting,
    enableRowSelection: false,
    onPaginationChange,
    onColumnFiltersChange,
    onSortingChange: handleSortingChange,
    manualPagination: true,
    manualSorting: true,
    manualFiltering: true,
    totalCount: data?.total || 0,
    ensurePageInRange,
  })

  const isCommon = logCategory === 'common'

  return (
    <DataTablePage
      table={table}
      columns={columns as ColumnDef<Record<string, unknown>>[]}
      isLoading={isLoadingData}
      isFetching={isFetching}
      emptyTitle={t('No Logs Found')}
      emptyDescription={t(
        'No usage logs available. Logs will appear here once API calls are made.'
      )}
      skeletonKeyPrefix='usage-log-skeleton'
      applyHeaderSize
      tableClassName={cn(
        '[&_[data-slot=table]]:text-[13px] [&_[data-slot=table]_td]:text-[13px] [&_[data-slot=table]_td_*]:text-[13px] [&_[data-slot=table]_th]:text-[13px] [&_[data-slot=table]_th_*]:text-[13px]'
      )}
      mobile={
        <UsageLogsMobileList
          table={table}
          isLoading={isLoadingData}
          logCategory={logCategory}
        />
      }
      toolbar={
        isCommon ? (
          <CommonLogsFilterBar table={table} />
        ) : (
          <TaskLogsFilterBar table={table} logCategory={logCategory} />
        )
      }
      renderRow={(row) => {
        const logType = (row.original as Record<string, unknown>).type as
          | number
          | undefined
        let tintClass =
          isCommon && logType != null ? (logTypeRowTint[logType] ?? '') : ''
        if (isCommon && isAdminView) {
          const other = parseLogOther(
            ((row.original as Record<string, unknown>).other as string) ?? ''
          )
          if (other?.admin_info?.quota_saturation) {
            tintClass = quotaSaturationRowTint
          }
        }

        return (
          <DataTableRow
            key={row.id}
            row={row}
            className={cn('transition-colors', tintClass)}
            getColumnClassName={() => (isCommon ? 'py-2' : 'py-3.5')}
          />
        )
      }}
    />
  )
}
