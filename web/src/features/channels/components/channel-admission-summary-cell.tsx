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
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
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

import { getChannelAdmissionStats } from '../api'
import {
  admissionRpmBlocked,
  effectiveAdmissionControl,
  formatAdmissionWaitMs,
  hasAdmissionQueue,
} from '../lib/admission'
import type { Channel } from '../types'

const MAX_SEGMENTED_SLOTS = 8

function rpmUsageBarClass(ratio: number): string {
  if (ratio >= 1) {
    return 'bg-destructive'
  }
  if (ratio >= 0.8) {
    return 'bg-warning'
  }
  return 'bg-success'
}

function AdmissionUsageBar(props: { ratio: number; toneClass: string }) {
  const clamped = Math.max(0, Math.min(1, props.ratio))
  return (
    <span
      aria-hidden='true'
      data-slot='admission-usage-bar'
      className='bg-foreground/10 block h-1 w-full overflow-hidden rounded-full'
    >
      <span
        className={cn(
          'block h-full rounded-full transition-[width,background-color]',
          props.toneClass
        )}
        style={{ width: `${clamped * 100}%` }}
      />
    </span>
  )
}

function AdmissionSlotGauge(props: {
  active: number
  limit: number
  toneClass: string
}) {
  return (
    <span
      aria-hidden='true'
      data-slot='admission-slot-gauge'
      className='grid w-full gap-0.5'
      style={{
        gridTemplateColumns: `repeat(${props.limit}, minmax(0, 1fr))`,
      }}
    >
      {Array.from({ length: props.limit }, (_, index) => (
        <span
          key={index}
          data-occupied={index < props.active ? 'true' : 'false'}
          className={cn(
            'h-1 rounded-full transition-colors',
            index < props.active ? props.toneClass : 'bg-foreground/10'
          )}
        />
      ))}
    </span>
  )
}

