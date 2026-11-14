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
import {
  Activity,
  ArrowLeft,
  BarChart3,
  CheckCircle2,
  Clock,
  Gauge,
  RefreshCw,
} from 'lucide-react'
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
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { IconBadge, type IconBadgeTone } from '@/components/ui/icon-badge'
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { toIntlLocale } from '@/i18n/languages'
import { formatCompactNumber } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { getAllChannels, getChannel, getChannelAdmissionStats } from '../api'
import {
  DEFAULT_ADMISSION_LIVE_REFRESH_SECONDS,
  admissionLiveStatus,
  admissionRpmBlocked,
  effectiveAdmissionControl,
  formatAdmissionBucketWidth,
  formatAdmissionWaitMs,
  hasAdmissionMonitor,
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

function MetricCell(props: {
  label: string
  value: string | number
  valueClassName?: string
}) {
  return (
    <div className='bg-muted/40 rounded-xl px-3 py-2'>
      <div className='text-muted-foreground truncate text-[11px] font-medium'>
        {props.label}
      </div>
      <div
        className={cn(
          'mt-1 truncate font-mono text-sm font-semibold tabular-nums',
          props.valueClassName
        )}
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
      const leftQueued = hasAdmissionMonitor(left) ? 0 : 1
      const rightQueued = hasAdmissionMonitor(right) ? 0 : 1
      if (leftQueued !== rightQueued) {
        return leftQueued - rightQueued
      }
      return left.name.localeCompare(right.name)
    })
  }, [channels])
  const fallbackChannelId =
    sortedChannels.find((channel) => hasAdmissionMonitor(channel))?.id ??
    sortedChannels[0]?.id ??
    DEFAULT_CHANNEL_ID
  const channelId = props.initialChannelId ?? fallbackChannelId
  const selectedChannelQuery = useQuery({
    queryKey: ['channel-admission-monitor-channel', channelId],
    queryFn: async () => requireServerSuccess(await getChannel(channelId)).data,
    enabled: channelId > 0,
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
    selectedChannelQuery.data ??
    sortedChannels.find((channel) => channel.id === channelId)
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
  const rpmLimit = selectedChannel?.rpm_limit ?? 0
  const rpm = live?.rpm ?? 0
  const rpmUsers = live?.rpm_users ?? []
  const userRpmLimits = new Map(
    (selectedChannel?.user_rate_limits ?? []).map((entry) => [
      entry.user_id,
      entry.rpm_limit ?? 0,
    ])
  )
  const rpmConfigured =
    rpmLimit > 0 ||
    [...userRpmLimits.values()].some((userLimit) => userLimit > 0)
  const rpmBlocked = admissionRpmBlocked(rpm, rpmLimit, rpmUsers, userRpmLimits)
  let rpmLabel = '—'
  if (rpmConfigured) {
    rpmLabel = formatCount(rpm)
    if (rpmLimit > 0) {
      rpmLabel = `${formatCount(rpm)} / ${formatCount(rpmLimit)}`
    }
  }
  const rpmRows = (() => {
    const rows = new Map<
      number,
      { user_id: number; username?: string; count: number }
    >()
    for (const user of rpmUsers) {
      rows.set(user.user_id, { ...user })
    }
    for (const [userId, userLimit] of userRpmLimits) {
      if (userLimit > 0 && !rows.has(userId)) {
        rows.set(userId, {
          user_id: userId,
          username:
            (selectedChannel?.user_rate_limits ?? []).find(
              (entry) => entry.user_id === userId
            )?.username ?? undefined,
          count: 0,
        })
      }
    }
    return [...rows.values()].sort(
      (left, right) => right.count - left.count || left.user_id - right.user_id
    )
  })()
  let rpmPercent = 0
  if (rpmLimit > 0) {
    rpmPercent = Math.min(100, Math.round((rpm / rpmLimit) * 100))
  } else {
    for (const row of rpmRows) {
      const userLimit = userRpmLimits.get(row.user_id) ?? 0
      if (userLimit > 0) {
        rpmPercent = Math.max(
          rpmPercent,
          Math.min(100, Math.round((row.count / userLimit) * 100))
        )
      }
    }
  }
  const hasQueue = selectedChannel ? hasAdmissionQueue(selectedChannel) : true
  const monitorEnabled = selectedChannel
    ? hasAdmissionMonitor(selectedChannel)
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
  let liveStatusDotClass = 'bg-muted-foreground/50'
  let showLiveStatusBadge = true
  if (hasQueue) {
    if (liveStatus === 'running') {
      liveStatusLabel = t('Running')
      liveStatusDotClass = 'bg-success animate-pulse'
    } else if (liveStatus === 'waiting') {
      liveStatusLabel = t('Waiting')
      liveStatusDotClass = 'bg-warning animate-pulse'
    }
  } else if (rpmBlocked) {
    showLiveStatusBadge = false
  } else if (rpm > 0) {
    liveStatusLabel = t('Running')
    liveStatusDotClass = 'bg-success animate-pulse'
  }
  let rpmMeterClass = 'bg-success'
  if (rpmBlocked || rpmPercent >= 100) {
    rpmMeterClass = 'bg-destructive'
  } else if (rpmPercent >= 80) {
    rpmMeterClass = 'bg-warning'
  }
  let concurrencyMeterClass = 'bg-success'
  if (waitingCount > 0 || (limit > 0 && activeCount >= limit)) {
    concurrencyMeterClass = 'bg-warning'
  }
  const concurrencyPercent =
    limit > 0 ? Math.min(100, Math.round((activeCount / limit) * 100)) : 0
  let liveHeaderTone: IconBadgeTone = 'neutral'
  if (rpmBlocked) {
    liveHeaderTone = 'destructive'
  } else if (hasQueue && (waitingCount > 0 || liveStatus === 'waiting')) {
    liveHeaderTone = 'warning'
  } else if ((hasQueue && activeCount > 0) || (!hasQueue && rpm > 0)) {
    liveHeaderTone = 'success'
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
    const renderRpmUserCard = (row: {
      user_id: number
      username?: string
      count: number
    }) => {
      const userLimit = userRpmLimits.get(row.user_id) ?? 0
      const name = row.username || `${t('User')} #${row.user_id}`
      const atLimit = userLimit > 0 && row.count >= userLimit
      let ratio = 0
      let barClass = 'bg-success'
      let sharePercent: number | null = null
      if (userLimit > 0) {
        ratio = Math.min(100, Math.round((row.count / userLimit) * 100))
        if (atLimit) {
          barClass = 'bg-destructive'
        } else if (ratio >= 80) {
          barClass = 'bg-warning'
        }
      } else if (rpmLimit > 0) {
        ratio = Math.min(100, Math.round((row.count / rpmLimit) * 100))
        sharePercent = ratio
        barClass = 'bg-primary/80'
      }
      return (
        <div
          key={row.user_id}
          className='bg-background/85 shrink-0 rounded-lg px-2.5 py-1.5 shadow-2xs'
        >
          <div className='flex items-center gap-2'>
            <span className='min-w-0 flex-1 truncate text-xs font-medium'>
              {name}
            </span>
            <span
              className={cn(
                'shrink-0 font-mono text-[11px] tabular-nums',
                atLimit ? 'text-destructive font-semibold' : 'text-foreground'
              )}
            >
              {formatCount(row.count)}
              {userLimit > 0 && (
                <span className='text-muted-foreground'>
                  {' / '}
                  {formatCount(userLimit)}
                </span>
              )}
              {sharePercent !== null && (
                <span className='text-muted-foreground ml-1 text-[10px]'>
                  {sharePercent}%
                </span>
              )}
            </span>
          </div>
          {(userLimit > 0 || rpmLimit > 0) && (
            <Progress
              value={ratio}
              aria-label={name}
              className='mt-1.5 w-full'
              trackClassName='h-1 bg-foreground/10'
              indicatorClassName={barClass}
            />
          )}
        </div>
      )
    }

    content = (
      <>
        {!monitorEnabled && (
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
          <header className='flex flex-wrap items-center gap-2 border-b px-4 py-3 sm:px-5'>
            <IconBadge tone={liveHeaderTone} size='sm'>
              <Activity />
            </IconBadge>
            <h2 className='text-sm font-semibold'>{t('Live')}</h2>
            {showLiveStatusBadge && (
              <Badge variant='outline' className='gap-1.5 font-normal'>
                <span
                  aria-hidden='true'
                  className={cn(
                    'size-1.5 shrink-0 rounded-full',
                    liveStatusDotClass
                  )}
                />
                {liveStatusLabel}
              </Badge>
            )}
            {hasQueue && liveStatus === 'running' && waitingCount > 0 && (
              <Badge variant='warning' className='tabular-nums'>
                {t('Queued')} {formatCount(waitingCount)}
              </Badge>
            )}
            {rpmBlocked && (
              <Badge variant='destructive'>{t('RPM limit reached')}</Badge>
            )}
            <div className='ml-auto'>
              <ChannelAdmissionLiveRefreshSelect
                seconds={liveRefreshSeconds}
                onChange={handleLiveRefreshChange}
              />
            </div>
          </header>
          {hasQueue || !rpmConfigured ? (
            <div className='grid lg:h-[288px] lg:grid-cols-[256px_minmax(0,1fr)]'>
              <div className='flex min-h-0 flex-col p-4 max-lg:border-b sm:p-5 lg:border-r'>
                <div className='text-muted-foreground text-xs font-medium'>
                  {t('Concurrency')}
                </div>
                <div className='mt-1 font-mono text-3xl font-semibold tracking-tight tabular-nums'>
                  <span
                    key={live?.active}
                    className='admission-count-bump inline-block'
                  >
                    {formatCount(live?.active)}
                  </span>
                  {limit > 0 && (
                    <span className='text-muted-foreground text-xl font-medium'>
                      {' / '}
                      {formatCount(limit)}
                    </span>
                  )}
                </div>
                {limit > 0 && limit <= 12 && (
                  <div
                    aria-hidden='true'
                    className='mt-2.5 grid w-full gap-1'
                    style={{
                      gridTemplateColumns: `repeat(${limit}, minmax(0, 1fr))`,
                    }}
                  >
                    {Array.from({ length: limit }, (_, index) => (
                      <span
                        key={index}
                        className={cn(
                          'h-2 rounded-full transition-colors',
                          index < activeCount
                            ? concurrencyMeterClass
                            : 'bg-foreground/10'
                        )}
                      />
                    ))}
                  </div>
                )}
                {limit > 12 && (
                  <Progress
                    value={concurrencyPercent}
                    aria-label={t('Concurrency')}
                    className='mt-2.5 w-full'
                    trackClassName='h-2 bg-foreground/10'
                    indicatorClassName={concurrencyMeterClass}
                  />
                )}
                <div className='border-border/60 mt-3 min-h-0 flex-1 border-t pt-3'>
                  <div className='hover-scrollbar flex min-h-[26px] flex-wrap content-start items-center gap-1.5 lg:max-h-full lg:overflow-y-auto'>
                    {activeUsers.length > 0 ? (
                      <>
                        {activeUsers.flatMap((user) => {
                          const name =
                            user.username || `${t('User')} #${user.user_id}`
                          return Array.from(
                            { length: user.count },
                            (_, index) => {
                              const pillKey = `${user.user_id}-${index}`
                              const justAdmitted = admittedPills.has(pillKey)
                              return (
                                <div
                                  key={pillKey}
                                  data-admission-user={user.user_id}
                                  ref={(element) => {
                                    if (element) {
                                      activePillRefs.current.set(
                                        pillKey,
                                        element
                                      )
                                    } else {
                                      activePillRefs.current.delete(pillKey)
                                    }
                                  }}
                                  className={cn(
                                    'bg-background/90 border-border/60 inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs shadow-2xs',
                                    justAdmitted && 'admission-row-admitted'
                                  )}
                                >
                                  <span className='max-w-36 truncate font-medium'>
                                    {name}
                                  </span>
                                </div>
                              )
                            }
                          )
                        })}
                      </>
                    ) : (
                      <span className='text-muted-foreground text-xs'>—</span>
                    )}
                  </div>
                </div>
              </div>

              <div className='flex min-h-0 min-w-0 flex-col gap-3 p-4'>
                <div className='grid grid-cols-2 gap-2.5 sm:grid-cols-4'>
                  <MetricCell
                    label={t('Waiting')}
                    value={waitingLabel}
                    valueClassName={
                      waitingCount > 0 ? 'text-warning' : undefined
                    }
                  />
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
                  <TooltipProvider delay={150}>
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <div
                            tabIndex={0}
                            className='bg-muted/40 focus-visible:ring-ring/50 flex flex-col justify-between rounded-xl px-3 py-2 text-left outline-none focus-visible:ring-2'
                          />
                        }
                      >
                        <div className='text-muted-foreground truncate text-[11px] font-medium'>
                          {t('RPM')}
                        </div>
                        <div
                          className={cn(
                            'mt-1 truncate font-mono text-sm font-semibold tabular-nums',
                            rpmBlocked && 'text-destructive'
                          )}
                          title={rpmLabel}
                        >
                          {rpmLabel}
                        </div>
                        {rpmConfigured && (
                          <Progress
                            value={rpmPercent}
                            aria-label={t('RPM')}
                            className='mt-1.5 w-full'
                            trackClassName='h-1 bg-foreground/10'
                            indicatorClassName={rpmMeterClass}
                          />
                        )}
                      </TooltipTrigger>
                      <TooltipContent side='top' className='max-w-xs'>
                        <div className='flex flex-col gap-1 text-xs'>
                          <div className='font-medium'>{t('RPM')}</div>
                          {rpmUsers.length > 0 ? (
                            rpmUsers.map((user) => {
                              const userLimit =
                                userRpmLimits.get(user.user_id) ?? 0
                              const name =
                                user.username || `${t('User')} #${user.user_id}`
                              return (
                                <div
                                  key={user.user_id}
                                  className='flex items-center justify-between gap-3'
                                >
                                  <span className='truncate'>{name}</span>
                                  <span className='shrink-0 font-mono tabular-nums'>
                                    {formatCount(user.count)}
                                    {userLimit > 0
                                      ? ` / ${formatCount(userLimit)}`
                                      : ''}
                                  </span>
                                </div>
                              )
                            })
                          ) : (
                            <div className='text-muted-foreground'>
                              {t('No RPM usage this minute')}
                            </div>
                          )}
                        </div>
                      </TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                </div>

                <div
                  className={cn(
                    'grid min-h-0 flex-1 gap-3',
                    rpmConfigured && 'lg:grid-cols-[minmax(0,1fr)_264px]'
                  )}
                >
                  <div className='bg-muted/35 flex min-h-0 flex-col rounded-xl p-3'>
                    <header className='flex items-center gap-2'>
                      <span className='text-muted-foreground text-xs font-medium'>
                        {t('Queued requests')}
                      </span>
                      <span className='text-muted-foreground ml-auto font-mono text-xs tabular-nums'>
                        {waitingRequests.length}
                      </span>
                    </header>
                    <div className='hover-scrollbar mt-2 flex min-h-0 flex-1 flex-col overflow-y-auto pr-0.5'>
                      {waitingRequests.length > 0 ? (
                        <ul className='flex flex-wrap gap-1.5'>
                          {waitingRequests.map((request) => {
                            const displayName =
                              request.username ||
                              `${t('User')} #${request.user_id}`
                            const priorityLabel =
                              request.priority !== 0
                                ? `${t('Priority')} ${request.priority}`
                                : ''
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
                                    queuedRowRefs.current.delete(
                                      request.user_id
                                    )
                                  }
                                }}
                                className='admission-queued-enter bg-background/85 flex w-full items-center gap-2 rounded-full px-3 py-1 shadow-2xs sm:w-64'
                              >
                                <span className='min-w-0 flex-1 truncate text-xs font-medium'>
                                  {displayName}
                                </span>
                                {priorityLabel && (
                                  <Badge
                                    variant='secondary'
                                    className='h-4 px-1.5 text-[10px]'
                                  >
                                    {priorityLabel}
                                  </Badge>
                                )}
                                <span className='text-muted-foreground shrink-0 font-mono text-[11px] tabular-nums'>
                                  {formatAdmissionWaitMs(request.wait_ms)}
                                </span>
                              </li>
                            )
                          })}
                        </ul>
                      ) : (
                        <div className='text-muted-foreground m-auto flex items-center gap-1.5 text-xs'>
                          <CheckCircle2 className='text-success size-3.5' />
                          <span>{t('No users are waiting')}</span>
                        </div>
                      )}
                    </div>
                  </div>
                  {rpmConfigured && (
                    <div className='bg-muted/35 flex min-h-0 flex-col rounded-xl p-3'>
                      <header className='flex items-center gap-2'>
                        <span className='text-muted-foreground text-xs font-medium'>
                          {t('RPM users')}
                        </span>
                        <span className='text-muted-foreground ml-auto font-mono text-xs tabular-nums'>
                          {rpmRows.length}
                        </span>
                      </header>
                      <div className='hover-scrollbar mt-2 flex min-h-0 flex-1 flex-col gap-1.5 overflow-y-auto pr-0.5'>
                        {rpmRows.length > 0 ? (
                          rpmRows.map(renderRpmUserCard)
                        ) : (
                          <div className='text-muted-foreground m-auto text-xs'>
                            {t('No RPM usage this minute')}
                          </div>
                        )}
                      </div>
                    </div>
                  )}
                </div>
              </div>
            </div>
          ) : (
            <div className='grid lg:h-[288px] lg:grid-cols-[256px_minmax(0,1fr)]'>
              <div className='flex min-h-0 flex-col p-4 max-lg:border-b sm:p-5 lg:border-r'>
                <div className='text-muted-foreground text-xs font-medium'>
                  {t('RPM')}
                </div>
                <div
                  className={cn(
                    'mt-1 font-mono text-3xl font-semibold tracking-tight tabular-nums',
                    rpmBlocked && 'text-destructive'
                  )}
                >
                  <span>{formatCount(rpm)}</span>
                  {rpmLimit > 0 && (
                    <span className='text-muted-foreground text-xl font-medium'>
                      {' / '}
                      {formatCount(rpmLimit)}
                    </span>
                  )}
                </div>
                <Progress
                  value={rpmPercent}
                  aria-label={t('RPM')}
                  className='mt-2.5 w-full'
                  trackClassName='h-2 bg-foreground/10'
                  indicatorClassName={rpmMeterClass}
                />
                {(rpmLimit > 0 || rpmPercent > 0) && (
                  <div className='text-muted-foreground mt-1.5 font-mono text-xs tabular-nums'>
                    {rpmPercent}%
                  </div>
                )}
                <div className='border-border/60 text-muted-foreground mt-auto flex flex-col gap-1.5 border-t pt-3 text-xs'>
                  <div className='flex items-center justify-between gap-2'>
                    <span className='truncate'>{t('Concurrency limit')}</span>
                    <span className='shrink-0 font-mono'>{t('Unlimited')}</span>
                  </div>
                </div>
              </div>

              <div className='flex min-h-0 min-w-0 flex-col gap-3 p-4'>
                <div className='grid grid-cols-2 gap-2.5 sm:grid-cols-4'>
                  <MetricCell
                    label={t('RPM limit')}
                    value={
                      rpmLimit > 0 ? formatCount(rpmLimit) : t('Unlimited')
                    }
                  />
                  <MetricCell
                    label={t('RPM users')}
                    value={formatCount(rpmRows.length)}
                  />
                  <MetricCell
                    label={t('Concurrency limit')}
                    value={t('Unlimited')}
                  />
                  <MetricCell
                    label={t('Status')}
                    value={rpmBlocked ? t('Limited') : t('Healthy')}
                    valueClassName={
                      rpmBlocked ? 'text-destructive' : 'text-success'
                    }
                  />
                </div>

                <div className='bg-muted/35 flex min-h-0 flex-1 flex-col rounded-xl p-3'>
                  <header className='flex items-center gap-2'>
                    <span className='text-muted-foreground text-xs font-medium'>
                      {t('RPM users')}
                    </span>
                    <span className='text-muted-foreground ml-auto font-mono text-xs tabular-nums'>
                      {rpmRows.length}
                    </span>
                  </header>
                  <div className='hover-scrollbar mt-2 flex min-h-0 flex-1 flex-col overflow-y-auto pr-0.5'>
                    {rpmRows.length > 0 ? (
                      <div
                        className={cn(
                          'grid shrink-0 content-start gap-2',
                          rpmRows.length <= 2
                            ? 'sm:grid-cols-2'
                            : 'sm:grid-cols-2 lg:grid-cols-3'
                        )}
                      >
                        {rpmRows.map(renderRpmUserCard)}
                      </div>
                    ) : (
                      <div className='text-muted-foreground m-auto text-xs'>
                        {t('No RPM usage this minute')}
                      </div>
                    )}
                  </div>
                </div>
              </div>
            </div>
          )}
        </section>

        {hasQueue || !rpmConfigured ? (
          <>
            <section className='bg-card overflow-hidden rounded-2xl border shadow-xs'>
              <header className='flex flex-wrap items-center gap-2 border-b px-4 py-3 sm:px-5'>
                <IconBadge tone='info' size='sm'>
                  <Clock />
                </IconBadge>
                <h2 className='text-sm font-semibold'>{t('Selected range')}</h2>
                <span className='text-muted-foreground ml-auto font-mono text-xs tabular-nums'>
                  {rangeLabel} ·{' '}
                  {t('Aggregated every {{interval}}', {
                    interval: bucketWidth,
                  })}
                </span>
              </header>
              <div className='grid grid-cols-2 gap-2.5 p-4 sm:grid-cols-4 sm:p-5'>
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
                  valueClassName={
                    (stats?.timeouts ?? 0) > 0 ? 'text-destructive' : undefined
                  }
                />
                <MetricCell
                  label={t('Queue full')}
                  value={formatCount(stats?.queue_full)}
                  valueClassName={
                    (stats?.queue_full ?? 0) > 0 ? 'text-warning' : undefined
                  }
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

            <div className='grid gap-4 lg:grid-cols-2'>
              <ChannelAdmissionTrendChart
                title='Admissions'
                icon={Activity}
                iconTone='chart-1'
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
                icon={Clock}
                iconTone='chart-2'
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
                icon={BarChart3}
                iconTone='chart-3'
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
        ) : (
          <ChannelAdmissionTrendChart
            title='Admissions'
            icon={Activity}
            iconTone='chart-1'
            filled
            summary={
              <Badge
                variant='secondary'
                className='font-mono font-normal tabular-nums'
              >
                {t('Grants')} {formatCount(stats?.grants)}
              </Badge>
            }
            points={stats?.minute_series ?? []}
            bucketSeconds={stats?.bucket_seconds ?? 60}
            series={[{ key: 'grants', label: 'Grants', color: '#0ea5e9' }]}
          />
        )}

        <ChannelAdmissionSettingsPanel channel={selectedChannel} />
      </>
    )
  }

  return (
    <main className='h-full overflow-y-auto'>
      <div
        className={cn(
          'mx-auto flex w-full max-w-7xl flex-col gap-4 p-4 md:p-6',
          !hasQueue && 'pb-0 md:pb-0'
        )}
      >
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
            <div className='flex flex-col gap-1'>
              <h1 className='text-xl font-semibold tracking-tight'>
                {t('Channel Admission')}
              </h1>
              {selectedChannel ? (
                <div className='text-muted-foreground flex flex-wrap items-center gap-1.5 text-sm'>
                  <span className='text-foreground font-medium'>
                    {selectedChannel.name}
                  </span>
                  <span className='font-mono text-xs'>
                    #{selectedChannel.id}
                  </span>
                  {hasQueue && (
                    <Badge
                      variant='secondary'
                      className='font-mono font-normal tabular-nums'
                    >
                      {t('Concurrency')}{' '}
                      {limit > 0 ? formatCount(limit) : t('Per-user limits')}
                    </Badge>
                  )}
                  {rpmConfigured && (
                    <Badge
                      variant='secondary'
                      className='font-mono font-normal tabular-nums'
                    >
                      {t('RPM')}{' '}
                      {rpmLimit > 0
                        ? formatCount(rpmLimit)
                        : t('Per-user limits')}
                    </Badge>
                  )}
                  {(selectedChannel.priority ?? 0) !== 0 && (
                    <Badge
                      variant='outline'
                      className='font-mono font-normal tabular-nums'
                    >
                      {t('Priority')} {selectedChannel.priority}
                    </Badge>
                  )}
                </div>
              ) : (
                <p className='text-muted-foreground text-sm'>
                  {t('Current queue and seven-day admission history')}
                </p>
              )}
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
            <div className='flex w-full flex-col gap-1 sm:w-48'>
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
