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
import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
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
  admissionLiveStatus,
  effectiveAdmissionControl,
  formatAdmissionBucketWidth,
  formatAdmissionWaitMs,
  hasAdmissionQueue,
  newlyAdmittedUserIds,
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
  const activeUsers = live?.active_user_list ?? []
  const waitingUsers = live?.waiting_users ?? []
  const waitingRequests = useMemo(() => live?.waiting_requests ?? [], [live])
  const waitingCount = live?.waiting ?? 0
  const activeSignature = activeUsers
    .map((user) => `${user.user_id}:${user.count}`)
    .join(',')
  const previousActiveRef = useRef<Map<number, number>>(new Map())
  const activeInitializedRef = useRef(false)
  const [admittedPills, setAdmittedPills] = useState<ReadonlySet<string>>(
    () => new Set()
  )
  useEffect(() => {
    const next = new Map<number, number>()
    for (const entry of activeSignature.split(',')) {
      if (!entry) {
        continue
      }
      const [userId, count] = entry.split(':').map(Number)
      next.set(userId, count)
    }
    const previous = previousActiveRef.current
    previousActiveRef.current = next
    if (!activeInitializedRef.current) {
      activeInitializedRef.current = true
      return
    }
    const increased = newlyAdmittedUserIds(
      previous,
      Array.from(next, ([user_id, count]) => ({ user_id, count }))
    )
    const pillKeys: string[] = []
    for (const userId of increased) {
      const before = previous.get(userId) ?? 0
      const after = next.get(userId) ?? 0
      for (let index = before; index < after; index++) {
        pillKeys.push(`${userId}-${index}`)
      }
    }
    if (pillKeys.length === 0) {
      return
    }
    const showTimer = window.setTimeout(
      () => setAdmittedPills(new Set(pillKeys)),
      0
    )
    const clearTimer = window.setTimeout(() => setAdmittedPills(new Set()), 900)
    return () => {
      window.clearTimeout(showTimer)
      window.clearTimeout(clearTimer)
    }
  }, [activeSignature])
  const queuedRowRefs = useRef<Map<number, HTMLLIElement>>(new Map())
  const queuedRowSnapshotsRef = useRef<
    Map<number, { rect: DOMRect; capturedAt: number }>
  >(new Map())
  const activePillRefs = useRef<Map<string, HTMLElement>>(new Map())
  useLayoutEffect(() => {
    for (const request of waitingRequests) {
      const element = queuedRowRefs.current.get(request.user_id)
      if (element) {
        queuedRowSnapshotsRef.current.set(request.user_id, {
          rect: element.getBoundingClientRect(),
          capturedAt: Date.now(),
        })
      }
    }
  }, [waitingRequests])
  useLayoutEffect(() => {
    if (
      admittedPills.size === 0 ||
      window.matchMedia('(prefers-reduced-motion: reduce)').matches
    ) {
      return
    }
    for (const pillKey of admittedPills) {
      const pill = activePillRefs.current.get(pillKey)
      if (!pill) {
        continue
      }
      const userId = Number(pillKey.slice(0, pillKey.lastIndexOf('-')))
      const snapshot = queuedRowSnapshotsRef.current.get(userId)
      if (!snapshot || Date.now() - snapshot.capturedAt > 5_000) {
        continue
      }
      const { rect: start } = snapshot
      const target = pill.getBoundingClientRect()
      if (
        Math.abs(target.left - start.left) < 2 &&
        Math.abs(target.top - start.top) < 2
      ) {
        continue
      }
      const ghost = pill.cloneNode(true) as HTMLElement
      ghost.removeAttribute('data-admission-user')
      ghost.classList.remove('admission-row-admitted')
      ghost.setAttribute('aria-hidden', 'true')
      const startLeft = start.left + 12
      const startTop = start.top + (start.height - target.height) / 2
      const deltaX = target.left - startLeft
      const deltaY = target.top - startTop
      ghost.style.cssText = [
        'position: fixed',
        `left: ${startLeft}px`,
        `top: ${startTop}px`,
        `width: ${target.width}px`,
        `height: ${target.height}px`,
        'margin: 0',
        'z-index: 60',
        'pointer-events: none',
        'background-color: var(--background)',
        'transform-origin: top left',
        'will-change: transform, opacity',
        'box-shadow: 0 12px 28px rgba(16, 185, 129, 0.28)',
      ].join(';')
      document.body.appendChild(ghost)
      const animation = ghost.animate(
        [
          { transform: 'translate3d(0, 0, 0) scale(0.8)', opacity: 0.35 },
          {
            transform: `translate3d(${deltaX * 0.72}px, ${deltaY * 0.72}px, 0) scale(1.05)`,
            opacity: 0.9,
            offset: 0.38,
          },
          {
            transform: `translate3d(${deltaX * 0.94}px, ${deltaY * 0.94}px, 0) scale(1)`,
            opacity: 0.12,
            offset: 0.68,
          },
          {
            transform: `translate3d(${deltaX}px, ${deltaY}px, 0) scale(1)`,
            opacity: 0,
          },
        ],
        { duration: 700, easing: 'cubic-bezier(0.22, 1, 0.36, 1)' }
      )
      const removeGhost = () => ghost.remove()
      animation.addEventListener('finish', removeGhost, { once: true })
      animation.addEventListener('cancel', removeGhost, { once: true })
    }
  }, [admittedPills])
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
  const activeCount = live?.active ?? 0
  const liveStatus = admissionLiveStatus(activeCount, waitingCount)
  let liveStatusLabel = t('Ready')
  let liveStatusVariant: StatusVariant = 'neutral'
  let liveStatusPulse = false
  if (liveStatus === 'running') {
    liveStatusLabel = t('Running')
    liveStatusVariant = 'success'
    liveStatusPulse = true
  } else if (liveStatus === 'waiting') {
    liveStatusLabel = t('Waiting')
    liveStatusVariant = 'warning'
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
            {liveStatus === 'running' && waitingCount > 0 && (
              <StatusBadge
                label={`${t('Queued')} ${formatCount(waitingCount)}`}
                variant='warning'
                size='sm'
                pulse
                copyable={false}
              />
            )}
            <div className='ml-auto'>
              <ChannelAdmissionLiveRefreshSelect
                seconds={liveRefreshSeconds}
                onChange={handleLiveRefreshChange}
              />
            </div>
          </header>
          <div className='grid gap-3 p-3 sm:p-4 lg:grid-cols-[220px_minmax(0,1fr)]'>
            <div className='bg-muted/40 border-border/50 flex flex-col rounded-lg border p-3'>
              <div className='text-muted-foreground text-[10px] font-medium'>
                {t('Concurrency')}
              </div>
              <div className='mt-1 font-mono text-3xl font-semibold tabular-nums'>
                <span
                  key={live?.active}
                  className='admission-count-bump inline-block'
                >
                  {formatCount(live?.active)}
                </span>
                {limit > 0 && (
                  <span className='text-muted-foreground text-xl'>
                    {' / '}
                    {formatCount(limit)}
                  </span>
                )}
              </div>
              <div className='border-border/50 mt-3 border-t pt-3 lg:mt-auto'>
                {activeUsers.length > 0 ? (
                  <div className='mt-1.5 flex flex-wrap gap-1.5'>
                    {activeUsers.flatMap((user) => {
                      const name =
                        user.username || `${t('User')} #${user.user_id}`
                      return Array.from({ length: user.count }, (_, index) => {
                        const pillKey = `${user.user_id}-${index}`
                        const justAdmitted = admittedPills.has(pillKey)
                        return (
                          <div
                            key={pillKey}
                            data-admission-user={user.user_id}
                            ref={(element) => {
                              if (element) {
                                activePillRefs.current.set(pillKey, element)
                              } else {
                                activePillRefs.current.delete(pillKey)
                              }
                            }}
                            className={cn(
                              'bg-background/70 border-border/40 inline-flex items-center rounded-full border px-2 py-0.5 text-xs',
                              justAdmitted && 'admission-row-admitted'
                            )}
                          >
                            <span className='max-w-32 truncate font-medium'>
                              {name}
                            </span>
                          </div>
                        )
                      })
                    })}
                  </div>
                ) : (
                  <span className='text-muted-foreground mt-1 block text-xs'>
                    —
                  </span>
                )}
              </div>
            </div>

            <div className='flex min-w-0 flex-col gap-3'>
              <div className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
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

              <div className='bg-muted/30 border-border/60 flex flex-1 flex-col rounded-xl border p-3'>
                <header className='flex items-center gap-2'>
                  <span className='text-xs font-medium'>
                    {t('Queued requests')}
                  </span>
                  <span className='text-muted-foreground ml-auto text-xs tabular-nums'>
                    {waitingRequests.length}
                  </span>
                </header>
                {waitingRequests.length > 0 ? (
                  <ul className='mt-2 flex flex-wrap gap-1.5'>
                    {waitingRequests.map((request) => {
                      const displayName =
                        request.username || `${t('User')} #${request.user_id}`
                      const meta: string[] = []
                      if (request.username) {
                        meta.push(`#${request.user_id}`)
                      }
                      if (request.priority !== 0) {
                        meta.push(`${t('Priority')} ${request.priority}`)
                      }
                      return (
                        <li
                          key={request.id}
                          ref={(element) => {
                            if (element) {
                              queuedRowRefs.current.set(
                                request.user_id,
                                element
                              )
                            } else {
                              queuedRowRefs.current.delete(request.user_id)
                            }
                          }}
                          className='admission-queued-enter bg-background/70 border-border/40 flex w-full items-center justify-between gap-2 rounded-lg border px-2.5 py-1.5 sm:w-56'
                        >
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
                          <span className='text-muted-foreground shrink-0 text-right text-[11px] tabular-nums'>
                            {formatAdmissionWaitMs(request.wait_ms)}
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
          <div className='grid grid-cols-2 gap-2 p-3 sm:grid-cols-3 sm:p-4 lg:grid-cols-4 xl:grid-cols-8'>
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
              label={t('Priority grants')}
              value={formatCount(stats?.priority_grants)}
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
              { key: 'queue_full', label: 'Queue full', color: '#f59e0b' },
              { key: 'timeouts', label: 'Timeouts', color: '#ef4444' },
              {
                key: 'soup_grants',
                label: 'Lower-priority grants',
                color: '#8b5cf6',
              },
              {
                key: 'priority_grants',
                label: 'Priority grants',
                color: '#10b981',
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