export function ChannelAdmissionSummaryCell(props: { channel: Channel }) {
  const { t, i18n } = useTranslation()
  const channel = props.channel
  const statsQuery = useQuery({
    queryKey: ['channel-admission-summary', channel.id],
    queryFn: async () => {
      const end = Math.floor(Date.now() / 1000)
      const start = end - 60 * 60
      return requireServerSuccess(
        await getChannelAdmissionStats(channel.id, {
          start_timestamp: start,
          end_timestamp: end,
        })
      ).data
    },
    staleTime: 15_000,
    refetchInterval: 30_000,
    meta: { errorToast: false },
  })
  const stats = statsQuery.data
  const hasStats = stats !== undefined
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const formatCount = (value: number | undefined) =>
    value === undefined ? '—' : formatCompactNumber(value, locale)

  const concurrencyLimit = channel.concurrency_limit ?? 0
  const active = stats?.active ?? 0
  const activeCount = hasStats ? formatCount(active) : '—'
  const activeText =
    concurrencyLimit > 0
      ? `${activeCount}/${formatCount(concurrencyLimit)}`
      : activeCount
  const waiting = stats?.waiting ?? 0
  const waitingText = hasStats ? formatCount(waiting) : '—'
  const activeUsers = stats?.active_user_list ?? []
  const activePreview = activeUsers.slice(0, 5)

  const rpmLimit = channel.rpm_limit ?? 0
  const userLimits = channel.user_rate_limits ?? []
  const control = effectiveAdmissionControl(channel)
  const hasQueue = hasAdmissionQueue(channel)
  const rpm = stats?.rpm ?? 0
  const rpmUsers = stats?.rpm_users ?? []
  const userRpmLimits = new Map(
    userLimits.map((entry) => [entry.user_id, entry.rpm_limit ?? 0])
  )
  const rpmConfigured =
    rpmLimit > 0 ||
    [...userRpmLimits.values()].some((userLimit) => userLimit > 0)
  const rpmBlocked =
    hasStats && admissionRpmBlocked(rpm, rpmLimit, rpmUsers, userRpmLimits)

  const rpmCountText = hasStats ? formatCount(rpm) : '—'
  const rpmDisplay =
    rpmLimit > 0 ? `${rpmCountText}/${formatCount(rpmLimit)}` : rpmCountText

  let rpmRatio = 0
  if (hasStats) {
    if (rpmLimit > 0) {
      rpmRatio = Math.min(1, rpm / rpmLimit)
    } else {
      for (const user of rpmUsers) {
        const userLimit = userRpmLimits.get(user.user_id) ?? 0
        if (userLimit > 0) {
          rpmRatio = Math.max(rpmRatio, Math.min(1, user.count / userLimit))
        }
      }
    }
  }
  const rpmTone = statsQuery.isError
    ? 'bg-destructive'
    : rpmUsageBarClass(rpmBlocked ? 1 : rpmRatio)

  let concurrencyTone = 'bg-success'
  if (statsQuery.isError) {
    concurrencyTone = 'bg-destructive'
  } else if (!hasStats) {
    concurrencyTone = 'bg-muted-foreground/30'
  } else if (
    waiting > 0 ||
    (concurrencyLimit > 0 && active >= concurrencyLimit)
  ) {
    concurrencyTone = 'bg-warning'
  }

  let concurrencyMeter: ReactNode
  if (concurrencyLimit > 0 && concurrencyLimit <= MAX_SEGMENTED_SLOTS) {
    concurrencyMeter = (
      <AdmissionSlotGauge
        active={hasStats ? active : 0}
        limit={concurrencyLimit}
        toneClass={concurrencyTone}
      />
    )
  } else if (concurrencyLimit > MAX_SEGMENTED_SLOTS) {
    concurrencyMeter = (
      <AdmissionUsageBar
        ratio={hasStats ? Math.min(1, active / concurrencyLimit) : 0}
        toneClass={concurrencyTone}
      />
    )
  } else {
    concurrencyMeter = (
      <AdmissionUsageBar
        ratio={hasStats && active > 0 ? 1 : 0}
        toneClass={concurrencyTone}
      />
    )
  }

  const topRpmUser = hasQueue ? undefined : rpmUsers[0]
  const topRpmLimit = topRpmUser
    ? (userRpmLimits.get(topRpmUser.user_id) ?? 0)
    : 0
  let topRpmRatio = 0
  if (topRpmUser) {
    if (topRpmLimit > 0) {
      topRpmRatio = Math.min(1, topRpmUser.count / topRpmLimit)
    } else if (rpmLimit > 0) {
      topRpmRatio = Math.min(1, topRpmUser.count / rpmLimit)
    }
  }
  const topUserAtLimit =
    topRpmUser !== undefined &&
    topRpmLimit > 0 &&
    topRpmUser.count >= topRpmLimit

  let primaryTrack: ReactNode
  let secondaryTrack: ReactNode

  if (hasQueue) {
    primaryTrack = (
      <span className='flex w-full min-w-0 flex-col gap-1'>
        <span className='flex min-w-0 items-center justify-between gap-1.5'>
          <span className='flex min-w-0 items-center gap-1.5'>
            <span className='text-muted-foreground min-w-0 truncate text-xs! font-normal!'>
              {t('Concurrency')}
            </span>
          </span>
          <span className='flex shrink-0 items-center gap-1.5'>
            {waiting > 0 && (
              <Badge
                variant='warning'
                className='h-4 px-1.5 py-0 text-[10px]! leading-none font-medium! tabular-nums'
              >
                {t('Queued')} {waitingText}
              </Badge>
            )}
            <span
              className={cn(
                'font-mono! text-xs! font-semibold! tabular-nums',
                statsQuery.isError ? 'text-destructive' : 'text-foreground'
              )}
            >
              {activeText}
            </span>
          </span>
        </span>
        {concurrencyMeter}
      </span>
    )

    secondaryTrack = (
      <span className='flex w-full min-w-0 flex-col gap-1'>
        <span className='flex min-w-0 items-center justify-between gap-1.5'>
          <span className='text-muted-foreground min-w-0 truncate text-xs! font-normal!'>
            {t('RPM')}
          </span>
          <span className='flex shrink-0 items-center gap-1.5'>
            {rpmBlocked && (
              <Badge
                variant='destructive'
                className='h-4 px-1.5 py-0 text-[10px]! leading-none font-medium!'
              >
                {t('Limited')}
              </Badge>
            )}
            <span
              className={cn(
                'font-mono! text-xs! font-semibold! tabular-nums',
                rpmBlocked && 'text-destructive',
                !rpmBlocked && !rpmConfigured && 'text-muted-foreground',
                !rpmBlocked && rpmConfigured && 'text-foreground'
              )}
            >
              {rpmConfigured ? rpmDisplay : '—'}
            </span>
          </span>
        </span>
        <AdmissionUsageBar
          ratio={rpmConfigured ? rpmRatio : 0}
          toneClass={rpmTone}
        />
      </span>
    )
  } else {
    primaryTrack = (
      <span className='flex w-full min-w-0 flex-col gap-1'>
        <span className='flex min-w-0 items-center justify-between gap-1.5'>
          <span className='flex min-w-0 items-center gap-1.5'>
            <span className='text-muted-foreground min-w-0 truncate text-xs! font-normal!'>
              {t('RPM')}
            </span>
          </span>
          <span className='flex shrink-0 items-center gap-1.5'>
            {rpmBlocked && (
              <Badge
                variant='destructive'
                className='h-4 px-1.5 py-0 text-[10px]! leading-none font-medium!'
              >
                {t('Limited')}
              </Badge>
            )}
            <span
              className={cn(
                'font-mono! text-xs! font-semibold! tabular-nums',
                statsQuery.isError || rpmBlocked
                  ? 'text-destructive'
                  : 'text-foreground'
              )}
            >
              {rpmDisplay}
            </span>
          </span>
        </span>
        <AdmissionUsageBar ratio={rpmRatio} toneClass={rpmTone} />
      </span>
    )

    if (topRpmUser) {
      secondaryTrack = (
        <span className='flex w-full min-w-0 flex-col gap-1'>
          <span className='flex min-w-0 items-center justify-between gap-1.5'>
            <span className='flex min-w-0 items-center gap-1'>
              <span className='text-muted-foreground min-w-0 truncate text-xs! font-normal!'>
                {topRpmUser.username || `${t('User')} #${topRpmUser.user_id}`}
              </span>
              {rpmUsers.length > 1 && (
                <span className='text-muted-foreground/75 shrink-0 font-mono! text-[10px]! font-normal! tabular-nums'>
                  +{rpmUsers.length - 1}
                </span>
              )}
            </span>
            <span
              className={cn(
                'shrink-0 font-mono! text-xs! font-semibold! tabular-nums',
                topUserAtLimit ? 'text-destructive' : 'text-foreground'
              )}
            >
              {formatCount(topRpmUser.count)}
              {topRpmLimit > 0 ? `/${formatCount(topRpmLimit)}` : ''}
            </span>
          </span>
          <AdmissionUsageBar
            ratio={topRpmRatio}
            toneClass={rpmUsageBarClass(topUserAtLimit ? 1 : topRpmRatio)}
          />
        </span>
      )
    } else {
      secondaryTrack = (
        <span className='flex w-full min-w-0 flex-col gap-1'>
          <span className='flex min-w-0 items-center justify-between gap-1.5'>
            <span className='text-muted-foreground min-w-0 truncate text-xs! font-normal!'>
              {t('No RPM usage this minute')}
            </span>
            <span className='text-muted-foreground shrink-0 font-mono! text-xs! font-semibold! tabular-nums'>
              {hasStats ? '0' : '—'}
            </span>
          </span>
          <AdmissionUsageBar ratio={0} toneClass='bg-success' />
        </span>
      )
    }
  }

  let soupSummary: string
  if (control.soup_mode === 'off') {
    soupSummary = t('Off')
  } else if (control.soup_mode === 'ratio') {
    soupSummary = `${t('Admission ratio')} · ${control.soup_ratio_percent}%`
  } else {
    soupSummary = `${t('Interval')} · ${formatAdmissionWaitMs(control.soup_after_ms)} / ${formatAdmissionWaitMs(control.soup_interval_ms)}`
  }

  return (
    <TooltipProvider delay={150}>
      <Tooltip>
        <TooltipTrigger render={<span className='block w-full min-w-0' />}>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='border-border/60 bg-muted/25 hover:border-border hover:bg-muted/60 h-auto w-full max-w-full justify-start rounded-lg border px-2.5 py-1.5 text-left whitespace-normal transition-colors'
            aria-label={`${t('Admission Monitor')}: ${hasQueue ? `${t('Concurrency')} ${activeText}, ${t('Queued')} ${waitingText}` : `${t('RPM')} ${rpmDisplay}`}`}
            title={t('Admission Monitor')}
            onClick={(event) => event.stopPropagation()}
            render={
              <Link
                to='/channels/admission'
                search={{ channel_id: channel.id }}
              />
            }
          >
            <span
              data-testid='admission-summary'
              data-table-text='secondary'
              className='flex w-full min-w-0 flex-col gap-1.5 text-xs! leading-none font-normal'
            >
              {primaryTrack}
              {secondaryTrack}
            </span>
          </Button>
        </TooltipTrigger>
        <TooltipContent side='top' className='max-w-xs'>
          <div className='flex flex-col gap-1.5 text-xs'>
            {statsQuery.isError && (
              <div className='text-destructive'>
                {t('Failed to load channel admission statistics')}
              </div>
            )}
            {rpmBlocked && (
              <div className='text-destructive font-medium'>
                {t('RPM limit reached')}
              </div>
            )}
            <div>
              {t('Wait timeout')}:{' '}
              {formatAdmissionWaitMs(control.wait_timeout_ms)}
            </div>
            <div>
              {t('Lower-priority release')}: {soupSummary}
            </div>
            {activePreview.length > 0 && (
              <>
                <Separator />
                <div className='flex flex-col gap-0.5'>
                  <div className='font-medium'>{t('Active users')}</div>
                  {activePreview.map((user) => (
                    <div
                      key={user.user_id}
                      className='flex items-center justify-between gap-3'
                    >
                      <span className='truncate'>
                        {user.username || `${t('User')} #${user.user_id}`}
                      </span>
                      {user.count > 1 && (
                        <span className='text-muted-foreground shrink-0 font-mono tabular-nums'>
                          ×{user.count}
                        </span>
                      )}
                    </div>
                  ))}
                  {activeUsers.length > activePreview.length && (
                    <div className='text-muted-foreground'>
                      +{activeUsers.length - activePreview.length}
                    </div>
                  )}
                </div>
              </>
            )}
            {rpmUsers.length > 0 && (
              <>
                <Separator />
                <div className='flex flex-col gap-0.5'>
                  <div className='font-medium'>{t('RPM users')}</div>
                  {rpmUsers.slice(0, 5).map((user) => {
                    const userLimit = userRpmLimits.get(user.user_id) ?? 0
                    return (
                      <div
                        key={user.user_id}
                        className='flex items-center justify-between gap-3'
                      >
                        <span className='truncate'>
                          {user.username || `${t('User')} #${user.user_id}`}
                        </span>
                        <span className='text-muted-foreground shrink-0 font-mono tabular-nums'>
                          {formatCount(user.count)}
                          {userLimit > 0 ? ` / ${formatCount(userLimit)}` : ''}
                        </span>
                      </div>
                    )
                  })}
                  {rpmUsers.length > 5 && (
                    <div className='text-muted-foreground'>
                      +{rpmUsers.length - 5}
                    </div>
                  )}
                </div>
              </>
            )}
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
