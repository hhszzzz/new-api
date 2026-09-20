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

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import dayjs from '@/lib/dayjs'

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
  canViewFullPrompt: boolean
  onOpen: (eventID: number) => void
}): ColumnDef<PromptAuditEvent>[] {
  const { t } = useTranslation()
  const { canDelete, canViewFullPrompt, onOpen } = options

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
          <div className='min-w-30 font-mono text-xs tabular-nums'>
            <div>
              {dayjs
                .unix(row.original.created_at)
                .format('YYYY-MM-DD HH:mm:ss')}
            </div>
            <div className='text-muted-foreground mt-0.5'>
              #{row.original.id}
            </div>
          </div>
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
              <div className='font-medium'>
                {t('User')} #{event.user_id}
              </div>
              <div className='text-muted-foreground max-w-52 truncate text-xs'>
                {event.group || '—'} ·{' '}
                {event.token_name || `#${event.token_id}`}
              </div>
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
              <div className='max-w-56 truncate font-medium'>
                {event.model || '—'}
              </div>
              <div className='text-muted-foreground max-w-56 truncate text-xs'>
                {event.protocol} ·{' '}
                {event.delivery_status || event.endpoint_id || '—'}
              </div>
              <div className='text-muted-foreground max-w-56 truncate font-mono text-[11px]'>
                {event.request_id || event.prompt_hash}
              </div>
            </div>
          )
        },
        meta: { label: t('Request'), mobileTitle: true },
      },
      {
        id: 'prompt',
        header: t('Prompt'),
        size: 340,
        cell: ({ row }) => {
          const event = row.original
          const canOpenFull = canViewFullPrompt && event.full_prompt_available
          return (
            <div className='max-w-96 min-w-48'>
              <p className='line-clamp-2 break-words whitespace-normal'>
                {event.redacted_preview || '—'}
              </p>
              {canOpenFull && (
                <Badge variant='outline' className='mt-1.5 text-[10px]'>
                  {t('Full prompt')}
                </Badge>
              )}
            </div>
          )
        },
        meta: { label: t('Prompt'), mobileOrder: 2 },
      },
      {
        id: 'actions',
        header: t('Actions'),
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
        meta: { label: t('Actions'), mobileOrder: 4 },
      }
    )

    return columns
  }, [canDelete, canViewFullPrompt, onOpen, t])
}
