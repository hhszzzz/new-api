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
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
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
  admissionSparklineBars,
  effectiveAdmissionControl,
  formatAdmissionWaitMs,
  formatUserAdmissionLimit,
} from '../lib/admission'
import type { Channel } from '../types'

export function ChannelAdmissionSummaryCell(props: { channel: Channel }) {
  const { t, i18n } = useTranslation()
  const channel = props.channel
  const statsQuery = useQuery({
    queryKey: ['channel-admission-summary', channel.id],
    queryFn: async () => {
      // One hour of minute buckets feeds the compact sparkline, while the same
      // response carries the live active/waiting state.
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
  const activeCount = hasStats ? formatCount(stats.active) : '—'
  const activeText =
    concurrencyLimit > 0
      ? `${activeCount}/${formatCount(concurrencyLimit)}`
      : activeCount
  const waiting = stats?.waiting ?? 0
  const waitingText = hasStats ? formatCount(waiting) : '—'
  const waitingUsers = stats?.waiting_users ?? []
  const queuedPreview = waitingUsers.slice(0, 5)
  const activeUsers = stats?.active_user_list ?? []
  const activePreview = activeUsers.slice(0, 5)
  const sparkBars = admissionSparklineBars(stats?.minute_series ?? [], 24)
  const sparkMax = Math.max(1, ...sparkBars.map((bar) => bar.grants))
  let statusDot = 'bg-emerald-500'
  if (statsQuery.isError) {
    statusDot = 'bg-destructive'
  } else if (!hasStats) {
    statusDot = 'bg-muted-foreground/40'
  } else if (waiting > 0) {
    statusDot = 'bg-amber-500'
  }
  const rpmLimit = channel.rpm_limit ?? 0
  const control = effectiveAdmissionControl(channel)
  const userLimits = channel.user_rate_limits ?? []
  const userLimitSummary = userLimits
    .map((limit) => formatUserAdmissionLimit(limit, t))
    .join(', ')
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
        <TooltipTrigger render={<span className='block min-w-0' />}>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='h-auto min-h-9 max-w-full justify-start px-1 py-1 text-left whitespace-normal'
            aria-label={`${t('Admission Monitor')}: ${t('Concurrency')} ${activeText}, ${t('Queued')} ${waitingText}`}
            title={t('Admission Monitor')}
            onClick={(event) => event.stopPropagation()}
            render={
              <Link
                to='/channels/admission'
                search={{ channel_id: channel.id }}
              />
            }
          >
            <span className='grid min-w-0 gap-0.5 text-xs leading-tight'>
              <span className='flex min-w-0 items-center gap-1.5'>
                <span
                  aria-hidden='true'
                  className={cn('size-1.5 shrink-0 rounded-full', statusDot)}
                />
                <span className='truncate'>
                  {t('Concurrency')} {activeText} · {t('Queued')} {waitingText}
                </span>
              </span>
              {hasStats && sparkBars.length > 0 && (
                <span className='flex w-full flex-col gap-1'>
                  <span
                    aria-hidden='true'
                    data-testid='admission-sparkline'
                    className='flex h-4 w-full items-end gap-[2px]'
                  >
                    {sparkBars.map((bar) => {
                      let barClass = 'bg-muted-foreground/20'
                      if (bar.timeouts > 0 || bar.queueFull > 0) {
                        barClass = 'bg-destructive/80'
                      } else if (bar.waited > 0) {
                        barClass = 'bg-warning'
                      } else if (bar.grants > 0) {
                        barClass = 'bg-success/80'
                      }
                      return (
                        <span
                          key={bar.timestampMs}
                          className='flex h-full min-w-0 flex-1 items-end'
                        >
                          <span
                            data-testid='admission-spark-bar'
                            className={cn('w-full rounded-sm', barClass)}
                            style={{
                              height: `${Math.max(15, (bar.grants / sparkMax) * 100)}%`,
                            }}
                          />
                        </span>
                      )
                    })}
                  </span>
                  {waiting > 0 && (
                    <span className='text-muted-foreground text-[11px]'>
                      {t('Oldest wait')}:{' '}
                      {formatAdmissionWaitMs(stats?.oldest_wait_ms)}
                    </span>
                  )}
                </span>
              )}
            </span>
          </Button>
        </TooltipTrigger>
        <TooltipContent side='top' className='max-w-xs'>
          <div className='space-y-1 text-xs'>
            {statsQuery.isError && (
              <div className='text-destructive'>
                {t('Failed to load channel admission statistics')}
              </div>
            )}
            <div>
              {t('Concurrency limit')}:{' '}
              {concurrencyLimit > 0 ? concurrencyLimit : t('Unlimited')}
            </div>
            <div>
              {t('RPM limit')}: {rpmLimit > 0 ? rpmLimit : t('Unlimited')}
            </div>
            <div>
              {t('Priority')}: {channel.priority ?? 0}
            </div>
            <div>
              {t('Wait timeout')}:{' '}
              {formatAdmissionWaitMs(control.wait_timeout_ms)}
            </div>
            <div>
              {t('Lower-priority release')}: {soupSummary}
            </div>
            {userLimitSummary && (
              <div>
                {t('Per-user limits')}: {userLimitSummary}
              </div>
            )}
            {activePreview.length > 0 && (
              <div className='border-t pt-1'>
                <div className='font-medium'>{t('Active users')}</div>
                {activePreview.map((user) => (
                  <div
                    key={user.user_id}
                    className='flex items-center justify-between gap-3'
                  >
                    <span className='truncate'>
                      {user.username || `${t('User')} #${user.user_id}`}
                      {user.priority !== 0 && ` · P${user.priority}`}
                    </span>
                    {user.count > 1 && (
                      <span className='text-muted-foreground shrink-0 tabular-nums'>
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
            )}
            {queuedPreview.length > 0 && (
              <div className='border-t pt-1'>
                <div className='font-medium'>{t('Queued users')}</div>
                {queuedPreview.map((user) => (
                  <div
                    key={user.user_id}
                    className='flex items-center justify-between gap-3'
                  >
                    <span className='truncate'>
                      {user.position > 0 && (
                        <span className='text-muted-foreground'>
                          {user.position}.
                        </span>
                      )}{' '}
                      {user.username || `${t('User')} #${user.user_id}`}
                      {user.priority !== 0 && ` · P${user.priority}`}
                    </span>
                    <span className='text-muted-foreground shrink-0 tabular-nums'>
                      {formatAdmissionWaitMs(user.oldest_wait_ms)}
                    </span>
                  </div>
                ))}
                {waitingUsers.length > queuedPreview.length && (
                  <div className='text-muted-foreground'>
                    +{waitingUsers.length - queuedPreview.length}
                  </div>
                )}
              </div>
            )}
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
