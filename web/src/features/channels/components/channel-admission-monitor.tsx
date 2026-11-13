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
import { Link, useNavigate } from '@tanstack/react-router'
import { ArrowLeft, CheckCircle2, Gauge, RefreshCw } from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { toIntlLocale } from '@/i18n/languages'
import { formatCompactNumber } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { getAllChannels, getChannel, getChannelAdmissionStats } from '../api'
import {
  DEFAULT_ADMISSION_LIVE_REFRESH_SECONDS,
  effectiveAdmissionControl,
  formatAdmissionBucketWidth,
  formatAdmissionWaitMs,
  hasAdmissionQueue,
  parseAdmissionLiveRefreshSeconds,
} from '../lib/admission'
import type { Channel } from '../types'
import { ChannelAdmissionLiveRefreshSelect } from './channel-admission-live-refresh'
import { ChannelAdmissionSettingsPanel } from './channel-admission-settings-panel'
import { ChannelAdmissionTrendChart } from './channel-admission-trend-chart'

const EMPTY_CHANNELS: Channel[] = []
const DEFAULT_CHANNEL_ID = 0
const LIVE_REFRESH_STORAGE_KEY = 'channels:admission:live-refresh'

function readLiveRefreshSeconds(): number {
  if (typeof window === 'undefined') {
    return DEFAULT_ADMISSION_LIVE_REFRESH_SECONDS
  }
  try {
    return parseAdmissionLiveRefreshSeconds(
      window.localStorage.getItem(LIVE_REFRESH_STORAGE_KEY)
    )
  } catch {
    return DEFAULT_ADMISSION_LIVE_REFRESH_SECONDS
  }
}

const RANGE_OPTIONS = [
  { hours: 1, label: 'Last hour' },
  { hours: 6, label: 'Last 6 hours' },
  { hours: 12, label: 'Last 12 hours' },
  { hours: 24, label: 'Last 24 hours' },
  { hours: 168, label: 'Last 7 days' },
] as const

type AdmissionRangeHours = (typeof RANGE_OPTIONS)[number]['hours']

function MetricCell(props: { label: string; value: string | number }) {
  return (
    <div className='bg-muted/40 border-border/50 rounded-lg border px-2.5 py-2'>
      <div className='text-muted-foreground truncate text-[10px] font-medium'>
        {props.label}
      </div>
      <div
        className='mt-1 truncate font-mono text-sm font-semibold tabular-nums'
        title={String(props.value)}
      >
        {props.value}
      </div>
    </div>
  )
}

