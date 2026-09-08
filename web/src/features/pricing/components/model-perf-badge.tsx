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
import { memo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { StatusTimeline } from '@/features/performance-metrics/components/status-timeline'
import {
  formatLatency,
  formatThroughput,
} from '@/features/performance-metrics/lib/format'
import { normalizeStatusTimeline } from '@/features/performance-metrics/lib/model-status'
import type {
  ModelStatusModel,
  ModelStatusTimelinePoint,
} from '@/features/performance-metrics/status-types'
import { cn } from '@/lib/utils'

export type ModelPerfBadgeData = Pick<
  ModelStatusModel,
  'avg_latency_ms' | 'success_rate' | 'avg_tps'
> & {
  timeline?: ModelStatusTimelinePoint[]
}

export interface ModelPerfBadgeProps extends React.HTMLAttributes<HTMLDivElement> {
  perf: ModelPerfBadgeData | undefined
  generatedAt?: number
  onOpenPerformance?: () => void
}

export const ModelPerfBadge = memo(function ModelPerfBadge(
  props: ModelPerfBadgeProps
) {
  const { t } = useTranslation()
  const [initialTimestamp] = useState(() => Date.now() / 1000)
  const latencyText = formatLatency(props.perf?.avg_latency_ms ?? 0)
  const throughputText = formatThroughput(props.perf?.avg_tps ?? 0).replace(
    ' t/s',
    't/s'
  )
  const successRate = props.perf?.success_rate
  const hasSuccessRate =
    successRate != null &&
    Number.isFinite(successRate) &&
    successRate >= 0 &&
    successRate <= 100
  const timeline = normalizeStatusTimeline(
    props.perf?.timeline ?? [],
    props.generatedAt ?? initialTimestamp
  )

  return (
    <div
      aria-label={t('Performance metrics for the last 24 hours')}
      className={cn(
        'flex w-full min-w-0 items-center justify-between gap-3',
        props.className
      )}
    >
      <dl className='flex min-w-0 items-start gap-5 text-xs tabular-nums'>
        <div className='w-24 shrink-0'>
          <dt
            title={t('Request success rate sampled over the last 24 hours')}
            className='text-muted-foreground flex items-center justify-between gap-1 text-[11px] leading-4'
          >
            <span>{t('Status')}</span>
            <span className='font-mono'>
              {hasSuccessRate ? `${successRate.toFixed(1)}%` : '—%'}
            </span>
          </dt>
          <dd className='mt-1 flex h-3 items-center'>
            {props.onOpenPerformance ? (
              <Button
                variant='ghost'
                className='h-3 w-24 rounded-xs p-0'
                aria-label={t('View performance')}
                onClick={(event) => {
                  event.stopPropagation()
                  props.onOpenPerformance?.()
                }}
              >
                <StatusTimeline timeline={timeline} />
              </Button>
            ) : (
              <StatusTimeline timeline={timeline} />
            )}
          </dd>
        </div>
        <div title={t('Average latency')} className='w-11 shrink-0'>
          <dt className='text-muted-foreground text-[11px] leading-4'>
            {t('Latency short')}
          </dt>
          <dd className='mt-1 font-mono whitespace-nowrap'>
            {latencyText === '—' ? '—s' : latencyText}
          </dd>
        </div>
        <div title={t('Throughput')} className='w-[52px] shrink-0'>
          <dt className='text-muted-foreground text-[11px] leading-4'>
            {t('Throughput short')}
          </dt>
          <dd className='mt-1 font-mono whitespace-nowrap'>
            {throughputText === '—' ? '—t/s' : throughputText}
          </dd>
        </div>
      </dl>
      {props.children}
    </div>
  )
})
