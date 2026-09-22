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
import { Checkbox } from '@/components/ui/checkbox'
import dayjs from '@/lib/dayjs'

import { getPromptAuditProtocolName } from '../lib'
import type { PromptAuditEvent } from '../types'

function decisionBadgeVariant(decision: string) {
  if (decision === 'block' || decision === 'unavailable') return 'destructive'
  if (decision === 'flag') return 'warning'
  if (decision === 'pass') return 'secondary'
  return 'outline'
}

function statusBadgeVariant(status: string) {
  if (status === 'failed') return 'destructive'
  if (status === 'retry' || status === 'processing') return 'warning'
  if (status === 'done') return 'secondary'
  return 'outline'
}

export function usePromptAuditColumns(options: {
  canDelete: boolean
  onOpen: (eventID: number) => void
}): ColumnDef<PromptAuditEvent>[] {
  const { t } = useTranslation()
  const { canDelete, onOpen } = options

  return useMemo(() => {
    const columns: ColumnDef<PromptAuditEvent>[] = []
    if (canDelete) {
      columns.push({
        id: 'select',
        size: 44,
        enableHiding: false,
        header: ({ table }) => (
          <Checkbox
            checked={table.getIsAllPageRowsSelected()}
            indeterminate={table.getIsSomePageRowsSelected()}
            onCheckedChange={(value) =>
              table.toggleAllPageRowsSelected(Boolean(value))
            }
            aria-label={t('Select current page')}
          />
        ),
        cell: ({ row }) => (
          <Checkbox
            checked={row.getIsSelected()}
            onCheckedChange={(value) => row.toggleSelected(Boolean(value))}
            aria-label={t('Select audit record')}
          />
        ),
        meta: { mobileHidden: true },
      })
    }

    columns.push(
      {
        accessorKey: 'created_at',
        header: t('Time'),
        size: 150,
        cell: ({ row }) => (
          <span className='font-mono text-xs tabular-nums'>
            {dayjs.unix(row.original.created_at).format('YYYY-MM-DD HH:mm:ss')}
          </span>
        ),
        meta: { label: t('Time'), mobileOrder: 3 },
      },
      {
        id: 'result',
        header: t('Result'),
        size: 190,
        cell: ({ row }) => {
          const event = row.original
          return (
            <div className='min-w-0'>
              <div className='flex flex-wrap gap-1'>
                <Badge variant={decisionBadgeVariant(event.decision)}>
                  {t(event.decision || 'pending')}
                </Badge>
                <Badge variant={statusBadgeVariant(event.status)}>
                  {t(event.status)}
                </Badge>
                <Badge variant='outline'>
                  {event.inspection_type === 'wordlist'
                    ? t('Wordlist')
                    : t('Model audit')}
                </Badge>
                <Badge variant='outline'>
                  {event.direction === 'output'
                    ? t('Generated output')
                    : t('Request input')}
                </Badge>
              </div>
              {event.categories.length > 0 && (
                <p className='text-muted-foreground mt-1 max-w-48 truncate text-xs'>
                  {event.categories.map((category) => t(category)).join(', ')}
                </p>
              )}
            </div>
          )
        },
        meta: { label: t('Result'), mobileBadge: true },
      },
      {
        id: 'identity',
        header: t('Identity'),
        size: 190,
        cell: ({ row }) => {
          const event = row.original
          return (
            <div className='min-w-0'>
              <TruncatedCell className='max-w-52 font-medium'>
                {event.username || `#${event.user_id}`}
              </TruncatedCell>
              <TruncatedCell className='text-muted-foreground max-w-52 text-xs'>
                {t('Group')}: {event.group || '—'}
              </TruncatedCell>
              <TruncatedCell className='text-muted-foreground max-w-52 text-xs'>
                {t('Key')}: {event.token_name || `#${event.token_id}`}
              </TruncatedCell>
            </div>
          )
        },
        meta: { label: t('Identity'), mobileOrder: 1 },
      },
      {
        id: 'request',
        header: t('Request'),
        size: 230,
        cell: ({ row }) => {
          const event = row.original
          return (
            <div className='min-w-0'>
              <TruncatedCell className='max-w-56 font-medium'>
                {event.model || '—'}
              </TruncatedCell>
              <TruncatedCell className='text-muted-foreground max-w-56 text-xs'>
                {t('Protocol')}: {getPromptAuditProtocolName(event.protocol)}
              </TruncatedCell>
              <TruncatedCell className='text-muted-foreground max-w-56 text-xs'>
                {t('Audit model')}: {event.endpoint_model || '—'}
              </TruncatedCell>
            </div>
          )
        },
        meta: { label: t('Request'), mobileTitle: true },
      },
      {
        id: 'client',
        header: t('Client'),
        size: 200,
        cell: ({ row }) => {
          const event = row.original
          return (
            <div className='min-w-0'>
              <span className='font-mono text-xs'>{event.ip || '—'}</span>
              <TruncatedCell className='text-muted-foreground max-w-52 text-xs'>
                {event.user_agent || '—'}
              </TruncatedCell>
            </div>
          )
        },
        // Hideable: the reply headers are useful for investigation, but not for
        // every operator scanning the list.
        meta: { label: t('Client'), mobileHidden: true },
      },
      {
        id: 'prompt',
        header: t('Prompt'),
        size: 340,
        cell: ({ row }) => {
          const event = row.original
          return (
            <div className='max-w-96 min-w-48'>
              <p className='line-clamp-2 break-words whitespace-normal'>
                {event.redacted_preview || '—'}
              </p>
            </div>
          )
        },
        meta: { label: t('Prompt'), mobileOrder: 2 },
      },
      {
        id: 'actions',
        header: t('Details'),
        size: 72,
        enableHiding: false,
        cell: ({ row }) => (
          <div className='text-right'>
            <Button
              variant='ghost'
              size='icon-sm'
              aria-label={t('View details')}
              onClick={() => onOpen(row.original.id)}
            >
              <Eye />
            </Button>
          </div>
        ),
        meta: { label: t('Details'), mobileOrder: 4 },
      }
    )

    return columns
  }, [canDelete, onOpen, t])
}
