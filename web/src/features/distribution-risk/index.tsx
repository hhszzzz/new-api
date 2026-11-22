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
import { Link } from '@tanstack/react-router'
import { RefreshCw, Settings2 } from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTablePage,
  getInitialTablePageSize,
  useDataTable,
} from '@/components/data-table'
import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { getDefaultTimeRange } from '@/features/usage-logs/lib/utils'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { listDistributionRiskEvents } from './api'
import { useDistributionRiskEventColumns } from './components/distribution-risk-event-columns'
import { DistributionRiskEventDialog } from './components/distribution-risk-event-dialog'
import { DistributionRiskExportDialog } from './components/distribution-risk-export-dialog'
import {
  DistributionRiskFilterBar,
  type DistributionRiskEventDraftFilters,
} from './components/distribution-risk-filter-bar'
import type { DistributionRiskEvent, DistributionRiskEventQuery } from './types'

const TABLE_STATE_KEY = 'distribution-risk:events'
const EMPTY_EVENTS: DistributionRiskEvent[] = []
function createDefaultFilters(): DistributionRiskEventDraftFilters {
  const { start, end } = getDefaultTimeRange()
  return {
    status: 'all',
    level: 'all',
    username: '',
    start_timestamp: Math.floor(start.getTime() / 1000),
    end_timestamp: Math.floor(end.getTime() / 1000),
  }
}

