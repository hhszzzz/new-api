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
import { useTranslation } from 'react-i18next'

import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  formatLatency,
  formatThroughput,
  formatUptimePct,
} from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

import type { ModelStatusTimelinePoint } from '../types'
import {
  getModelStatusBarClass,
  getModelStatusLabel,
} from './status-presentation'

type StatusTimelineProps = {
  timeline: ModelStatusTimelinePoint[]
  hourFormatter: Intl.DateTimeFormat
  numberFormatter: Intl.NumberFormat
}

function formatHour(timestamp: number, formatter: Intl.DateTimeFormat): string {
  return formatter.format(new Date(timestamp * 1000))
}

function formatHourRange(
  timestamp: number,
  formatter: Intl.DateTimeFormat
): string {
  const start = new Date(timestamp * 1000)
  const end = new Date((timestamp + 60 * 60) * 1000)

  if (typeof formatter.formatRange === 'function') {
    return formatter.formatRange(start, end)
  }
  return `${formatter.format(start)}–${formatter.format(end)}`
}

export function StatusTimeline(props: StatusTimelineProps) {
  const { t } = useTranslation()
  const firstPoint = props.timeline[0]
  const lastPoint = props.timeline.at(-1)

  return (
    <div>
      <TooltipProvider delay={100}>
        <ol
          className='grid grid-cols-[repeat(24,minmax(0,1fr))] gap-0.5 sm:gap-1'
          aria-label={t('Status over the last 24 hours')}
        >
          {props.timeline.map((point) => {
            const barClassName = getModelStatusBarClass(
              point.status,
              point.success_rate
            )
            const statusLabel = getModelStatusLabel(t, point.status)
            const timeRangeLabel = formatHourRange(
              point.ts,
              props.hourFormatter
            )
            const requestCount =
              point.request_count === null
                ? '—'
                : props.numberFormatter.format(point.request_count)
            const successCount =
              point.success_count === null
                ? '—'
                : props.numberFormatter.format(point.success_count)
            const successRate =
              point.success_rate === null
                ? '—'
                : formatUptimePct(point.success_rate)
            const avgTtft =
              point.avg_ttft_ms === null
                ? '—'
                : formatLatency(point.avg_ttft_ms)
            const avgLatency =
              point.avg_latency_ms === null
                ? '—'
                : formatLatency(point.avg_latency_ms)
            const throughput =
              point.avg_tps === null ? '—' : formatThroughput(point.avg_tps)
            const accessibleLabel = [
              `${timeRangeLabel}: ${statusLabel}`,
              `${t('Requests')}: ${requestCount}`,
              `${t('Successful requests')}: ${successCount}`,
              `${t('Success rate')}: ${successRate}`,
              `${t('Average TTFT')}: ${avgTtft}`,
              `${t('Average latency')}: ${avgLatency}`,
              `${t('Throughput')}: ${throughput}`,
            ].join('; ')

            return (
              <li
                key={point.ts}
                className='min-w-0'
                aria-label={accessibleLabel}
              >
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <button
                        type='button'
                        className={cn(
                          'block h-7 w-full min-w-0 cursor-default overflow-hidden rounded-[3px] border-0 p-0 outline-none transition-[filter,box-shadow] hover:brightness-95 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 sm:h-8 sm:rounded',
                          barClassName
                        )}
                        aria-label={accessibleLabel}
                      />
                    }
                  />
                  <TooltipContent
                    role='tooltip'
                    side='top'
                    className='block w-64 max-w-[calc(100vw-2rem)] p-3'
                  >
                    <dl className='grid grid-cols-[minmax(0,1fr)_auto] gap-x-4 gap-y-1.5'>
                      <TooltipMetric label={t('Time')}>
                        <span>{timeRangeLabel}</span>
                      </TooltipMetric>
                      <TooltipMetric label={t('Requests')}>
                        {requestCount}
                      </TooltipMetric>
                      <TooltipMetric label={t('Successful requests')}>
                        {successCount}
                      </TooltipMetric>
                      <TooltipMetric label={t('Success rate')}>
                        {successRate}
                      </TooltipMetric>
                      <TooltipMetric label={t('Average TTFT')}>
                        {avgTtft}
                      </TooltipMetric>
                      <TooltipMetric label={t('Average latency')}>
                        {avgLatency}
                      </TooltipMetric>
                      <TooltipMetric label={t('Throughput')}>
                        {throughput}
                      </TooltipMetric>
                    </dl>
                  </TooltipContent>
                </Tooltip>
              </li>
            )
          })}
        </ol>
      </TooltipProvider>
      {firstPoint && lastPoint ? (
        <div className='text-muted-foreground mt-1.5 flex justify-between font-mono text-[10px] tabular-nums'>
          <time dateTime={new Date(firstPoint.ts * 1000).toISOString()}>
            {formatHour(firstPoint.ts, props.hourFormatter)}
          </time>
          <time
            dateTime={new Date((lastPoint.ts + 60 * 60) * 1000).toISOString()}
          >
            {formatHour(lastPoint.ts + 60 * 60, props.hourFormatter)}
          </time>
        </div>
      ) : null}
    </div>
  )
}

function TooltipMetric(props: { label: string; children: React.ReactNode }) {
  return (
    <>
      <dt className='text-background/70'>{props.label}</dt>
      <dd className='text-right font-mono font-medium tabular-nums'>
        {props.children}
      </dd>
    </>
  )
}
