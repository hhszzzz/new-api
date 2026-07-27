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
import type { OnChangeFn, SortingState, Row } from '@tanstack/react-table'
import { AlertTriangle, Eye, EyeOff, RefreshCw } from 'lucide-react'
import { useMemo, useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DISABLED_ROW_DESKTOP,
  DISABLED_ROW_MOBILE,
  DataTablePage,
  useDebouncedColumnFilter,
  useDataTable,
  usePersistedTableSorting,
} from '@/components/data-table'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { getLobeIcon } from '@/lib/lobe-icon'

import { getChannels, searchChannels, getGroups } from '../api'
import {
  DEFAULT_PAGE_SIZE,
  CHANNEL_STATUS,
  CHANNEL_STATUS_OPTIONS,
} from '../constants'
import {
  channelsQueryKeys,
  aggregateChannelsByAggregate,
  aggregateChannelsByTag,
  isChannelAggregateRow,
  getChannelTableRowId,
  isTagAggregateRow,
  getChannelTypeIcon,
  getChannelTypeLabel,
} from '../lib'
import type { Channel, ChannelSortBy } from '../types'
import { ChannelCard } from './channel-card'
import { useChannelsColumns } from './channels-columns'
import { useChannels } from './channels-provider'
import { DataTableBulkActions } from './data-table-bulk-actions'

const route = getRouteApi('/_authenticated/channels/')
const CHANNELS_COLUMN_VISIBILITY_STORAGE_KEY = 'channels:column-visibility'
const CHANNELS_COLUMN_SIZING_STORAGE_KEY = 'channels:column-sizing'
const CHANNELS_VIEW_MODE_STORAGE_KEY = 'channels:view-mode'
const CHANNELS_TABLE_STATE_STORAGE_KEY = 'channels:admin'

const CHANNEL_SORTABLE_COLUMNS = new Set<ChannelSortBy>([
  'id',
  'name',
  'priority',
  'balance',
  'response_time',
  'test_time',
])

function isDisabledChannelRow(channel: Channel) {
  return (
    !isTagAggregateRow(channel) &&
    !isChannelAggregateRow(channel) &&
    channel.status !== CHANNEL_STATUS.ENABLED
  )
}

