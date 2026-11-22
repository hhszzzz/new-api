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
import { Eye } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { TruncatedCell } from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import dayjs from '@/lib/dayjs'

import type { DistributionRiskEvent } from '../types'

export function useDistributionRiskEventColumns(
  onDetails: (eventId: number) => void
): ColumnDef<DistributionRiskEvent>[] {
  const { t } = useTranslation()

  return useMemo(
    () => [
      {
        accessorKey: 'id',
        header: t('Risk event ID'),
        size: 110,
        cell: ({ row }) => (
          <span className='font-mono tabular-nums'>#{row.original.id}</span>
        ),
        meta: { label: t('Risk event ID') },
      },
      {
        accessorKey: 'created_at',
        header: t('Observed at'),
        size: 180,
        cell: ({ row }) => (
          <span className='font-mono tabular-nums'>
            {dayjs.unix(row.original.created_at).format('YYYY-MM-DD HH:mm:ss')}
          </span>
        ),
        meta: { label: t('Observed at'), mobileTitle: true },
      },
      {
        accessorKey: 'user_id',
        header: t('User ID'),
        size: 90,
        cell: ({ row }) => (
          <span className='font-mono tabular-nums'>{row.original.user_id}</span>
        ),
        meta: { label: t('User ID') },
      },
      {
        id: 'scope',
        header: t('Scope'),
        size: 145,
        accessorFn: (event) => `${event.scope_kind}:${event.scope_id}`,
        cell: ({ row }) =>
          `${row.original.scope_kind === 'user' ? t('User account') : t('Token')} ${row.original.scope_id}`,
        meta: { label: t('Scope') },
      },
      {
        id: 'tokens',
        header: t('Contributing tokens'),
        size: 180,
        accessorFn: (event) => event.token_ids.join(', '),
        cell: ({ getValue }) => (
          <TruncatedCell className='max-w-44 font-mono'>
            {getValue<string>() || '—'}
          </TruncatedCell>
        ),
        meta: { label: t('Contributing tokens'), mobileHidden: true },
      },
      {
        accessorKey: 'score',
        header: t('Risk score'),
        size: 90,
        cell: ({ row }) => (
          <span className='font-semibold tabular-nums'>
            {row.original.score}
          </span>
        ),
        meta: { label: t('Risk score') },
      },
      {
        accessorKey: 'level',
        header: t('Risk level'),
        size: 100,
        cell: ({ row }) => (
          <Badge
            variant={row.original.level === 'high' ? 'destructive' : 'warning'}
          >
            {row.original.level === 'high' ? t('High') : t('Medium')}
          </Badge>
        ),
        meta: { label: t('Risk level'), mobileBadge: true },
      },
      {
        accessorKey: 'status',
        header: t('Status'),
        size: 120,
        cell: ({ row }) => {
          const status = (
            {
              confirmed: { label: t('Confirmed'), variant: 'destructive' },
              false_positive: {
                label: t('False positive'),
                variant: 'secondary',
              },
              pending: { label: t('Pending'), variant: 'warning' },
            } as const
          )[row.original.status]
          return <Badge variant={status.variant}>{status.label}</Badge>
        },
        meta: { label: t('Status'), mobileBadge: true },
      },
      {
        accessorKey: 'review_reason',
        header: t('Review reason'),
        size: 220,
        cell: ({ row }) => (
          <TruncatedCell className='max-w-56'>
            {row.original.review_reason || '—'}
          </TruncatedCell>
        ),
        meta: { label: t('Review reason'), mobileHidden: true },
      },
      {
        id: 'details',
        header: t('Details'),
        size: 72,
        enableHiding: false,
        cell: ({ row }) => (
          <Button
            type='button'
            size='icon-sm'
            variant='ghost'
            aria-label={`${t('Details')} #${row.original.id}`}
            title={t('Details')}
            onClick={() => onDetails(row.original.id)}
          >
            <Eye />
          </Button>
        ),
        meta: { label: t('Details') },
      },
    ],
    [onDetails, t]
  )
}
