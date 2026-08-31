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
import type { ColumnDef } from '@tanstack/react-table'
import { RefreshCw } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { cn } from '@/lib/utils'

import { formatAccountPoolCountdown } from '../lib/quota'
import type { AccountPoolAccount, AccountPoolSnapshot } from '../types'
import { AccountPoolMobileList } from './account-pool-mobile-list'
import { AccountStatusBadge } from './account-status-badge'
import { QuotaWindow } from './quota-window'

type AccountPoolTableProps = {
  snapshot?: AccountPoolSnapshot
  isLoading: boolean
  isFetching: boolean
  isRefreshing: boolean
  refreshDisabled: boolean
  refreshLabel: string
  now: number
  onRefresh: () => void
}

type StatusFilter = 'all' | AccountPoolAccount['status']
const EMPTY_ACCOUNTS: AccountPoolAccount[] = []

export function AccountPoolTable(props: AccountPoolTableProps) {
  const { t } = useTranslation()
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const accounts = props.snapshot?.accounts ?? EMPTY_ACCOUNTS
  const filteredAccounts = useMemo(() => {
    if (statusFilter === 'all') return accounts
    return accounts.filter((account) => account.status === statusFilter)
  }, [accounts, statusFilter])
  const statusFilterLabel = {
    all: t('All statuses'),
    available: t('Available'),
    limited: t('Quota exhausted'),
    error: t('Error'),
    unavailable: t('Unavailable'),
    disabled: t('Disabled'),
  }[statusFilter]
  const nextRefreshCountdown = formatAccountPoolCountdown(
    props.snapshot?.next_refresh_at ?? null,
    props.now
  )

  const columns = useMemo<ColumnDef<AccountPoolAccount, unknown>[]>(
    () => [
      {
        id: 'account',
        header: t('Account'),
        size: 190,
        cell: ({ row }) => (
          <div className='min-w-0 py-1'>
            <div className='truncate font-medium'>
              {row.original.display_name}
            </div>
            {row.original.email ? (
              <div className='text-muted-foreground max-w-52 truncate text-xs'>
                {row.original.email}
              </div>
            ) : null}
          </div>
        ),
      },
      {
        accessorKey: 'status',
        header: t('Status'),
        size: 100,
        cell: ({ row }) => <AccountStatusBadge status={row.original.status} />,
      },
      {
        accessorKey: 'plan',
        header: t('Plan'),
        size: 90,
        cell: ({ row }) => (
          <Badge variant='secondary' className='capitalize'>
            {row.original.plan || t('Unknown')}
          </Badge>
        ),
      },
      {
        id: 'primary-window',
        header: t('5-hour quota'),
        size: 220,
        cell: ({ row }) => (
          <QuotaWindow window={row.original.primary_window} now={props.now} />
        ),
      },
      {
        id: 'secondary-window',
        header: t('Weekly / monthly quota'),
        size: 220,
        cell: ({ row }) => (
          <QuotaWindow window={row.original.secondary_window} now={props.now} />
        ),
      },
      {
        accessorKey: 'subscription_active_until',
        header: t('Subscription expires'),
        size: 150,
        cell: ({ row }) => (
          <span className='text-muted-foreground text-xs tabular-nums'>
            {row.original.subscription_active_until
              ? new Date(
                  row.original.subscription_active_until
                ).toLocaleString()
              : t('Unknown')}
          </span>
        ),
      },
    ],
    [props.now, t]
  )

  const { table } = useDataTable({
    data: filteredAccounts,
    columns,
    totalCount: filteredAccounts.length,
    tableStateStorageKey: 'account-pool-table',
    columnSizingStorageKey: 'account-pool-table:v2:column-sizing',
    initialPagination: { pageIndex: 0, pageSize: 10 },
    enableRowSelection: false,
    enableSorting: false,
    getRowId: (row) => row.public_id,
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={props.isLoading}
      isFetching={props.isFetching}
      emptyTitle={t('No accounts in the pool')}
      emptyDescription={t(
        'Codex accounts will appear after the first successful sync.'
      )}
      fixedHeight={false}
      applyHeaderSize
      skeletonKeyPrefix='account-pool'
      tableClassName='[&_[data-slot=table-header]]:sticky [&_[data-slot=table-header]]:top-0 [&_[data-slot=table-header]]:z-10'
      toolbar={
        <div className='bg-card flex flex-wrap items-center justify-between gap-2 rounded-xl border px-3 py-2.5 shadow-xs'>
          <div className='flex flex-wrap items-center gap-2'>
            <Badge variant='outline'>
              {t('{{count}} accounts', {
                count: props.snapshot?.summary.total ?? 0,
              })}
            </Badge>
            <Badge className='border-success/30 bg-success/10 text-success'>
              {t('{{count}} available', {
                count: props.snapshot?.summary.available ?? 0,
              })}
            </Badge>
            <Select
              value={statusFilter}
              onValueChange={(value) => setStatusFilter(value as StatusFilter)}
            >
              <SelectTrigger size='sm' aria-label={t('Filter by status')}>
                <SelectValue>{statusFilterLabel}</SelectValue>
              </SelectTrigger>
              <SelectContent align='start'>
                <SelectItem value='all'>{t('All statuses')}</SelectItem>
                <SelectItem value='available'>{t('Available')}</SelectItem>
                <SelectItem value='limited'>{t('Quota exhausted')}</SelectItem>
                <SelectItem value='error'>{t('Error')}</SelectItem>
                <SelectItem value='unavailable'>{t('Unavailable')}</SelectItem>
                <SelectItem value='disabled'>{t('Disabled')}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className='flex flex-wrap items-center justify-end gap-x-3 gap-y-1'>
            {props.snapshot?.updated_at ? (
              <div className='text-muted-foreground text-xs tabular-nums'>
                <span>{t('Updated')}: </span>
                <time dateTime={props.snapshot.updated_at}>
                  {new Date(props.snapshot.updated_at).toLocaleString()}
                </time>
              </div>
            ) : null}
            {props.snapshot?.next_refresh_at && nextRefreshCountdown ? (
              <time
                dateTime={props.snapshot.next_refresh_at}
                className='text-muted-foreground text-xs tabular-nums'
              >
                {t('Next refresh in {{time}}', {
                  time: nextRefreshCountdown,
                })}
              </time>
            ) : null}
            <Button
              type='button'
              size='sm'
              variant='outline'
              onClick={props.onRefresh}
              disabled={props.refreshDisabled || props.isRefreshing}
              aria-label={props.refreshLabel}
            >
              <RefreshCw
                className={cn('size-4', props.isRefreshing && 'animate-spin')}
                aria-hidden='true'
              />
              {props.refreshLabel}
            </Button>
          </div>
        </div>
      }
      mobile={
        <AccountPoolMobileList
          table={table}
          isLoading={props.isLoading}
          now={props.now}
        />
      }
    />
  )
}
