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
import { Alert02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { useRadarFormatters } from '../hooks/use-radar-formatters'
import {
  createModelColorMap,
  getHistorySeries,
  getIqTone,
  IQ_TEXT_CLASSES,
  resolveRadarModel,
  type ModelRadarIconRegistry,
} from '../lib/model-radar'
import type {
  ModelRadarConfiguration,
  ModelRadarDegradationAlert,
  ModelRadarHistoryFrame,
  ModelRadarSettings,
} from '../types'
import { ModelBadge } from './model-badge'
import { Sparkline } from './sparkline'

export function DegradationAlerts(props: {
  alerts: ModelRadarDegradationAlert[]
  history: ModelRadarHistoryFrame[]
  configurations: ModelRadarConfiguration[]
  settings?: ModelRadarSettings
  iconRegistry?: ModelRadarIconRegistry
}) {
  const { t } = useTranslation()
  if (props.alerts.length === 0) return null
  const modelColors = createModelColorMap(props.configurations)

  return (
    <section aria-labelledby='degradation-alerts-title' className='pt-6 pb-2'>
      <header className='mb-4'>
        <div className='flex items-center gap-2'>
          <HugeiconsIcon
            icon={Alert02Icon}
            className='size-4 text-amber-600 dark:text-amber-400'
            strokeWidth={2}
            aria-hidden='true'
          />
          <h2 id='degradation-alerts-title' className='text-base font-semibold'>
            {t('Degradation alerts')}
          </h2>
        </div>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t('Configurations whose IQ has declined across recent windows.')}
        </p>
      </header>

      <div className='grid gap-3 lg:grid-cols-2'>
        {props.alerts.map((alert) => (
          <AlertCard
            key={`${alert.model}:${alert.effort}`}
            alert={alert}
            color={modelColors.get(alert.model) ?? '#64748b'}
            series={getHistorySeries(
              props.history,
              alert.model,
              alert.effort,
              48
            )}
            iconRegistry={props.iconRegistry}
            settings={props.settings}
          />
        ))}
      </div>
    </section>
  )
}

function AlertCard(props: {
  alert: ModelRadarDegradationAlert
  color: string
  series: number[]
  settings?: ModelRadarSettings
  iconRegistry?: ModelRadarIconRegistry
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const alert = props.alert
  const resolved = resolveRadarModel(alert.model, props.settings)
  const trend = alert.trend_48h ?? []
  // Upstream's hourly trend is finer than the history frames. History still
  // charts snapshots stored before the trend was synced, and a tier whose
  // trend has only one reading so far.
  const series =
    trend.length >= 2 || (trend.length > 0 && props.series.length < 2)
      ? trend.map((point) => point.iq)
      : props.series
  // Upstream reports no 48-hour window while a tier has less history than
  // that, so there is no baseline to show.
  let startIq: number | null = null
  if (alert.degradation_48h_iq !== null) {
    startIq =
      series.length > 0 ? series[0] : alert.iq + alert.degradation_48h_iq
  }

  return (
    <article
      className='bg-card min-w-0 rounded-xl border p-4'
      aria-label={`${resolved.displayName} ${alert.effort}`}
    >
      <div className='flex min-w-0 items-start justify-between gap-4'>
        <div className='flex min-w-0 items-center gap-2'>
          <ModelBadge
            color={props.color}
            model={alert.model}
            settings={props.settings}
            iconRegistry={props.iconRegistry}
          />
          <div className='min-w-0'>
            <p className='truncate text-sm font-semibold' title={alert.model}>
              {resolved.displayName}
            </p>
            <p className='text-muted-foreground text-[11px] capitalize'>
              {alert.effort}
            </p>
          </div>
        </div>
        <div className='shrink-0 text-right'>
          <p
            className={cn(
              'text-2xl leading-tight font-semibold tabular-nums',
              IQ_TEXT_CLASSES[getIqTone(alert.iq)]
            )}
          >
            {alert.iq.toFixed(1)}
            <span className='text-muted-foreground ml-1 text-[10px] font-normal'>
              IQ
            </span>
          </p>
          <p className='text-muted-foreground text-[11px] tabular-nums'>
            {t('48 hours ago')}{' '}
            {startIq === null
              ? t('Insufficient data')
              : format.decimal(startIq)}
          </p>
        </div>
      </div>

      <div className='mt-3 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between sm:gap-4'>
        <Sparkline
          values={series}
          label={`${resolved.displayName} ${alert.effort}`}
        />
        <dl className='grid shrink-0 grid-cols-3 gap-3 border-t pt-2 sm:border-t-0 sm:pt-0'>
          <Decline label={t('12 hours')} value={alert.degradation_12h_iq} />
          <Decline
            label={t('24 hours')}
            value={alert.degradation_24h_iq}
            average={alert.average_iq_24h ?? null}
          />
          <Decline
            label={t('48 hours')}
            value={alert.degradation_48h_iq}
            average={alert.average_iq_48h ?? null}
          />
        </dl>
      </div>
    </article>
  )
}

function Decline(props: {
  label: string
  value: number | null
  average?: number | null
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  let valueClass = 'text-muted-foreground'
  let displayValue = '0.0'
  if (props.value === null) {
    displayValue = t('Insufficient data')
  } else if (props.value > 0) {
    valueClass = 'text-destructive'
    displayValue = `-${props.value.toFixed(1)}`
  } else if (props.value < 0) {
    valueClass = 'text-emerald-700 dark:text-emerald-400'
    displayValue = `+${Math.abs(props.value).toFixed(1)}`
  }

  return (
    <div className='sm:text-right'>
      <dt className='text-muted-foreground text-[10px]'>{props.label}</dt>
      <dd
        className={cn(
          'mt-0.5 tabular-nums',
          props.value === null
            ? 'text-[10px] font-normal'
            : 'text-xs font-semibold',
          valueClass
        )}
      >
        {displayValue}
      </dd>
      {props.value !== null &&
      props.average !== null &&
      props.average !== undefined ? (
        <dd className='text-muted-foreground text-[10px] tabular-nums'>
          {t('Average {{value}}', { value: format.decimal(props.average) })}
        </dd>
      ) : null}
    </div>
  )
}
