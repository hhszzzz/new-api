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
  CrownIcon,
  DollarSignIcon,
  GaugeIcon,
  GridViewIcon,
  HierarchyIcon,
  InformationCircleIcon,
  LayerIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

import { useRadarFormatters } from '../hooks/use-radar-formatters'
import {
  EFFORT_ORDER,
  compareModelsByBestIq,
  getIqTone,
  getModelIconKey,
  getPassRate,
  groupConfigurations,
} from '../lib/model-radar'
import type { ModelRadarConfiguration } from '../types'

const CELL_TONE_CLASSES = {
  high: 'border-emerald-500/25 bg-emerald-500/10 hover:bg-emerald-500/20',
  mid: 'border-amber-500/25 bg-amber-500/10 hover:bg-amber-500/20',
  low: 'border-destructive/25 bg-destructive/10 hover:bg-destructive/20',
} as const

const CELL_TEXT_CLASSES = {
  high: 'text-emerald-700 dark:text-emerald-300',
  mid: 'text-amber-700 dark:text-amber-300',
  low: 'text-destructive',
} as const

// Vendor icon for a radar model, falling back to its group color dot.
// Matches the model-square rendering: mono vendor icon at size 20.
export function ModelBadge(props: { color: string; model: string }) {
  const iconKey = getModelIconKey(props.model)
  if (iconKey) {
    return (
      <span className='flex size-5 shrink-0 items-center justify-center overflow-hidden'>
        {getLobeIcon(iconKey, 20)}
      </span>
    )
  }
  return (
    <span
      className='size-2.5 shrink-0 rounded-full'
      style={{ backgroundColor: props.color }}
      aria-hidden='true'
    />
  )
}