export function ChannelAdmissionMonitor(props: {
  initialChannelId?: number
  initialRangeHours?: AdmissionRangeHours
}) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const rangeHours = props.initialRangeHours ?? 24
  const [liveRefreshSeconds, setLiveRefreshSeconds] = useState(
    readLiveRefreshSeconds
  )
  const handleLiveRefreshChange = (seconds: number) => {
    setLiveRefreshSeconds(seconds)
    try {
      window.localStorage.setItem(LIVE_REFRESH_STORAGE_KEY, String(seconds))
    } catch {
      // Storage may be unavailable in private browsing; the session still works.
    }
  }
  const handleChannelChange = (value: string | null) => {
    const nextChannelId = value ? Number(value) : undefined
    void navigate({
      to: '/channels/admission',
      search: (previous) => ({ ...previous, channel_id: nextChannelId }),
      replace: true,
    })
  }
  const handleRangeChange = (value: string | null) => {
    if (!value) {
      return
    }
    const nextRange = Number(value) as AdmissionRangeHours
    void navigate({
      to: '/channels/admission',
      search: (previous) => ({ ...previous, range: nextRange }),
      replace: true,
    })
  }
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const formatCount = (value: number | undefined) =>
    value === undefined ? '—' : formatCompactNumber(value, locale)
  let refetchInterval = 60_000
  if (rangeHours <= 1) {
    refetchInterval = 30_000
  } else if (rangeHours > 24) {
    refetchInterval = 120_000
  }

  const channelsQuery = useQuery({
    queryKey: ['channel-admission-monitor-channels'],
    queryFn: getAllChannels,
    staleTime: 0,
  })
  const channels = channelsQuery.data ?? EMPTY_CHANNELS
  const sortedChannels = useMemo(() => {
    return [...channels].sort((left, right) => {
      const leftQueued = hasAdmissionQueue(left) ? 0 : 1
      const rightQueued = hasAdmissionQueue(right) ? 0 : 1
      if (leftQueued !== rightQueued) {
        return leftQueued - rightQueued
      }
      return left.name.localeCompare(right.name)
    })
  }, [channels])
  const fallbackChannelId =
    sortedChannels.find((channel) => hasAdmissionQueue(channel))?.id ??
    sortedChannels[0]?.id ??
    DEFAULT_CHANNEL_ID
  const channelId = props.initialChannelId ?? fallbackChannelId
  const selectedChannelQuery = useQuery({
    queryKey: ['channel-admission-monitor-channel', channelId],
    queryFn: async () => requireServerSuccess(await getChannel(channelId)).data,
    enabled:
      channelId > 0 &&
      !sortedChannels.some((channel) => channel.id === channelId),
    staleTime: 60_000,
  })
  const liveQuery = useQuery({
    queryKey: ['channel-admission-live', channelId],
    queryFn: async () => {
      // Queue state changes second by second, so it is polled separately from
      // the heavier window scan that feeds the charts.
      const now = Math.floor(Date.now() / 1000)
      return requireServerSuccess(
        await getChannelAdmissionStats(channelId, {
          start_timestamp: now,
          end_timestamp: now,
        })
      ).data
    },
    enabled: channelId > 0,
    staleTime: 5_000,
    refetchInterval:
      liveRefreshSeconds > 0 ? liveRefreshSeconds * 1_000 : false,
    meta: { errorToast: false },
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
    refetchInterval,
    meta: { errorToast: false },
  })
  const stats = statsQuery.data
  const [manualRefreshing, setManualRefreshing] = useState(false)
  const refreshNow = async () => {
    if (manualRefreshing) {
      return
    }
    setManualRefreshing(true)
    try {
      await Promise.all([
        channelsQuery.refetch(),
        selectedChannelQuery.refetch(),
        liveQuery.refetch(),
        statsQuery.refetch(),
      ])
    } finally {
      setManualRefreshing(false)
    }
  }
  const live = liveQuery.data ?? stats
  const waitingUsers = live?.waiting_users ?? []
  const waitingCount = live?.waiting ?? 0
  const selectedChannel =
    sortedChannels.find((channel) => channel.id === channelId) ??
    selectedChannelQuery.data
  const channelOptions = sortedChannels.map((channel) => ({
    value: String(channel.id),
    label: `${channel.name} · #${channel.id}`,
  }))
  if (
    selectedChannel &&
    !channelOptions.some((option) => option.value === String(channelId))
  ) {
    channelOptions.unshift({
      value: String(selectedChannel.id),
      label: `${selectedChannel.name} · #${selectedChannel.id}`,
    })
  }
  const limit = selectedChannel?.concurrency_limit ?? 0
  const activeLabel =
    limit > 0
      ? `${formatCount(live?.active)} / ${formatCount(limit)}`
      : formatCount(live?.active)
  const queueEnabled = selectedChannel
    ? hasAdmissionQueue(selectedChannel)
    : true
  const admissionControl = selectedChannel
    ? effectiveAdmissionControl(selectedChannel)
    : undefined
  const waitingLabel =
    admissionControl && admissionControl.max_waiting_total > 0
      ? `${formatCount(waitingCount)} / ${formatCount(admissionControl.max_waiting_total)}`
      : formatCount(waitingCount)
  const rangeOption = RANGE_OPTIONS.find(
    (option) => option.hours === rangeHours
  )
  const rangeLabel = t(rangeOption?.label ?? 'Last 24 hours')
  const bucketWidth = formatAdmissionBucketWidth(stats?.bucket_seconds ?? 60)
  let liveStatusLabel = t('Ready')
  let liveStatusVariant: StatusVariant = 'neutral'
  let liveStatusPulse = false
  if (waitingCount > 0) {
    liveStatusLabel = t('Waiting')
    liveStatusVariant = 'warning'
    liveStatusPulse = true
  } else if ((live?.active ?? 0) > 0) {
    liveStatusLabel = t('Running')
    liveStatusVariant = 'success'
    liveStatusPulse = true
  }
  let content: ReactNode
  if (channelsQuery.isLoading) {
    content = <LoadingState />
  } else if (channelsQuery.isError) {
    content = (
      <p role='alert' className='text-destructive text-sm'>
        {t('Failed to load channels')}
      </p>
    )
  } else if (statsQuery.isError && !stats) {
    content = (
      <p role='alert' className='text-destructive text-sm'>
        {t('Failed to load channel admission statistics')}
      </p>
    )
  } else if (!selectedChannel) {
    content = (
      <EmptyState
        icon={Gauge}
        title={t('No channels found')}
        description={t(
          'Create a channel before monitoring its admission queue.'
        )}
      />
    )
  } else if (statsQuery.isLoading) {
    content = <LoadingState />
  } else {
    content = (
      <>
        {!queueEnabled && (
          <EmptyState
            icon={Gauge}
            bordered
            className='min-h-[160px] rounded-2xl'
            title={t('Admission queue is inactive')}
            description={t(
              'This channel has no concurrency limit, so requests are never queued. Configure a concurrency limit to start using the queue.'
            )}
            action={
              <Button
                variant='outline'
                render={<Link to='/channels' search={{ edit: channelId }} />}
              >
                {t('Edit channel settings')}
              </Button>
            }
          />
        )}

        <section className='bg-card overflow-hidden rounded-2xl border shadow-xs'>
          <header className='flex flex-wrap items-center gap-2 border-b px-4 py-2.5'>
            <h2 className='text-sm font-semibold'>{t('Live')}</h2>
            <StatusBadge
              label={liveStatusLabel}
              variant={liveStatusVariant}
              size='sm'
              pulse={liveStatusPulse}
              copyable={false}
            />
            <div className='ml-auto'>
              <ChannelAdmissionLiveRefreshSelect
                seconds={liveRefreshSeconds}
                onChange={handleLiveRefreshChange}
              />
            </div>
          </header>
          <div className='space-y-3 p-3 sm:p-4'>
            <div className='grid grid-cols-2 content-start gap-2 sm:grid-cols-3 xl:grid-cols-5'>
              <MetricCell label={t('Concurrency')} value={activeLabel} />
              <MetricCell
                label={t('Active users')}
                value={formatCount(live?.active_users)}
              />
              <MetricCell label={t('Waiting')} value={waitingLabel} />
              <MetricCell
                label={t('Waiting users')}
                value={formatCount(waitingUsers.length)}
              />
              <MetricCell
                label={t('Oldest wait')}
                value={
                  waitingCount > 0
                    ? formatAdmissionWaitMs(live?.oldest_wait_ms)
                    : '—'
                }
              />
            </div>

            <div className='bg-muted/30 border-border/60 rounded-xl border p-3'>
              <header className='flex items-center gap-2'>
                <span className='text-xs font-medium'>{t('Queued users')}</span>
                <span className='text-muted-foreground ml-auto text-xs tabular-nums'>
                  {waitingUsers.length}
                </span>
              </header>
              {waitingUsers.length > 0 ? (
                <ul className='mt-2 grid gap-1.5 sm:grid-cols-2 xl:grid-cols-3'>
                  {waitingUsers.map((user) => {
                    const displayName =
                      user.username || `${t('User')} #${user.user_id}`
                    const initial = user.username
                      ? user.username.slice(0, 1).toUpperCase()
                      : '#'
                    const badge =
                      user.position > 0 ? String(user.position) : initial
                    const meta: string[] = []
                    if (user.username) {
                      meta.push(`#${user.user_id}`)
                    }
                    if (user.priority !== 0) {
                      meta.push(`${t('Priority')} ${user.priority}`)
                    }
                    return (
                      <li
                        key={user.user_id}
                        className='bg-background/70 border-border/40 flex items-center justify-between gap-2 rounded-lg border px-2.5 py-1.5'
                      >
                        <span className='flex min-w-0 items-center gap-2'>
                          <span className='bg-warning/10 text-warning flex size-6 shrink-0 items-center justify-center rounded-md text-[10px] font-semibold'>
                            {badge}
                          </span>
                          <span className='min-w-0'>
                            <span className='block truncate text-xs font-medium'>
                              {displayName}
                            </span>
                            {meta.length > 0 && (
                              <span className='text-muted-foreground block truncate text-[11px]'>
                                {meta.join(' · ')}
                              </span>
                            )}
                          </span>
                        </span>
                        <span className='shrink-0 text-right text-[11px] tabular-nums'>
                          <span className='block font-semibold'>
                            ×{user.count}
                          </span>
                          <span className='text-muted-foreground'>
                            {formatAdmissionWaitMs(user.oldest_wait_ms)}
                          </span>
                        </span>
                      </li>
                    )
                  })}
                </ul>
              ) : (
                <div className='text-muted-foreground mt-2 flex items-center gap-1.5 text-xs'>
                  <CheckCircle2 className='text-success size-3.5' />
                  <span>{t('No users are waiting')}</span>
                </div>
              )}
            </div>
          </div>
        </section>

        <section className='bg-card overflow-hidden rounded-2xl border shadow-xs'>
          <header className='flex flex-wrap items-center gap-2 border-b px-4 py-2.5'>
            <h2 className='text-sm font-semibold'>{t('Selected range')}</h2>
            <span className='text-muted-foreground ml-auto text-xs'>
              {rangeLabel} ·{' '}
              {t('Aggregated every {{interval}}', { interval: bucketWidth })}
            </span>
          </header>
          <div className='grid grid-cols-2 gap-2 p-3 sm:grid-cols-3 sm:p-4 lg:grid-cols-4 xl:grid-cols-7'>
            <MetricCell
              label={t('Grants')}
              value={formatCount(stats?.grants)}
            />
            <MetricCell
              label={t('Waited requests')}
              value={formatCount(stats?.waited_grants)}
            />
            <MetricCell
              label={t('Timeouts')}
              value={formatCount(stats?.timeouts)}
            />
            <MetricCell
              label={t('Queue full')}
              value={formatCount(stats?.queue_full)}
            />
            <MetricCell
              label={t('Lower-priority grants')}
              value={formatCount(stats?.soup_grants)}
            />
            <MetricCell
              label={t('Wait p50')}
              value={formatAdmissionWaitMs(stats?.wait_p50_ms)}
            />
            <MetricCell
              label={t('Wait p95')}
              value={formatAdmissionWaitMs(stats?.wait_p95_ms)}
            />
          </div>
        </section>

        <ChannelAdmissionSettingsPanel channel={selectedChannel} />

        <div className='grid gap-4 lg:grid-cols-2'>
          <ChannelAdmissionTrendChart
            title='Admissions'
            filled
            points={stats?.minute_series ?? []}
            bucketSeconds={stats?.bucket_seconds ?? 60}
            series={[
              { key: 'grants', label: 'Grants', color: '#0ea5e9' },
              {
                key: 'waited',
                label: 'Waited requests',
                color: '#f59e0b',
              },
            ]}
          />
          <ChannelAdmissionTrendChart
            title='Queue wait time'
            points={stats?.minute_series ?? []}
            bucketSeconds={stats?.bucket_seconds ?? 60}
            series={[
              { key: 'wait_p50_ms', label: 'Wait p50', color: '#10b981' },
              { key: 'wait_p95_ms', label: 'Wait p95', color: '#e11d48' },
            ]}
            unit='ms'
          />
          <ChannelAdmissionTrendChart
            title='Queue pressure'
            className='lg:col-span-2'
            points={stats?.minute_series ?? []}
            bucketSeconds={stats?.bucket_seconds ?? 60}
            series={[
              { key: 'queue_full', label: 'Queue full', color: '#f97316' },
              { key: 'timeouts', label: 'Timeouts', color: '#8b5cf6' },
              {
                key: 'soup_grants',
                label: 'Lower-priority grants',
                color: '#0ea5e9',
              },
            ]}
          />
        </div>
      </>
    )
  }

  return (
    <main className='h-full overflow-y-auto'>
      <div className='mx-auto flex w-full max-w-7xl flex-col gap-4 p-4 md:p-6'>
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
              <h1 className='text-xl font-semibold'>
                {t('Channel Admission')}
              </h1>
              <p className='text-muted-foreground text-sm'>
                {selectedChannel
                  ? `${selectedChannel.name} · #${selectedChannel.id}`
                  : t('Current queue and seven-day admission history')}
              </p>
            </div>
          </div>
          <div className='flex flex-wrap items-end gap-2'>
            <div className='flex w-full flex-col gap-1 sm:w-60'>
              <Label htmlFor='admission-monitor-channel'>{t('Channel')}</Label>
              <Combobox
                id='admission-monitor-channel'
                options={channelOptions}
                value={channelId ? String(channelId) : ''}
                onValueChange={handleChannelChange}
                placeholder={t('Select channel')}
                searchPlaceholder={t('Search channels')}
                emptyText={t('No channels found')}
                className='h-8 w-full'
              />
            </div>
            <div className='flex w-full flex-col gap-1 sm:w-60'>
              <Label htmlFor='admission-monitor-range'>{t('Time range')}</Label>
              <Select
                value={String(rangeHours)}
                onValueChange={handleRangeChange}
                items={RANGE_OPTIONS.map((option) => ({
                  value: String(option.hours),
                  label: t(option.label),
                }))}
              >
                <SelectTrigger id='admission-monitor-range' className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {RANGE_OPTIONS.map((option) => (
                      <SelectItem
                        key={option.hours}
                        value={String(option.hours)}
                      >
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
              disabled={manualRefreshing}
              onClick={() => {
                void refreshNow()
              }}
            >
              <RefreshCw
                className={cn('size-4', manualRefreshing && 'animate-spin')}
              />
            </Button>
          </div>
        </header>

        {content}
      </div>
    </main>
  )
}