export function DistributionRiskPage() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const canRead = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.DISTRIBUTION_RISK,
    ADMIN_PERMISSION_ACTIONS.READ
  )
  const canManage = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.DISTRIBUTION_RISK,
    ADMIN_PERMISSION_ACTIONS.MANAGE
  )
  const [initialFilters] = useState(createDefaultFilters)
  const [draftFilters, setDraftFilters] = useState(initialFilters)
  const [appliedFilters, setAppliedFilters] = useState(initialFilters)
  const [pagination, setPagination] = useState(() => ({
    pageIndex: 0,
    pageSize: getInitialTablePageSize(TABLE_STATE_KEY, 20),
  }))
  const [eventID, setEventID] = useState<number | null>(null)
  const isSuperAdmin = user?.role === ROLE.SUPER_ADMIN

  const invalidDateRange =
    draftFilters.start_timestamp !== undefined &&
    draftFilters.end_timestamp !== undefined &&
    draftFilters.start_timestamp > draftFilters.end_timestamp
  const queryFilters = useMemo<DistributionRiskEventQuery>(() => {
    const filters: DistributionRiskEventQuery = {
      status: appliedFilters.status,
      level: appliedFilters.level,
    }
    if (appliedFilters.username.trim()) {
      filters.username = appliedFilters.username.trim()
    }
    if (appliedFilters.start_timestamp !== undefined) {
      filters.start_timestamp = appliedFilters.start_timestamp
    }
    if (appliedFilters.end_timestamp !== undefined) {
      filters.end_timestamp = appliedFilters.end_timestamp
    }
    return filters
  }, [appliedFilters])

  const eventsQuery = useQuery({
    queryKey: [
      'distribution-risk',
      'events',
      queryFilters,
      pagination.pageIndex + 1,
      pagination.pageSize,
    ],
    queryFn: ({ signal }) =>
      listDistributionRiskEvents(
        queryFilters,
        pagination.pageIndex + 1,
        pagination.pageSize,
        signal
      ),
    enabled: canRead,
    refetchInterval: 60_000,
    meta: { errorToast: false },
  })
  const pendingCount = eventsQuery.data?.pending_count ?? 0

  const openDetails = useCallback((id: number) => setEventID(id), [])
  const columns = useDistributionRiskEventColumns(openDetails)
  const { table } = useDataTable({
    columns,
    tableStateStorageKey: TABLE_STATE_KEY,
    data: canRead ? (eventsQuery.data?.items ?? EMPTY_EVENTS) : EMPTY_EVENTS,
    getRowId: (event) => String(event.id),
    totalCount: eventsQuery.data?.total ?? 0,
    pagination,
    onPaginationChange: (updater) => {
      if (eventsQuery.isFetching) return
      setPagination((current) => {
        const next = typeof updater === 'function' ? updater(current) : updater
        if (next.pageSize === current.pageSize) {
          return next
        }
        return {
          ...next,
          pageIndex: 0,
        }
      })
    },
    ensurePageInRange: (pageCount) => {
      if (!eventsQuery.isSuccess || eventsQuery.isFetching) {
        return
      }
      setPagination((current) =>
        current.pageIndex >= Math.max(1, pageCount)
          ? { ...current, pageIndex: Math.max(0, pageCount - 1) }
          : current
      )
    },
    enableRowSelection: false,
    enableSorting: false,
    manualFiltering: true,
    manualPagination: true,
  })

  const onFilterChange = (patch: Partial<DistributionRiskEventDraftFilters>) =>
    setDraftFilters((current) => ({ ...current, ...patch }))

  const applyFilters = () => {
    if (invalidDateRange) return
    if (
      pagination.pageIndex === 0 &&
      JSON.stringify(draftFilters) === JSON.stringify(appliedFilters)
    ) {
      void eventsQuery.refetch()
      return
    }
    setAppliedFilters({ ...draftFilters })
    setPagination((current) => ({ ...current, pageIndex: 0 }))
  }

  const resetFilters = () => {
    const filters = createDefaultFilters()
    setDraftFilters(filters)
    setAppliedFilters(filters)
    setPagination((current) => ({ ...current, pageIndex: 0 }))
  }

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('Risk logs')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {canRead && pendingCount > 0 && (
          <Badge variant='warning'>
            {t('Pending: {{count}}', {
              count: pendingCount,
            })}
          </Badge>
        )}
        {isSuperAdmin && (
          <Button
            size='sm'
            variant='outline'
            render={
              <Link
                to='/system-settings/security/$section'
                params={{ section: 'distribution-risk' }}
              />
            }
          >
            <Settings2 data-icon='inline-start' />
            {t('Detection settings')}
          </Button>
        )}
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-3'>
          {invalidDateRange && (
            <Alert variant='destructive'>
              <AlertDescription>
                {t('End time must be after start time')}
              </AlertDescription>
            </Alert>
          )}
          {canRead ? (
            <DataTablePage
              table={table}
              columns={columns}
              isLoading={eventsQuery.isPending}
              isFetching={eventsQuery.isFetching}
              emptyTitle={t('No risk events')}
              className='h-auto min-h-0 flex-1'
              pinnedColumns={[{ columnId: 'details', side: 'right' }]}
              applyHeaderSize
              getColumnClassName={() => 'py-2'}
              tableClassName='[&_[data-slot=table]]:text-[13px] [&_[data-slot=table]_td]:text-[13px] [&_[data-slot=table]_td_*]:text-[13px] [&_[data-slot=table]_th]:text-[13px] [&_[data-slot=table]_th_*]:text-[13px]'
              toolbar={
                <DistributionRiskFilterBar
                  table={table}
                  filters={draftFilters}
                  onChange={onFilterChange}
                  onSearch={applyFilters}
                  onReset={resetFilters}
                  isFetching={eventsQuery.isFetching}
                  actionEnd={
                    <div className='flex items-center gap-2'>
                      <DistributionRiskExportDialog filters={queryFilters} />
                      <Button
                        type='button'
                        size='icon-sm'
                        variant='outline'
                        aria-label={t('Refresh')}
                        title={t('Refresh')}
                        disabled={eventsQuery.isFetching}
                        onClick={() => void eventsQuery.refetch()}
                      >
                        <RefreshCw
                          className={
                            eventsQuery.isFetching ? 'animate-spin' : ''
                          }
                        />
                      </Button>
                    </div>
                  }
                />
              }
            />
          ) : (
            <Alert variant='destructive'>
              <AlertDescription>
                {t('You do not have permission to view risk events')}
              </AlertDescription>
            </Alert>
          )}
        </div>
        <DistributionRiskEventDialog
          key={eventID ?? 'closed'}
          eventID={eventID}
          canManage={canManage}
          onOpenChange={(open) => {
            if (!open) setEventID(null)
          }}
          onReviewed={() => {
            setPagination((current) => ({ ...current, pageIndex: 0 }))
          }}
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
