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
      className={cn('w-full min-w-0', props.className)}
    >
      <dl className='grid grid-cols-[119px_minmax(0,1fr)_minmax(0,1fr)] items-start gap-x-3 text-xs tabular-nums'>
        <div className='min-w-0'>
          <dt
            title={t('Request success rate sampled over the last 24 hours')}
            className='text-muted-foreground flex items-center justify-between gap-1 text-[11px] leading-4'
          >
            <span>{t('Status')}</span>
            <span className='font-mono'>
              {hasSuccessRate ? `${successRate.toFixed(1)}%` : '—%'}
            </span>
          </dt>
          <dd className='mt-1 flex h-4 items-center'>
            {props.onOpenPerformance ? (
              <Button
                variant='ghost'
                className='h-4 w-[119px] rounded-xs p-0'
                aria-label={t('View performance')}
                onClick={(event) => {
                  event.stopPropagation()
                  props.onOpenPerformance?.()
                }}
              >
                <StatusTimeline compact timeline={timeline} />
              </Button>
            ) : (
              <StatusTimeline compact timeline={timeline} />
            )}
          </dd>
        </div>
        <div title={t('Average latency')} className='min-w-0 text-right'>
          <dt className='text-muted-foreground truncate text-[11px] leading-4'>
            {t('Latency short')}
          </dt>
          <dd
            className='mt-1 h-4 truncate font-mono leading-4 whitespace-nowrap'
            title={latencyText}
          >
            {latencyText === '—' ? '—s' : latencyText}
          </dd>
        </div>
        <div title={t('Throughput')} className='min-w-0 text-right'>
          <dt className='text-muted-foreground truncate text-[11px] leading-4'>
            {t('Throughput short')}
          </dt>
          <dd
            className='mt-1 h-4 truncate font-mono leading-4 whitespace-nowrap'
            title={throughputText}
          >
            {throughputText === '—' ? '—t/s' : throughputText}
          </dd>
        </div>
      </dl>
      {props.children}
    </div>
  )
})
