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
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { VChart } from '@visactor/react-vchart'
import { ArrowLeft, RefreshCw } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  getChannel,
  getChannelAdmissionStats,
  getChannels,
} from '@/features/channels/api'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'

import type { Channel, ChannelAdmissionMetricPoint } from '../types'

const EMPTY_CHANNELS: Channel[] = []

const RANGE_OPTIONS = [
  { hours: 1, label: 'Last hour' },
  { hours: 24, label: 'Last 24 hours' },
  { hours: 168, label: 'Last 7 days' },
] as const

function minuteLabel(timestampMs: number, hours: number): string {
  const date = new Date(timestampMs)
  const hour = String(date.getHours()).padStart(2, '0')
  if (hours <= 24) {
    const minute = String(date.getMinutes()).padStart(2, '0')
    return `${hour}:${minute}`
  }
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${month}-${day} ${hour}:00`
}

function AdmissionTrendChart(props: {
  title: string
  points: ChannelAdmissionMetricPoint[]
  hours: number
  series: Array<{
    key: keyof ChannelAdmissionMetricPoint
    label: string
    color: string
  }>
  unit?: string
}) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()
  const textColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.68)'
      : 'rgba(15, 23, 42, 0.58)'
  const gridColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.12)'
      : 'rgba(15, 23, 42, 0.12)'
  const spec = useMemo(() => {
    if (props.points.length === 0) return null
    const values = props.points.flatMap((point) =>
      props.series.map((series) => ({
        time: minuteLabel(point.timestamp_ms, props.hours),
        series: t(series.label),
        value: Number(point[series.key]) || 0,
      }))
    )
    return {
      type: 'line' as const,
      data: [{ id: 'channel-admission', values }],
      xField: 'time',
      yField: 'value',
      seriesField: 'series',
      point: { visible: false },
      line: { style: { lineWidth: 2 } },
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
              value: (datum: { value: number }) =>
                String(Math.round(datum.value)) +
                (props.unit ? ' ' + props.unit : ''),
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
          label: {
            formatMethod: (value: number | string) =>
              String(value) + (props.unit ? ' ' + props.unit : ''),
            style: { fill: textColor, fontSize: 10 },
          },
          grid: {
            visible: true,
            style: { lineDash: [3, 3], stroke: gridColor },
          },
        },
      ],
    }
  }, [
    gridColor,
    props.hours,
    props.points,
    props.series,
    props.unit,
    t,
    textColor,
  ])

  return (
    <section className='space-y-2'>
      <h2 className='text-sm font-medium'>{t(props.title)}</h2>
      <div className='h-64 rounded-md border p-3'>
        {spec && themeReady ? (
          <VChart
            spec={spec}
            options={{ ...VCHART_OPTION, theme: resolvedTheme }}
            className='h-full w-full'
          />
        ) : (
          <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
            {t('No admission history for this period')}
          </div>
        )}
      </div>
    </section>
  )
}

export function ChannelAdmissionMonitor({
  initialChannelId,
}: {
  initialChannelId?: number
}) {
  const { t } = useTranslation()
  const [selectedChannelId, setSelectedChannelId] = useState<
    number | undefined
  >(initialChannelId)
  const [rangeHours, setRangeHours] =
    useState<(typeof RANGE_OPTIONS)[number]['hours']>(24)
  const channelsQuery = useQuery({
    queryKey: ['channel-admission-monitor-channels'],
    queryFn: async () => {
      const response = await getChannels({ p: 1, page_size: 100 })
      return requireServerSuccess(response).data?.items ?? []
    },
    staleTime: 60_000,
  })
  const channels = channelsQuery.data ?? EMPTY_CHANNELS
  const channelId = selectedChannelId ?? channels[0]?.id ?? 0
  const selectedChannelQuery = useQuery({
    queryKey: ['channel-admission-monitor-channel', channelId],
    queryFn: async () => requireServerSuccess(await getChannel(channelId)).data,
    enabled:
      channelId > 0 && !channels.some((channel) => channel.id === channelId),
    staleTime: 60_000,
  })
  const statsQuery = useQuery({
    queryKey: ['channel-admission-stats', channelId, rangeHours],
    queryFn: async () => {
      const end = Math.floor(Date.now() / 1000)
      const start = end - rangeHours * 60 * 60
      return requireServerSuccess(
        await getChannelAdmissionStats(channelId, {
          start_timestamp: start,
          end_timestamp: end,
        })
      ).data
    },
    enabled: channelId > 0,
    refetchInterval: 15_000,
  })
  const stats = statsQuery.data
  const selectedChannel =
    channels.find((channel) => channel.id === channelId) ??
    selectedChannelQuery.data
  const channelOptions =
    selectedChannel && !channels.some((channel) => channel.id === channelId)
      ? [selectedChannel, ...channels]
      : channels

  return (
    <main className='mx-auto flex w-full max-w-7xl flex-col gap-6 p-4 md:p-6'>
      <header className='flex flex-wrap items-end justify-between gap-3'>
        <div className='flex items-start gap-3'>
          <Button
            variant='outline'
            size='icon'
            aria-label={t('Back to channels')}
            title={t('Back to channels')}
            render={<Link to='/channels' />}
          >
            <ArrowLeft className='size-4' />
          </Button>
          <div className='space-y-1'>
            <h1 className='text-xl font-semibold'>{t('Channel Admission')}</h1>
            <p className='text-muted-foreground text-sm'>
              {selectedChannel?.name ||
                t('Current queue and seven-day admission history')}
            </p>
          </div>
        </div>
        <div className='flex flex-wrap items-end gap-2'>
          <div className='min-w-56 space-y-1'>
            <Label htmlFor='admission-monitor-channel'>{t('Channel')}</Label>
            <Select
              value={channelId ? String(channelId) : ''}
              onValueChange={(value) =>
                setSelectedChannelId(value ? Number(value) : undefined)
              }
            >
              <SelectTrigger id='admission-monitor-channel'>
                <SelectValue placeholder={t('Select channel')} />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {channelOptions.map((channel) => (
                    <SelectItem key={channel.id} value={String(channel.id)}>
                      {channel.name} · #{channel.id}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
          <div className='min-w-40 space-y-1'>
            <Label htmlFor='admission-monitor-range'>{t('Time range')}</Label>
            <Select
              value={String(rangeHours)}
              onValueChange={(value) =>
                setRangeHours(
                  Number(value) as (typeof RANGE_OPTIONS)[number]['hours']
                )
              }
            >
              <SelectTrigger id='admission-monitor-range'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {RANGE_OPTIONS.map((option) => (
                    <SelectItem key={option.hours} value={String(option.hours)}>
                      {t(option.label)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
          <Button
            type='button'
            variant='outline'
            size='icon'
            aria-label={t('Refresh')}
            title={t('Refresh')}
            disabled={statsQuery.isFetching}
            onClick={() => void statsQuery.refetch()}
          >
            <RefreshCw
              className={
                statsQuery.isFetching ? 'size-4 animate-spin' : 'size-4'
              }
            />
          </Button>
        </div>
      </header>

      {statsQuery.isError ? (
        <p role='alert' className='text-destructive text-sm'>
          {t('Failed to load channel admission statistics')}
        </p>
      ) : (
        <>
          <section className='grid grid-cols-2 gap-3 border-y py-4 sm:grid-cols-4 lg:grid-cols-8'>
            {[
              [t('Active'), stats?.active ?? 0],
              [t('Waiting'), stats?.waiting ?? 0],
              [t('Oldest wait'), `${stats?.oldest_wait_ms ?? 0} ms`],
              [t('Wait p50'), `${stats?.wait_p50_ms ?? 0} ms`],
              [t('Wait p95'), `${stats?.wait_p95_ms ?? 0} ms`],
              [t('Soup grants'), stats?.soup_grants ?? 0],
              [t('Queue full'), stats?.queue_full ?? 0],
              [t('Timeouts'), stats?.timeouts ?? 0],
            ].map(([label, value]) => (
              <div key={String(label)} className='space-y-1'>
                <p className='text-muted-foreground text-xs'>{label}</p>
                <p className='text-lg font-semibold tabular-nums'>{value}</p>
              </div>
            ))}
          </section>

          {statsQuery.isLoading ? (
            <p className='text-muted-foreground py-16 text-center text-sm'>
              {t('Loading...')}
            </p>
          ) : (
            <div className='grid gap-6 lg:grid-cols-2'>
              <AdmissionTrendChart
                title='Admissions'
                points={stats?.minute_series ?? []}
                hours={rangeHours}
                series={[
                  { key: 'grants', label: 'Grants', color: '#0ea5e9' },
                  {
                    key: 'soup_grants',
                    label: 'Soup grants',
                    color: '#f97316',
                  },
                ]}
              />
              <AdmissionTrendChart
                title='Queue wait time'
                points={stats?.minute_series ?? []}
                hours={rangeHours}
                series={[
                  { key: 'wait_p50_ms', label: 'Wait p50', color: '#10b981' },
                  { key: 'wait_p95_ms', label: 'Wait p95', color: '#e11d48' },
                ]}
                unit='ms'
              />
              <AdmissionTrendChart
                title='Queue pressure'
                points={stats?.minute_series ?? []}
                hours={rangeHours}
                series={[
                  { key: 'queue_full', label: 'Queue full', color: '#f97316' },
                  { key: 'timeouts', label: 'Timeouts', color: '#8b5cf6' },
                ]}
              />
            </div>
          )}
        </>
      )}
    </main>
  )
}