export function CapabilityMatrix(props: {
  configurations: ModelRadarConfiguration[]
}) {
  const { t } = useTranslation()
  const [selected, setSelected] = useState<ModelRadarConfiguration | null>(null)
  const groups = groupConfigurations(props.configurations).sort(
    compareModelsByBestIq
  )
  const topGroup = groups[0]

  return (
    <section
      aria-labelledby='capability-matrix-title'
      className='border-border/70 mt-2 border-t pt-6'
    >
      <header className='mb-4'>
        <div className='flex items-center gap-2'>
          <HugeiconsIcon
            icon={GridViewIcon}
            className='text-primary size-4'
            strokeWidth={2}
            aria-hidden='true'
          />
          <h2 id='capability-matrix-title' className='text-base font-semibold'>
            {t('Capability matrix')}
          </h2>
        </div>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t('Compare IQ across every model and reasoning effort.')}
        </p>
      </header>

      <div className='bg-card overflow-x-auto rounded-xl border'>
        <table className='w-full min-w-xl table-fixed border-collapse'>
          <thead>
            <tr className='border-border/70 border-b'>
              <th className='text-muted-foreground bg-card sticky left-0 w-44 p-2.5 text-left text-xs font-medium sm:p-3'>
                {t('Model')}
              </th>
              {EFFORT_ORDER.map((effort) => (
                <th
                  key={effort}
                  className='text-muted-foreground p-2.5 text-center text-xs font-medium capitalize sm:p-3'
                >
                  {effort}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {groups.map((group) => {
              const isTopModel = group === topGroup
              const bestIq = Math.max(
                ...group.configurations.map((item) => item.iq)
              )
              return (
                <tr
                  key={group.model}
                  className={cn(
                    'border-border/50 hover:bg-muted/30 border-b transition-colors last:border-b-0',
                    isTopModel && 'bg-primary/[0.03]'
                  )}
                >
                  <th
                    scope='row'
                    className='sticky left-0 bg-inherit p-2.5 text-left sm:p-3'
                  >
                    <div className='flex min-w-0 items-center gap-2'>
                      <ModelBadge color={group.color} model={group.model} />
                      <span className='max-w-40 truncate text-sm font-semibold'>
                        {group.model}
                      </span>
                      {isTopModel ? (
                        <HugeiconsIcon
                          icon={CrownIcon}
                          className='text-primary size-3.5 shrink-0'
                          strokeWidth={2}
                          aria-label={t('IQ max')}
                        />
                      ) : null}
                    </div>
                  </th>
                  {EFFORT_ORDER.map((effort) => {
                    const configuration = group.configurations.find(
                      (item) => item.effort.toLowerCase() === effort
                    )
                    return (
                      <td key={effort} className='p-1.5 sm:p-2'>
                        {configuration ? (
                          <MatrixCell
                            configuration={configuration}
                            isBest={configuration.iq === bestIq}
                            onSelect={setSelected}
                          />
                        ) : (
                          <span
                            className='text-muted-foreground/40 block py-3 text-center text-xs'
                            aria-hidden='true'
                          >
                            —
                          </span>
                        )}
                      </td>
                    )
                  })}
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      <ConfigurationDetails
        configuration={selected}
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
      />
    </section>
  )
}

function MatrixCell(props: {
  configuration: ModelRadarConfiguration
  isBest: boolean
  onSelect: (configuration: ModelRadarConfiguration) => void
}) {
  const { t } = useTranslation()
  const configuration = props.configuration
  const tone = getIqTone(configuration.iq)

  return (
    <button
      type='button'
      className={cn(
        'focus-visible:ring-ring relative flex w-full min-w-0 flex-col items-center gap-0.5 rounded-md border px-2 py-2 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-offset-1',
        CELL_TONE_CLASSES[tone],
        props.isBest && 'ring-primary/50 ring-1 ring-inset'
      )}
      aria-label={t('View details for {{model}} {{effort}}', {
        model: configuration.model,
        effort: configuration.effort,
      })}
      onClick={() => props.onSelect(configuration)}
    >
      {props.isBest ? (
        <HugeiconsIcon
          icon={CrownIcon}
          className='text-primary absolute top-1 right-1 size-2.5'
          strokeWidth={2.5}
          aria-hidden='true'
        />
      ) : null}
      <span
        className={cn(
          'text-base leading-tight font-bold tabular-nums',
          CELL_TEXT_CLASSES[tone]
        )}
      >
        {configuration.iq.toFixed(1)}
      </span>
      <span className='text-muted-foreground text-[10px] tabular-nums'>
        {configuration.passed}/{configuration.valid_tasks}
      </span>
    </button>
  )
}

function ConfigurationDetails(props: {
  configuration: ModelRadarConfiguration | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const configuration = props.configuration
  if (!configuration) return null

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
          <DialogTitle className='break-words'>
            {configuration.model}{' '}
            <span className='text-muted-foreground ml-2 text-sm font-normal capitalize'>
              {configuration.effort}
            </span>
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

        <div className='grid grid-cols-2 gap-2 sm:grid-cols-4'>
          <div className='bg-muted/30 rounded-lg border px-3 py-2.5'>
            <p className='text-muted-foreground text-[11px]'>{t('IQ')}</p>
            <p className='mt-0.5 text-lg leading-tight font-semibold tabular-nums'>
              {format.decimal(configuration.iq)}
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
              {t('Passed / valid tasks')}
            </p>
            <p className='mt-0.5 text-lg leading-tight font-semibold tabular-nums'>
              {format.integer(configuration.passed)} /{' '}
              {format.integer(configuration.valid_tasks)}
            </p>
          </div>
          <div className='bg-muted/30 rounded-lg border px-3 py-2.5'>
            <p className='text-muted-foreground text-[11px]'>
              {t('Total runs')}
            </p>
            <p className='mt-0.5 text-lg leading-tight font-semibold tabular-nums'>
              {format.integer(configuration.total_runs) ?? t('Not available')}
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
              </div>
            ))}
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
              'IQ is the latest valid pass rate per task multiplied by 150. The combined cost index is provided by the source after normalizing weighted price and duration to 100.'
            )}
          </p>
        </div>
      </DialogContent>
    </Dialog>
  )
}
