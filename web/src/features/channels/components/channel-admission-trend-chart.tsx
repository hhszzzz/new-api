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
import { VChart } from '@visactor/react-vchart'
import { Activity, BarChart3, type LucideIcon } from 'lucide-react'
import { useMemo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { IconBadge, type IconBadgeTone } from '@/components/ui/icon-badge'
import { useChartTheme } from '@/lib/use-chart-theme'
import { cn } from '@/lib/utils'
import { VCHART_OPTION } from '@/lib/vchart'

import {
  admissionBucketLabel,
  formatAdmissionBucketWidth,
  formatAdmissionWaitMs,
} from '../lib/admission'
import type { ChannelAdmissionMetricPoint } from '../types'

export function ChannelAdmissionTrendChart(props: {
  title: string
  icon?: LucideIcon
  iconTone?: IconBadgeTone
  summary?: ReactNode
  points: ChannelAdmissionMetricPoint[]
  bucketSeconds: number
  series: Array<{
    key: keyof ChannelAdmissionMetricPoint
    label: string
    color: string
  }>
  unit?: string
  filled?: boolean
  className?: string
}) {
  const { t } = useTranslation()
  const HeaderIcon = props.icon ?? Activity
  const { resolvedTheme, themeReady } = useChartTheme()
  const textColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.68)'
      : 'rgba(15, 23, 42, 0.58)'
  const gridColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.12)'
      : 'rgba(15, 23, 42, 0.12)'
  const hasActivity = props.points.some((point) =>
    props.series.some((series) => (Number(point[series.key]) || 0) > 0)
  )
  const spec = useMemo(() => {
    if (props.points.length === 0) return null
    const values = props.points.flatMap((point) =>
      props.series.map((series) => ({
        time: admissionBucketLabel(point.timestamp_ms, props.bucketSeconds),
        series: t(series.label),
        value: Number(point[series.key]) || 0,
      }))
    )
    // Wait time is stored in milliseconds but shown in seconds everywhere else,
    // including the live stat cards and the queue table.
    const formatValue = (value: number) =>
      props.unit === 'ms'
        ? formatAdmissionWaitMs(value)
        : String(Math.round(value))
    return {
      type: 'area' as const,
      stack: false,
      data: [{ id: 'channel-admission', values }],
      xField: 'time',
      yField: 'value',
      seriesField: 'series',
      point: { visible: false },
      area: { style: { fillOpacity: props.filled ? 0.12 : 0 } },
      line: {
        style: { lineWidth: 2, curveType: 'step' as const },
      },
      color: {
        specified: Object.fromEntries(
          props.series.map((item) => [t(item.label), item.color])
        ),
      },
      legends: {
        visible: true,
        orient: 'top' as const,
        position: 'middle' as const,
      },
      tooltip: {
        mark: {
          title: { value: (datum: { time: string }) => datum.time },
          content: [
            {
              key: (datum: { series: string }) => datum.series,
              value: (datum: { value: number }) => formatValue(datum.value),
            },
          ],
        },
      },
      axes: [
        {
          orient: 'bottom' as const,
          label: { style: { fill: textColor, fontSize: 10 }, tickCount: 6 },
          tick: { visible: false },
        },
        {
          orient: 'left' as const,
          min: 0,
          label: {
            formatMethod: (value: number | string) =>
              formatValue(Number(value)),
            style: { fill: textColor, fontSize: 10 },
          },
          tick:
            props.unit === 'ms'
              ? { visible: false }
              : { visible: false, noDecimals: true },
          grid: {
            visible: true,
            style: { lineDash: [3, 3], stroke: gridColor },
          },
        },
      ],
    }
  }, [
    gridColor,
    props.bucketSeconds,
    props.filled,
    props.points,
    props.series,
    props.unit,
    t,
    textColor,
  ])

  return (
    <section
      className={cn(
        'bg-card touch-pan-y overflow-hidden rounded-2xl border shadow-xs',
        props.className
      )}
    >
      <header className='flex flex-wrap items-center gap-2 border-b px-4 py-3 sm:px-5'>
        <IconBadge tone={props.iconTone ?? 'info'} size='sm'>
          <HeaderIcon />
        </IconBadge>
        <h2 className='text-sm font-semibold'>{t(props.title)}</h2>
        {props.summary}
        <span className='text-muted-foreground ml-auto font-mono text-xs tabular-nums'>
          {t('Aggregated every {{interval}}', {
            interval: formatAdmissionBucketWidth(props.bucketSeconds),
          })}
        </span>
      </header>
      <div className='h-56 p-3 sm:h-64 sm:p-4'>
        {themeReady &&
          (hasActivity && spec ? (
            <VChart
              key={`channel-admission-${props.title}-${resolvedTheme}`}
              spec={{
                ...spec,
                theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                background: 'transparent',
              }}
              options={VCHART_OPTION}
              className='h-full w-full'
            />
          ) : (
            <div className='text-muted-foreground flex h-full flex-col items-center justify-center gap-2 text-xs'>
              <span className='bg-muted/60 flex size-9 items-center justify-center rounded-xl'>
                <BarChart3 className='size-4' aria-hidden='true' />
              </span>
              <span>{t('No data available')}</span>
            </div>
          ))}
      </div>
    </section>
  )
}