export function ChannelsTable() {
  const { t } = useTranslation()
  const {
    enableTagMode,
    idSort,
    batchMode,
    sensitiveVisible,
    setSensitiveVisible,
  } = useChannels()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const defaultPageSize = isMobile ? 10 : DEFAULT_PAGE_SIZE

  // Table state
  const [sorting, setSorting] = usePersistedTableSorting(
    CHANNELS_TABLE_STATE_STORAGE_KEY,
    CHANNEL_SORTABLE_COLUMNS
  )

  // URL state management
  const {
    globalFilter,
    onGlobalFilterChange,
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
      defaultPageSize,
      pageSizeStorageKey: 'channels:admin:page-size',
    },
    globalFilter: { enabled: true, key: 'filter' },
    columnFilters: [
      {
        columnId: 'status',
        searchKey: 'status',
        type: 'array',
      },
      { columnId: 'type', searchKey: 'type', type: 'array' },
      { columnId: 'group', searchKey: 'group', type: 'array' },
      { columnId: 'model', searchKey: 'model', type: 'string' },
    ],
  })

  // Extract filters from column filters
  const statusFilter =
    (columnFilters.find((f) => f.id === 'status')?.value as string[]) || []
  const typeFilter = useMemo(
    () => (columnFilters.find((f) => f.id === 'type')?.value as string[]) || [],
    [columnFilters]
  )
  const groupFilter =
    (columnFilters.find((f) => f.id === 'group')?.value as string[]) || []
  const {
    value: modelFilter,
    inputValue: modelFilterInput,
    onChange: onModelFilterInputChange,
    onCompositionStart: onModelFilterCompositionStart,
    onCompositionEnd: onModelFilterCompositionEnd,
    resetInput: resetModelFilterInput,
  } = useDebouncedColumnFilter({
    columnFilters,
    columnId: 'model',
    onColumnFiltersChange,
  })

  // Determine whether to use search or regular list API
  const shouldSearch = Boolean(globalFilter?.trim() || modelFilter.trim())

  const sortParams = useMemo(() => {
    const activeSort = sorting[0]
    if (
      !activeSort ||
      !CHANNEL_SORTABLE_COLUMNS.has(activeSort.id as ChannelSortBy)
    ) {
      return {}
    }

    return {
      sort_by: activeSort.id as ChannelSortBy,
      sort_order: activeSort.desc ? 'desc' : 'asc',
    } as const
  }, [sorting])

  const handleSortingChange: OnChangeFn<SortingState> = (updater) => {
    setSorting((previous) => {
      const next = typeof updater === 'function' ? updater(previous) : updater
      if (pagination.pageIndex > 0) {
        onPaginationChange({ ...pagination, pageIndex: 0 })
      }
      return next
    })
  }

  // Fetch groups for filter
  const { data: groupsData } = useQuery({
    queryKey: ['groups'],
    queryFn: getGroups,
  })

  const groupOptions = useMemo(
    () =>
      (groupsData?.data || []).map((g) => ({
        label: g,
        value: g,
      })),
    [groupsData]
  )

  // Fetch channels data
  // eslint-disable-next-line @tanstack/query/exhaustive-deps
  const { data, isLoading, isFetching, isError, error, refetch } = useQuery({
    queryKey: channelsQueryKeys.list({
      keyword: globalFilter,
      model: modelFilter,
      group:
        groupFilter.length > 0 && !groupFilter.includes('all')
          ? groupFilter[0]
          : undefined,
      status:
        statusFilter.length > 0 && !statusFilter.includes('all')
          ? statusFilter[0]
          : undefined,
      type:
        typeFilter.length > 0 && !typeFilter.includes('all')
          ? Number(typeFilter[0])
          : undefined,
      tag_mode: enableTagMode,
      aggregate_mode: !enableTagMode,
      id_sort: idSort,
      ...sortParams,
      p: pagination.pageIndex + 1,
      page_size: pagination.pageSize,
    }),
    queryFn: async () => {
      if (shouldSearch) {
        const response = await searchChannels({
          keyword: globalFilter,
          model: modelFilter,
          group:
            groupFilter.length > 0 && !groupFilter.includes('all')
              ? groupFilter[0]
              : undefined,
          status:
            statusFilter.length > 0 && !statusFilter.includes('all')
              ? statusFilter[0]
              : undefined,
          type:
            typeFilter.length > 0 && !typeFilter.includes('all')
              ? Number(typeFilter[0])
              : undefined,
          tag_mode: enableTagMode,
          aggregate_mode: !enableTagMode,
          id_sort: idSort,
          ...sortParams,
          p: pagination.pageIndex + 1,
          page_size: pagination.pageSize,
        })
        if (!response.success) {
          throw new Error(response.message || t('Failed to load channels'))
        }
        return response
      } else {
        const response = await getChannels({
          group:
            groupFilter.length > 0 && !groupFilter.includes('all')
              ? groupFilter[0]
              : undefined,
          status:
            statusFilter.length > 0 && !statusFilter.includes('all')
              ? statusFilter[0]
              : undefined,
          type:
            typeFilter.length > 0 && !typeFilter.includes('all')
              ? Number(typeFilter[0])
              : undefined,
          tag_mode: enableTagMode,
          aggregate_mode: !enableTagMode,
          id_sort: idSort,
          ...sortParams,
          p: pagination.pageIndex + 1,
          page_size: pagination.pageSize,
        })
        if (!response.success) {
          throw new Error(response.message || t('Failed to load channels'))
        }
        return response
      }
    },
    placeholderData: (previousData) => previousData,
  })

  // Apply tag aggregation if tag mode is enabled
  const channels = useMemo(() => {
    const rawChannels = data?.data?.items || []

    if (enableTagMode && rawChannels.length > 0) {
      return aggregateChannelsByTag(rawChannels)
    }

    if (!enableTagMode && rawChannels.length > 0) {
      return aggregateChannelsByAggregate(rawChannels)
    }

    return rawChannels
  }, [data, enableTagMode])

  const totalCount = data?.data?.total || 0
  const typeCounts = data?.data?.type_counts

  // Columns configuration
  const columns = useChannelsColumns({ enableSelection: batchMode })

  // React Table instance
  const { table } = useDataTable({
    data: channels,
    columns,
    tableStateStorageKey: CHANNELS_TABLE_STATE_STORAGE_KEY,
    totalCount,
    sorting,
    initialPagination: { pageIndex: 0, pageSize: defaultPageSize },
    initialColumnVisibility: {
      models: false,
      tag: false,
    },
    columnVisibilityStorageKey: CHANNELS_COLUMN_VISIBILITY_STORAGE_KEY,
    columnSizingStorageKey: isMobile
      ? false
      : CHANNELS_COLUMN_SIZING_STORAGE_KEY,
    columnFilters,
    pagination,
    globalFilter,
    enableRowSelection: batchMode
      ? (row: Row<Channel>) =>
          !isTagAggregateRow(row.original) &&
          !isChannelAggregateRow(row.original)
      : false,
    onSortingChange: handleSortingChange,
    onColumnFiltersChange,
    onPaginationChange,
    onGlobalFilterChange,
    getRowId: getChannelTableRowId,
    getSubRows: (row: Channel & { children?: Channel[] }) => row.children,
    manualPagination: true,
    manualSorting: true,
    manualFiltering: true,
    withExpandedRowModel: true,
    enableColumnResizing: !isMobile,
    ensurePageInRange,
  })

  useEffect(() => {
    if (!batchMode) {
      table.resetRowSelection()
    }
  }, [batchMode, table])

  // Prepare filter options from existing channel types only.
  const typeFilterOptions = useMemo(() => {
    const counts = typeCounts || {}
    const typeIds = Object.entries(counts)
      .map(([type, count]) => ({
        type: Number(type),
        count: Number(count) || 0,
      }))
      .filter((item) => item.type > 0 && item.count > 0)
      .sort((a, b) => {
        const labelA = t(getChannelTypeLabel(a.type))
        const labelB = t(getChannelTypeLabel(b.type))
        return labelA.localeCompare(labelB)
      })

    const selectedType = typeFilter.find((value) => value !== 'all')
    if (selectedType) {
      const selectedTypeId = Number(selectedType)
      const alreadyIncluded = typeIds.some(
        (item) => item.type === selectedTypeId
      )
      if (selectedTypeId > 0 && !alreadyIncluded) {
        typeIds.push({
          type: selectedTypeId,
          count: Number(counts[selectedType]) || 0,
        })
      }
    }

    const totalTypes = Object.values(counts).reduce(
      (sum, count) => sum + (Number(count) || 0),
      0
    )

    return [
      {
        label: 'All Types',
        value: 'all',
        count: totalTypes,
      },
      ...typeIds.map((item) => {
        const iconName = getChannelTypeIcon(item.type)
        return {
          label: getChannelTypeLabel(item.type),
          value: String(item.type),
          count: item.count,
          iconNode: getLobeIcon(`${iconName}.Color`, 16),
        }
      }),
    ]
  }, [t, typeCounts, typeFilter])

  const groupFilterOptions = [
    { label: t('All Groups'), value: 'all' },
    ...groupOptions.map((option) => ({
      ...option,
      label: sensitiveVisible ? option.label : '••••',
    })),
  ]

  const isLoadingData = isLoading || (isFetching && !data)
  const errorMessage =
    error instanceof Error ? error.message : t('Failed to load channels')

  const tablePage = (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoadingData}
      isFetching={isFetching}
      emptyTitle={t('No Channels Found')}
      emptyDescription={t(
        'No channels available. Create your first channel to get started.'
      )}
      skeletonKeyPrefix='channel-skeleton'
      enableCardView
      viewModeStorageKey={CHANNELS_VIEW_MODE_STORAGE_KEY}
      renderCard={(row, { isSelected }) => (
        <ChannelCard row={row} isSelected={isSelected} />
      )}
      cardGridClassName='grid grid-cols-1 gap-3 sm:gap-4 lg:grid-cols-3'
      applyHeaderSize
      toolbarProps={{
        searchPlaceholder: t('Filter by name, ID, or key...'),
        searchDebounceMs: 500,
        onReset: () => {
          resetModelFilterInput()
        },
        additionalSearch: (
          <Input
            placeholder={t('Filter by model...')}
            value={modelFilterInput}
            onChange={onModelFilterInputChange}
            onCompositionStart={onModelFilterCompositionStart}
            onCompositionEnd={onModelFilterCompositionEnd}
            className='w-full sm:w-[150px] lg:w-[180px]'
          />
        ),
        filters: [
          {
            columnId: 'status',
            title: t('Status'),
            options: [...CHANNEL_STATUS_OPTIONS],
            singleSelect: true,
          },
          {
            columnId: 'type',
            title: t('Type'),
            options: typeFilterOptions,
            singleSelect: true,
          },
          {
            columnId: 'group',
            title: t('Group'),
            options: groupFilterOptions,
            singleSelect: true,
          },
        ],
        preActions: (
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant='ghost'
                  size='icon'
                  onClick={() => setSensitiveVisible(!sensitiveVisible)}
                  aria-label={sensitiveVisible ? t('Hide') : t('Show')}
                  className='text-muted-foreground hover:text-foreground size-8'
                />
              }
            >
              {sensitiveVisible ? <Eye /> : <EyeOff />}
            </TooltipTrigger>
            <TooltipContent>
              {sensitiveVisible ? t('Hide') : t('Show')}
            </TooltipContent>
          </Tooltip>
        ),
      }}
      getRowClassName={(row, { isMobile }) => {
        if (!isDisabledChannelRow(row.original)) {
          return undefined
        }
        if (isMobile) {
          return DISABLED_ROW_MOBILE
        }
        return DISABLED_ROW_DESKTOP
      }}
      bulkActions={batchMode ? <DataTableBulkActions table={table} /> : null}
    />
  )

  if (isError && !data) {
    return (
      <div className='flex min-h-56 items-center justify-center'>
        <Alert className='max-w-lg'>
          <AlertTriangle className='h-4 w-4' />
          <AlertDescription className='flex items-center justify-between gap-3'>
            <span>{errorMessage}</span>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => void refetch()}
            >
              <RefreshCw className='h-4 w-4' />
              {t('Retry')}
            </Button>
          </AlertDescription>
        </Alert>
      </div>
    )
  }

  return (
    <div className='flex h-full min-h-0 flex-col gap-2'>
      {isError && (
        <Alert>
          <AlertTriangle className='h-4 w-4' />
          <AlertDescription className='flex items-center justify-between gap-3'>
            <span>{errorMessage}</span>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => void refetch()}
            >
              <RefreshCw className='h-4 w-4' />
              {t('Retry')}
            </Button>
          </AlertDescription>
        </Alert>
      )}
      {tablePage}
    </div>
  )
}
