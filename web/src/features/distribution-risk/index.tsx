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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { RefreshCw, Settings2 } from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import {
  DistributionRiskFilterBar,
  type DistributionRiskEventDraftFilters,
} from './components/distribution-risk-filter-bar'
import type { DistributionRiskEvent, DistributionRiskEventQuery } from './types'

const PAGE_SIZE = 20
const EMPTY_EVENTS: DistributionRiskEvent[] = []
const DEFAULT_FILTERS: DistributionRiskEventDraftFilters = {
  status: 'all',
  level: 'all',
  scope_kind: 'all',
  user_id: '',
}

export function DistributionRiskPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
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
  const [draftFilters, setDraftFilters] =
    useState<DistributionRiskEventDraftFilters>(DEFAULT_FILTERS)
  const [appliedFilters, setAppliedFilters] =
    useState<DistributionRiskEventDraftFilters>(DEFAULT_FILTERS)
  const [pagination, setPagination] = useState({
    pageIndex: 0,
    pageSize: PAGE_SIZE,
  })
  const [eventID, setEventID] = useState<number | null>(null)
  const isSuperAdmin = user?.role === ROLE.SUPER_ADMIN

  const invalidDateRange =
    draftFilters.start_timestamp !== undefined &&
    draftFilters.end_timestamp !== undefined &&
    draftFilters.start_timestamp > draftFilters.end_timestamp
  const invalidUserID =
    appliedFilters.user_id !== '' &&
    (!/^\d+$/.test(appliedFilters.user_id) ||
      !Number.isSafeInteger(Number(appliedFilters.user_id)) ||
      Number(appliedFilters.user_id) <= 0)
  const invalidDraftUserID =
    draftFilters.user_id !== '' &&
    (!/^\d+$/.test(draftFilters.user_id) ||
      !Number.isSafeInteger(Number(draftFilters.user_id)) ||
      Number(draftFilters.user_id) <= 0)

  const queryFilters = useMemo<DistributionRiskEventQuery>(() => {
    const filters: DistributionRiskEventQuery = {
      status: appliedFilters.status,
      level: appliedFilters.level,
      scope_kind: appliedFilters.scope_kind,
    }
    if (appliedFilters.user_id) filters.user_id = Number(appliedFilters.user_id)
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
    queryFn: () =>
      listDistributionRiskEvents(
        queryFilters,
        pagination.pageIndex + 1,
        pagination.pageSize
      ),
    enabled: canRead && !invalidUserID,
    refetchInterval: 60_000,
  })

  const openDetails = useCallback((id: number) => setEventID(id), [])
  const columns = useDistributionRiskEventColumns(openDetails)
  const { table } = useDataTable({
    columns,
    data: eventsQuery.data?.items ?? EMPTY_EVENTS,
    getRowId: (event) => String(event.id),
    totalCount: eventsQuery.data?.total ?? 0,
    pagination,
    onPaginationChange: (updater) => {
      if (eventsQuery.isFetching || eventsQuery.isError) return
      setPagination((current) =>
        typeof updater === 'function' ? updater(current) : updater
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
    if (invalidDateRange || invalidDraftUserID) return
    setAppliedFilters({ ...draftFilters })
    setPagination((current) => ({ ...current, pageIndex: 0 }))
  }

  const resetFilters = () => {
    setDraftFilters(DEFAULT_FILTERS)
    setAppliedFilters(DEFAULT_FILTERS)
    setPagination((current) => ({ ...current, pageIndex: 0 }))
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Risk logs')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {(eventsQuery.data?.pending_count ?? 0) > 0 && (
          <Badge variant='warning'>
            {t('Pending: {{count}}', {
              count: eventsQuery.data?.pending_count ?? 0,
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
        <div className='space-y-3'>
          {canRead && (eventsQuery.data?.pending_count ?? 0) > 0 && (
            <div
              className='border-warning/40 bg-warning/10 flex flex-wrap items-center justify-between gap-3 rounded-md border px-3 py-2.5'
              role='status'
            >
              <p className='text-sm'>
                {t(
                  '{{count}} distribution risk events are waiting for review.',
                  { count: eventsQuery.data?.pending_count ?? 0 }
                )}
              </p>
              <Button
                size='sm'
                variant='outline'
                onClick={() => {
                  const next = {
                    ...DEFAULT_FILTERS,
                    status: 'pending' as const,
                  }
                  setDraftFilters(next)
                  setAppliedFilters(next)
                  setPagination((current) => ({ ...current, pageIndex: 0 }))
                }}
              >
                {t('Review pending')}
              </Button>
            </div>
          )}

          {invalidDateRange && (
            <Alert variant='destructive'>
              <AlertDescription>
                {t('End time must be after start time')}
              </AlertDescription>
            </Alert>
          )}
          {invalidDraftUserID && (
            <Alert variant='destructive'>
              <AlertDescription>{t('Enter a valid user ID')}</AlertDescription>
            </Alert>
          )}
          {eventsQuery.isError && (
            <Alert variant='destructive'>
              <AlertDescription className='flex items-center justify-between gap-3'>
                <span>
                  {eventsQuery.error.message || t('Failed to load risk events')}
                </span>
                <Button
                  type='button'
                  size='sm'
                  variant='outline'
                  onClick={() => void eventsQuery.refetch()}
                >
                  {t('Retry')}
                </Button>
              </AlertDescription>
            </Alert>
          )}

          {canRead ? (
            <DataTablePage
              table={table}
              columns={columns}
              isLoading={eventsQuery.isPending && !invalidUserID}
              isFetching={eventsQuery.isFetching}
              emptyTitle={
                eventsQuery.isError
                  ? t('Failed to load risk events')
                  : t('No risk events')
              }
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
                        className={eventsQuery.isFetching ? 'animate-spin' : ''}
                      />
                    </Button>
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
            void queryClient.invalidateQueries({
              queryKey: ['distribution-risk', 'events'],
            })
          }}
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
