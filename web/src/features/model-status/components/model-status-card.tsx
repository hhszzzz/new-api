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
  GaugeIcon,
  HeartPulseIcon,
  Timer01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  formatLatency,
  formatThroughput,
  formatUptimePct,
} from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

import { normalizeStatusTimeline } from '../lib/model-status'
import type { ModelStatusModel } from '../types'
import { getModelStatusLabel, STATUS_PRESENTATION } from './status-presentation'
import { StatusTimeline } from './status-timeline'

type ModelStatusCardProps = {
  model: ModelStatusModel
  generatedAt: number
  hourFormatter: Intl.DateTimeFormat
}

export function ModelStatusCard(props: ModelStatusCardProps) {
  const { t } = useTranslation()
  const titleId = useId()
  const presentation = STATUS_PRESENTATION[props.model.status]
  const timeline = normalizeStatusTimeline(
    props.model.timeline,
    props.generatedAt
  )

  return (
    <article className='min-w-0' aria-labelledby={titleId}>
      <Card size='sm'>
        <CardHeader>
          <CardDescription className='text-xs font-medium break-all'>
            {props.model.vendor || '—'}
          </CardDescription>
          <CardTitle>
            <h2 id={titleId} className='font-mono break-all'>
              {props.model.model_name}
            </h2>
          </CardTitle>
          <CardAction>
            <span
              role='img'
              aria-label={getModelStatusLabel(t, props.model.status)}
              className={cn(
                'block size-2.5 rounded-full',
                presentation.dotClassName
              )}
            />
          </CardAction>
        </CardHeader>

        <CardContent className='flex flex-col gap-4'>
          <dl className='grid grid-cols-3 gap-2'>
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

          <div>
            <p className='text-muted-foreground mb-2 text-[11px] font-medium tracking-wide uppercase'>
              {t('Last 24 hours')}
            </p>
            <StatusTimeline
              timeline={timeline}
              hourFormatter={props.hourFormatter}
            />
          </div>
        </CardContent>
      </Card>
    </article>
  )
}

function Metric(props: {
  icon: React.ComponentProps<typeof HugeiconsIcon>['icon']
  label: string
  value: string
}) {
  return (
    <div className='bg-muted/35 min-w-0 rounded-lg px-2.5 py-2.5 sm:px-3'>
      <dt className='text-muted-foreground flex min-w-0 items-center gap-1 text-[10px] font-medium sm:text-[11px]'>
        <HugeiconsIcon
          icon={props.icon}
          className='size-3 shrink-0'
          strokeWidth={2}
          aria-hidden='true'
        />
        <span className='truncate'>{props.label}</span>
      </dt>
      <dd className='mt-1 truncate font-mono text-xs font-semibold tabular-nums sm:text-sm'>
        {props.value}
      </dd>
    </div>
  )
}
