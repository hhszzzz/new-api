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
import type { Table } from '@tanstack/react-table'
import { Database } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'

import { getAccountPoolSecondaryWindowLabel } from '../lib/quota'
import type { AccountPoolAccount } from '../types'
import { AccountStatusBadge } from './account-status-badge'
import { QuotaWindow } from './quota-window'

type AccountPoolMobileListProps = {
  table: Table<AccountPoolAccount>
  isLoading: boolean
  now: number
}

export function AccountPoolMobileList(props: AccountPoolMobileListProps) {
  const { t } = useTranslation()
  if (props.isLoading) {
    return (
      <div className='space-y-2'>
        {[1, 2, 3].map((item) => (
          <div key={item} className='space-y-4 rounded-xl border p-4'>
            <div className='flex items-center justify-between gap-3'>
              <Skeleton className='h-5 w-28' />
              <Skeleton className='h-5 w-20 rounded-full' />
            </div>
            <Skeleton className='h-14 w-full' />
            <Skeleton className='h-14 w-full' />
          </div>
        ))}
      </div>
    )
  }

  const rows = props.table.getRowModel().rows
  if (rows.length === 0) {
    return (
      <div className='rounded-xl border p-6'>
        <Empty className='border-none p-0'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <Database className='size-6' />
            </EmptyMedia>
            <EmptyTitle>{t('No accounts in the pool')}</EmptyTitle>
            <EmptyDescription>
              {t('Codex accounts will appear after the first successful sync.')}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    )
  }

  return (
    <div className='space-y-2'>
      {rows.map((row) => {
        const account = row.original
        return (
          <article
            key={account.public_id}
            className='bg-card space-y-4 rounded-xl border p-4 shadow-xs'
          >
            <header className='flex min-w-0 items-start justify-between gap-3'>
              <div className='min-w-0'>
                <div className='truncate text-sm font-semibold'>
                  {account.display_name}
                </div>
                {account.email ? (
                  <div className='text-muted-foreground truncate text-xs'>
                    {account.email}
                  </div>
                ) : null}
                {account.stale ? (
                  <div className='text-warning text-xs'>{t('Stale row')}</div>
                ) : null}
              </div>
              <AccountStatusBadge status={account.status} />
            </header>

            <div className='text-xs'>
              <div>
                <div className='text-muted-foreground'>{t('Plan')}</div>
                <Badge variant='secondary' className='mt-1 capitalize'>
                  {account.plan || t('Unknown')}
                </Badge>
              </div>
            </div>

            <div className='space-y-4 border-t pt-4'>
              {account.primary_window || !account.secondary_window ? (
                <QuotaWindow
                  window={account.primary_window}
                  now={props.now}
                  label={t('5-hour quota')}
                  compact
                />
              ) : null}
              {account.secondary_window ? (
                <QuotaWindow
                  window={account.secondary_window}
                  now={props.now}
                  label={t(
                    getAccountPoolSecondaryWindowLabel(account.secondary_window)
                  )}
                  compact
                />
              ) : null}
            </div>
          </article>
        )
      })}
    </div>
  )
}
