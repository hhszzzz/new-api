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
import { Cancel01Icon, InformationCircleIcon } from '@hugeicons/core-free-icons'
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
import { benchStatusLabelKey } from '../lib/bench'
import { getIqTone, IQ_TEXT_CLASSES } from '../lib/model-radar'
import type { ModelRadarBenchCell, ModelRadarBenchDetailMetric } from '../types'

// Metric keys double as the English i18n source strings.
const DETAIL_METRICS = [
  ['total_tokens', 'Mean total tokens'],
  ['input_tokens', 'Mean input tokens'],
  ['output_tokens', 'Mean output tokens'],
  ['cache_tokens', 'Mean cached input tokens'],
  ['reasoning_tokens', 'Mean reasoning tokens'],
  ['cache_write_tokens', 'Mean cache-write tokens'],
  ['turn_count', 'Mean completed Codex turns'],
] as const

function cacheHitRate(cell: ModelRadarBenchCell): number | null {
  const detail = cell.detail_metrics
  if (!detail || detail.metrics_state !== 'available') return null
  const input = detail.metrics.input_tokens
  const cache = detail.metrics.cache_tokens
  if (
    !input ||
    !cache ||
    input.mean === null ||
    cache.mean === null ||
    input.known_samples !== detail.valid_grade_events ||
    cache.known_samples !== detail.valid_grade_events ||
    input.mean <= 0 ||
    cache.mean > input.mean
  ) {
    return null
  }
  return cache.mean / input.mean
}

export function BenchCellDetails(props: {
  cell: ModelRadarBenchCell | null
  displayName: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const cell = props.cell
  if (!cell) return null
  const detail = cell.detail_metrics
  const scoreTone =
    cell.score === null
      ? 'text-muted-foreground'
      : IQ_TEXT_CLASSES[getIqTone((cell.score / 150) * 100)]
  const metricsAvailable =
    detail !== null &&
    detail.metrics_state === 'available' &&
    Object.keys(detail.metrics).length > 0
  const hitRate = cacheHitRate(cell)

  const sampleNote = (metric: ModelRadarBenchDetailMetric) =>
    t('Known samples {{known}}/{{total}}', {
      known: format.integer(metric.known_samples),
      total: format.integer(metric.total_samples),
    })

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
              {t(benchStatusLabelKey(cell.score_status))}
            </Badge>
          </DialogTitle>
          <DialogDescription>
            {t(
              'Public RadarBench score, grading activity and observed metrics for this tier.'
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
              {t('RadarBench score')}
            </p>
            <p
              className={cn(
                'mt-0.5 text-lg leading-tight font-semibold tabular-nums',
                scoreTone
              )}
            >
              {cell.score === null ? '—' : cell.score.toFixed(1)}
            </p>
          </div>
          <div className='bg-muted/30 rounded-lg border px-3 py-2.5'>
            <p className='text-muted-foreground text-[11px]'>
              {t('Task coverage')}
            </p>
            <p className='mt-0.5 text-lg leading-tight font-semibold tabular-nums'>
              {format.integer(cell.coverage)}/
              {format.integer(cell.required_tasks)}
            </p>
          </div>
          <div className='bg-muted/30 rounded-lg border px-3 py-2.5'>
            <p className='text-muted-foreground text-[11px]'>
              {t('Current scoring samples')}
            </p>
            <p className='mt-0.5 text-lg leading-tight font-semibold tabular-nums'>
              {format.integer(cell.graded_runs_count) ?? '—'}
            </p>
          </div>
        </div>

        {detail ? (
          <section aria-label={t('Grading activity')}>
            <h3 className='mb-2 text-xs font-semibold'>
              {t('Grading activity')}
            </h3>
            <dl className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
              {[
                [
                  t('Valid grading events'),
                  format.integer(detail.valid_grade_events) ?? '—',
                ],
                [
                  t('Formal grades in 24h'),
                  format.integer(detail.graded_24h) ?? '—',
                ],
                [
                  t('Formal grades in 48h'),
                  format.integer(detail.graded_48h) ?? '—',
                ],
                [
                  t('Latest valid grade'),
                  format.dateTime(detail.latest_graded_at) ?? '—',
                ],
                [t('Read as of'), format.dateTime(detail.as_of) ?? '—'],
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
        ) : (
          <p className='text-muted-foreground text-xs'>
            {t(
              'This snapshot was published before grading details were added.'
            )}
          </p>
        )}

        <section aria-label={t('Observed metrics')}>
          <h3 className='mb-2 text-xs font-semibold'>
            {t('Observed metrics')}
          </h3>
          <dl className='overflow-hidden rounded-lg border'>
            <DetailRow
              label={t('Mean API equivalent')}
              value={format.usd(cell.price.mean) ?? '—'}
              note={t('Known samples {{known}}/{{total}}', {
                known: format.integer(cell.price.known_samples) ?? '—',
                total: format.integer(cell.price.total_samples) ?? '—',
              })}
            />
            <DetailRow
              label={t('Mean duration')}
              value={`${format.decimal(cell.duration.mean) ?? '—'} ${t('min')}`}
              note={t('Known samples {{known}}/{{total}}', {
                known: format.integer(cell.duration.known_samples) ?? '—',
                total: format.integer(cell.duration.total_samples) ?? '—',
              })}
            />
          </dl>
        </section>

        {metricsAvailable ? (
          <section aria-label={t('Token usage')}>
            <h3 className='mb-2 text-xs font-semibold'>{t('Token usage')}</h3>
            <dl className='overflow-hidden rounded-lg border'>
              {DETAIL_METRICS.map(([key, label]) => {
                const metric = detail.metrics[key]
                if (!metric) return null
                return (
                  <DetailRow
                    key={key}
                    label={t(label)}
                    value={format.compact(metric.mean) ?? '—'}
                    note={sampleNote(metric)}
                  />
                )
              })}
              {hitRate === null ? null : (
                <DetailRow
                  label={t('Cache hit rate')}
                  value={format.percent(hitRate) ?? '—'}
                  note={t('Cached input tokens divided by input tokens')}
                />
              )}
            </dl>
          </section>
        ) : null}

        <div className='bg-muted/40 flex items-start gap-2 rounded-lg p-3'>
          <HugeiconsIcon
            icon={InformationCircleIcon}
            className='text-muted-foreground mt-0.5 size-4 shrink-0'
            strokeWidth={2}
            aria-hidden='true'
          />
          <div className='text-muted-foreground text-xs leading-relaxed'>
            <p>
              {t(
                'The score uses the latest three valid grades per task, equally weighted over covered tasks.'
              )}
            </p>
            <p className='mt-1'>
              {t(
                'Token and cost metrics cover all retained valid grades of this task set and are marked partial when a sample lacks the measurement.'
              )}
            </p>
            {cell.scoring_policy_version ? (
              <p className='mt-1 break-all'>
                {t('Scoring policy')}: {cell.scoring_policy_version}
              </p>
            ) : null}
          </div>
        </div>
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
