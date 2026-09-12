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
import {
  Cancel01Icon,
  CellsIcon,
  Clock03Icon,
  DollarSignIcon,
  GaugeIcon,
  HierarchyIcon,
  InformationCircleIcon,
  LayerIcon,
} from '@hugeicons/core-free-icons'
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
import {
  getPassRate,
  getVendorMeta,
  getHistorySeries,
  resolveRadarModel,
  OTHER_VENDOR,
} from '../lib/model-radar'
import type {
  ModelRadarConfiguration,
  ModelRadarHistoryFrame,
  ModelRadarSettings,
} from '../types'
import { Sparkline } from './sparkline'

export function ConfigurationDetails(props: {
  configuration: ModelRadarConfiguration | null
  history: ModelRadarHistoryFrame[]
  settings?: ModelRadarSettings
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const configuration = props.configuration
  if (!configuration) return null
  const resolved = resolveRadarModel(configuration.model, props.settings)
  const vendor = getVendorMeta(resolved.vendor)

  const passRate = getPassRate(configuration)
  const passPercent = Math.min(100, Math.max(0, passRate * 100))
  let passBarColor = 'bg-destructive'
  if (passRate >= 0.8) {
    passBarColor = 'bg-emerald-500'
  } else if (passRate >= 0.5) {
    passBarColor = 'bg-amber-500'
  }

  const efficiencyMetrics = [
    {
      icon: DollarSignIcon,
      label: t('Average cost'),
      value: format.usd(configuration.average_price_usd),
      samplesLabel: t('Cost samples'),
      samples: format.integer(configuration.price_samples),
      priceBand: configuration.average_price_usd_by_band,
    },
    {
      icon: Clock03Icon,
      label: t('Average duration'),
      value: format.decimal(configuration.average_minutes),
      samplesLabel: t('Duration samples'),
      samples: format.integer(configuration.duration_samples),
      unit: t('minutes'),
    },
    {
      icon: HierarchyIcon,
      label: t('Average Agent steps'),
      value: format.decimal(configuration.average_agent_steps),
      samplesLabel: t('Agent step samples'),
      samples: format.integer(configuration.agent_steps_samples),
    },
    {
      icon: LayerIcon,
      label: t('Average tokens'),
      value: format.compact(configuration.average_total_tokens),
      samplesLabel: t('Token samples'),
      samples: format.integer(configuration.token_samples),
    },
    {
      icon: CellsIcon,
      label: t('Cache hit rate'),
      value: format.percent(configuration.cache_hit_rate),
      samplesLabel: t('Cache samples'),
      samples: format.integer(configuration.cache_token_samples),
    },
    {
      icon: GaugeIcon,
      label: t('Combined cost index'),
      value: format.decimal(configuration.combined_cost_index),
      samplesLabel: null,
      samples: null,
    },
  ]

  const auditMetrics: Array<[string, string | null]> = [
    [t('Total runs'), format.integer(configuration.total_runs)],
    [
      t('Incomplete cost samples'),
      format.integer(configuration.incomplete_cost_samples),
    ],
    [t('Latest graded'), format.dateTime(configuration.latest_graded_at)],
  ]

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        showCloseButton={false}
        className='max-h-[min(88vh,760px)] overflow-y-auto sm:max-w-2xl'
      >
        <DialogHeader className='pr-10'>
          <DialogTitle className='break-words' title={configuration.model}>
            {resolved.displayName}{' '}
            <span className='text-muted-foreground ml-2 text-sm font-normal capitalize'>
              {configuration.effort}
            </span>{' '}
            <Badge variant='secondary' className='ml-2 align-middle'>
              {vendor.key === OTHER_VENDOR ? t('Other') : vendor.label}
            </Badge>
          </DialogTitle>
          <DialogDescription>
            {t(
              'Complete capability and efficiency metrics for this configuration.'
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

        <div className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
          <div className='bg-muted/30 rounded-lg border px-3 py-2.5'>
            <p className='text-muted-foreground text-[11px]'>
              {t('Comprehensive IQ')}
            </p>
            <p className='mt-0.5 text-lg leading-tight font-semibold tabular-nums'>
              {format.decimal(configuration.comprehensive_iq ?? null) ?? '—'}
            </p>
          </div>
          <div className='bg-muted/30 rounded-lg border px-3 py-2.5'>
            <p className='text-muted-foreground text-[11px]'>
              {t('Software engineering IQ')}
            </p>
            <p className='mt-0.5 text-lg leading-tight font-semibold tabular-nums'>
              {format.decimal(configuration.iq)}
            </p>
          </div>
          <div className='bg-muted/30 rounded-lg border px-3 py-2.5'>
            <p className='text-muted-foreground text-[11px]'>
              {t('Visual spatial reasoning IQ')}
            </p>
            <p className='mt-0.5 text-lg leading-tight font-semibold tabular-nums'>
              {format.decimal(configuration.visual_iq ?? null) ?? '—'}
            </p>
          </div>
          <div className='bg-muted/30 rounded-lg border px-3 py-2.5'>
            <p className='text-muted-foreground text-[11px]'>
              {t('Pass rate')}
            </p>
            <p className='mt-0.5 text-lg leading-tight font-semibold tabular-nums'>
              {format.percent(passRate)}
            </p>
          </div>
          <div className='bg-muted/30 rounded-lg border px-3 py-2.5'>
            <p className='text-muted-foreground text-[11px]'>
              {t('Passed / valid samples')}
            </p>
            <p className='mt-0.5 text-lg leading-tight font-semibold tabular-nums'>
              {format.integer(configuration.passed)} /{' '}
              {format.integer(configuration.valid_tasks)}
            </p>
          </div>
          <div className='bg-muted/30 rounded-lg border px-3 py-2.5'>
            <p className='text-muted-foreground text-[11px]'>
              {t('Runs 24h / 48h / total')}
            </p>
            <p className='mt-0.5 text-lg leading-tight font-semibold tabular-nums'>
              {format.integer(configuration.runs_24h ?? null) ?? '—'} /{' '}
              {format.integer(configuration.runs_48h ?? null) ?? '—'} /{' '}
              {format.integer(configuration.total_runs) ?? '—'}
            </p>
          </div>
        </div>

        <div
          role='progressbar'
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(passPercent)}
          aria-label={t('Pass rate')}
        >
          <div className='bg-muted h-1.5 overflow-hidden rounded-full'>
            <div
              className={cn('h-full rounded-full transition-all', passBarColor)}
              style={{ width: `${passPercent}%` }}
            />
          </div>
        </div>

        <section aria-label={t('Efficiency metrics')}>
          <h3 className='mb-2 text-xs font-semibold'>
            {t('Efficiency metrics')}
          </h3>
          <div className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
            {efficiencyMetrics.map((metric) => (
              <div
                key={metric.label}
                className='bg-card min-w-0 rounded-lg border px-3 py-2.5'
              >
                <div className='flex items-center gap-1.5'>
                  <HugeiconsIcon
                    icon={metric.icon}
                    className='text-muted-foreground size-3.5 shrink-0'
                    strokeWidth={2}
                    aria-hidden='true'
                  />
                  <p className='text-muted-foreground truncate text-[11px]'>
                    {metric.label}
                  </p>
                </div>
                <p className='mt-1 truncate text-sm font-semibold tabular-nums'>
                  {metric.value ?? t('Not available')}
                  {metric.unit && metric.value !== null ? (
                    <span className='text-muted-foreground ml-1 text-[10px] font-normal'>
                      {metric.unit}
                    </span>
                  ) : null}
                </p>
                {metric.samples !== null ? (
                  <p className='text-muted-foreground mt-0.5 truncate text-[10px] tabular-nums'>
                    {metric.samplesLabel}: {metric.samples}
                  </p>
                ) : null}
                {metric.priceBand ? (
                  <p className='text-muted-foreground mt-1 text-[10px] tabular-nums'>
                    {t('Off-peak')}{' '}
                    {format.usd(metric.priceBand.off_peak ?? null) ?? '—'} ·{' '}
                    {t('Peak')}{' '}
                    {format.usd(metric.priceBand.peak ?? null) ?? '—'}
                  </p>
                ) : null}
              </div>
            ))}
          </div>
        </section>

        <section aria-label={t('Software engineering IQ trend (72h)')}>
          <h3 className='mb-2 text-xs font-semibold'>
            {t('Software engineering IQ trend (72h)')}
          </h3>
          <div className='bg-muted/20 flex rounded-lg border p-3'>
            <Sparkline
              values={getHistorySeries(
                props.history,
                configuration.model,
                configuration.effort
              )}
              label={`${configuration.model} ${configuration.effort}`}
              windowHours={72}
            />
          </div>
        </section>

        <dl className='grid grid-cols-1 overflow-hidden rounded-lg border sm:grid-cols-3'>
          {auditMetrics.map(([label, value]) => (
            <div
              key={label}
              className='border-border/70 flex items-center justify-between gap-4 border-b px-3 py-2.5 last:border-b-0 sm:border-b-0 sm:odd:border-r sm:[&:nth-child(2)]:border-r'
            >
              <dt className='text-muted-foreground text-xs'>{label}</dt>
              <dd className='text-right text-sm font-medium tabular-nums'>
                {value ?? t('Not available')}
              </dd>
            </div>
          ))}
        </dl>

        <div className='bg-muted/40 flex items-start gap-2 rounded-lg p-3'>
          <HugeiconsIcon
            icon={InformationCircleIcon}
            className='text-muted-foreground mt-0.5 size-4 shrink-0'
            strokeWidth={2}
            aria-hidden='true'
          />
          <p className='text-muted-foreground text-xs leading-relaxed'>
            {t(
              'Comprehensive IQ combines software-engineering and visual-spatial IQ, weighted by valid task count. Configurations without a visual-spatial score show the software-engineering IQ.'
            )}
          </p>
        </div>
      </DialogContent>
    </Dialog>
  )
}
