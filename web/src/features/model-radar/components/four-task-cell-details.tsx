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
import { Cancel01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

import { useRadarFormatters } from '../hooks/use-radar-formatters'
import { fourTaskStatusLabelKey } from '../lib/four-task'
import { getIqTone, IQ_TEXT_CLASSES } from '../lib/model-radar'
import type { ModelRadarFourTask, ModelRadarFourTaskCell } from '../types'

export function FourTaskCellDetails(props: {
  cell: ModelRadarFourTaskCell | null
  board: ModelRadarFourTask
  displayName: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const cell = props.cell
  if (!cell) return null
  const board = props.board
  const scoreTone =
    cell.score === null
      ? 'text-muted-foreground'
      : IQ_TEXT_CLASSES[getIqTone((cell.score / 150) * 100)]

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        showCloseButton={false}
        className='max-h-[min(88vh,760px)] overflow-y-auto sm:max-w-2xl'
      >
        <DialogHeader className='pr-10'>
          <DialogTitle className='break-words'>
            {props.displayName}{' '}
            <span className='text-muted-foreground ml-1 text-sm font-normal capitalize'>
              {cell.effort}
            </span>
            <Badge variant='secondary' className='ml-2 align-middle'>
              {t(fourTaskStatusLabelKey(cell))}
            </Badge>
          </DialogTitle>
          <DialogDescription>
            {t(
              'Public four-task score, cost and runtime for this configuration.'
            )}
          </DialogDescription>
        </DialogHeader>
        <DialogClose
          render={
            <Button
              type='button'
              variant='ghost'
              size='icon-sm'
              className='absolute top-2 right-2'
              aria-label={t('Close')}
            />
          }
        >
          <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} />
        </DialogClose>

        <div className='grid grid-cols-3 gap-2'>
          <div className='bg-muted/30 rounded-lg border px-3 py-2.5'>
            <p className='text-muted-foreground text-[11px]'>
              {t('Total score')}
            </p>
            <p
              className={cn(
                'mt-0.5 text-lg leading-tight font-semibold tabular-nums',
                scoreTone
              )}
            >
              {cell.score === null ? '—' : format.decimal(cell.score)}
            </p>
          </div>
          <div className='bg-muted/30 rounded-lg border px-3 py-2.5'>
            <p className='text-muted-foreground text-[11px]'>
              {t('Graded tasks')}
            </p>
            <p className='mt-0.5 text-lg leading-tight font-semibold tabular-nums'>
              {format.integer(cell.graded_tasks)}/
              {format.integer(cell.required_tasks)}
            </p>
          </div>
          <div className='bg-muted/30 rounded-lg border px-3 py-2.5'>
            <p className='text-muted-foreground text-[11px]'>{t('Rank')}</p>
            <p className='mt-0.5 text-lg leading-tight font-semibold tabular-nums'>
              {cell.rank === null ? '—' : `#${format.integer(cell.rank)}`}
            </p>
          </div>
        </div>

        <section aria-label={t('Observed metrics')}>
          <h3 className='mb-2 text-xs font-semibold'>
            {t('Observed metrics')}
          </h3>
          <dl className='overflow-hidden rounded-lg border'>
            <DetailRow
              label={t('Cost (USD)')}
              value={format.usd(cell.cost_usd) ?? '—'}
              note={
                cell.cost_usd === null
                  ? t('Not published by this source')
                  : t('API-equivalent estimate, not a subscription charge')
              }
            />
            <DetailRow
              label={t('Runtime (min)')}
              value={
                cell.duration_minutes === null
                  ? '—'
                  : (format.decimal(cell.duration_minutes) ?? '—')
              }
              note={
                cell.duration_minutes === null
                  ? t('Not published by this source')
                  : t('Total of the four graded tasks')
              }
            />
          </dl>
        </section>

        <section aria-label={t('Round')}>
          <h3 className='mb-2 text-xs font-semibold'>{t('Round')}</h3>
          <dl className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
            {[
              [t('Round'), board.round_id],
              [t('Protocol'), board.protocol],
              [t('Verified at'), format.dateTime(board.verified_at) ?? '—'],
              [
                t('Test window'),
                `${format.dateTime(board.test_started_at) ?? '—'} – ${
                  format.dateTime(board.test_completed_at) ?? '—'
                }`,
              ],
              [
                t('Graded'),
                `${format.integer(board.graded)}/${format.integer(board.expected)}`,
              ],
              [t('Publication'), board.publication_status],
            ].map(([label, value]) => (
              <div
                key={label}
                className='bg-card min-w-0 rounded-lg border px-3 py-2'
              >
                <dt className='text-muted-foreground truncate text-[11px]'>
                  {label}
                </dt>
                <dd className='mt-0.5 truncate text-sm font-semibold tabular-nums'>
                  {value}
                </dd>
              </div>
            ))}
          </dl>
        </section>
      </DialogContent>
    </Dialog>
  )
}

function DetailRow(props: { label: string; value: string; note?: string }) {
  return (
    <div className='border-border/70 flex items-center justify-between gap-4 border-b px-3 py-2 last:border-b-0'>
      <dt className='min-w-0'>
        <span className='block truncate text-xs'>{props.label}</span>
        {props.note ? (
          <span className='text-muted-foreground block truncate text-[10px] tabular-nums'>
            {props.note}
          </span>
        ) : null}
      </dt>
      <dd className='text-right text-sm font-medium tabular-nums'>
        {props.value}
      </dd>
    </div>
  )
}
