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

import { distributionRiskTokenLabel } from '../lib/format'
import type { DistributionRiskEvent } from '../types'

export function useDistributionRiskEventColumns(
  onDetails: (eventId: number) => void
): ColumnDef<DistributionRiskEvent>[] {
  const { t } = useTranslation()

  return useMemo(
    () => [
      {
        accessorKey: 'created_at',
        header: t('Observed at'),
        size: 150,
        cell: ({ row }) => (
          <span className='font-mono tabular-nums'>
            {dayjs.unix(row.original.created_at).format('YYYY-MM-DD HH:mm:ss')}
          </span>
        ),
        meta: { label: t('Observed at'), mobileTitle: true },
      },
      {
        accessorKey: 'username',
        header: t('User'),
        size: 100,
        cell: ({ row }) => (
          <TruncatedCell className='max-w-24'>
            {row.original.username || '—'}
          </TruncatedCell>
        ),
        meta: { label: t('User') },
      },
      {
        id: 'token',
        header: t('Token'),
        size: 105,
        accessorFn: (event) =>
          event.tokens
            .map((token) => distributionRiskTokenLabel(token))
            .join(', '),
        cell: ({ getValue }) => (
          <TruncatedCell className='max-w-28 font-mono'>
            {getValue<string>() || '—'}
          </TruncatedCell>
        ),
        meta: { label: t('Token'), mobileHidden: true },
      },
      {
        id: 'risk',
        header: t('Risk'),
        size: 150,
        accessorFn: (event) => `${event.score} ${event.level}`,
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

          return (
            <div className='flex flex-col items-start gap-1'>
              <div className='flex items-center gap-1.5'>
                <span className='font-semibold tabular-nums'>
                  {row.original.score}
                </span>
                <Badge
                  variant={
                    row.original.level === 'high' ? 'destructive' : 'warning'
                  }
                >
                  {row.original.level === 'high' ? t('High') : t('Medium')}
                </Badge>
              </div>
              <Badge variant={status.variant}>{status.label}</Badge>
            </div>
          )
        },
        meta: { label: t('Risk'), mobileBadge: true },
      },
      {
        id: 'details',
        header: t('Details'),
        size: 56,
        enableHiding: false,
        cell: ({ row }) => (
          <Button
            type='button'
            size='icon-sm'
            variant='ghost'
            aria-label={t('Details')}
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
