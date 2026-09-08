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
  ChartHistogramIcon,
  GaugeIcon,
  HeartPulseIcon,
  TextFirstlineLeftIcon,
  Tick02Icon,
  Timer01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import {
  formatLatency,
  formatThroughput,
  formatUptimePct,
} from '@/features/performance-metrics/lib/format'

import { normalizeStatusTimeline } from '../lib/model-status'
import type { ModelStatusModel } from '../status-types'
import { StatusTimeline } from './status-timeline'

type ModelStatusDetailsProps = {
  model: ModelStatusModel
  generatedAt: number
  hourFormatter: Intl.DateTimeFormat
  numberFormatter: Intl.NumberFormat
}

export function ModelStatusDetails(props: ModelStatusDetailsProps) {
  const { t } = useTranslation()
  const timeline = normalizeStatusTimeline(
    props.model.timeline,
    props.generatedAt
  )

  return (
    <section
      className='flex min-w-0 flex-col gap-4'
      aria-label={t('Performance metrics for the last 24 hours')}
    >
      <dl className='border-border/60 bg-muted/25 grid grid-cols-2 gap-px overflow-hidden rounded-lg border sm:grid-cols-3 lg:grid-cols-6'>
        <Metric
          icon={ChartHistogramIcon}
          label={t('Requests')}
          value={
            props.model.request_count === null
              ? '—'
              : props.numberFormatter.format(props.model.request_count)
          }
        />
        <Metric
          icon={Tick02Icon}
          label={t('Successful requests')}
          value={
            props.model.success_count === null
              ? '—'
              : props.numberFormatter.format(props.model.success_count)
          }
        />
        <Metric
          icon={HeartPulseIcon}
          label={t('Success rate')}
          value={
            props.model.success_rate === null
              ? '—'
              : formatUptimePct(props.model.success_rate)
          }
        />
        <Metric
          icon={TextFirstlineLeftIcon}
          label={t('Average TTFT')}
          value={
            props.model.avg_ttft_ms === null
              ? '—'
              : formatLatency(props.model.avg_ttft_ms)
          }
        />
        <Metric
          icon={Timer01Icon}
          label={t('Average latency')}
          value={
            props.model.avg_latency_ms === null
              ? '—'
              : formatLatency(props.model.avg_latency_ms)
          }
        />
        <Metric
          icon={GaugeIcon}
          label={t('Throughput')}
          value={
            props.model.avg_tps === null
              ? '—'
              : formatThroughput(props.model.avg_tps)
          }
        />
      </dl>

      <StatusTimeline
        timeline={timeline}
        hourFormatter={props.hourFormatter}
        numberFormatter={props.numberFormatter}
      />
    </section>
  )
}

function Metric(props: {
  icon: React.ComponentProps<typeof HugeiconsIcon>['icon']
  label: string
  value: string
}) {
  return (
    <div className='min-w-0 px-1 py-2 text-center sm:px-2'>
      <dt className='text-muted-foreground flex min-w-0 items-center justify-center gap-1 text-[9px] font-medium sm:text-[10px]'>
        <HugeiconsIcon
          icon={props.icon}
          className='size-3 shrink-0'
          strokeWidth={2}
          aria-hidden='true'
        />
        <span className='truncate'>{props.label}</span>
      </dt>
      <dd className='mt-1 truncate font-mono text-[10px] font-semibold tabular-nums sm:text-xs'>
        {props.value}
      </dd>
    </div>
  )
}
