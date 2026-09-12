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

import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import {
  formatSubscriptionName,
  getSubscriptionState,
  getUsageMeters,
  type SubscriptionState,
} from '@/features/subscriptions/lib'
import type { UserSubscription } from '@/features/subscriptions/types'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { UsageMeterRow } from './usage-meter-row'

interface SubscriptionCardProps {
  subscription: UserSubscription
  planTitle?: string
  nowSeconds: number
}

const STATE_BADGE: Record<
  SubscriptionState,
  { label: string; variant: StatusVariant }
> = {
  active: { label: 'Active', variant: 'success' },
  paused: { label: 'Paused', variant: 'warning' },
  cancelled: { label: 'Cancelled', variant: 'neutral' },
  expired: { label: 'Expired', variant: 'neutral' },
}

function remainingDays(endTime: number, nowSeconds: number): number {
  if (!endTime) return 0
  return Math.max(0, Math.ceil((endTime - nowSeconds) / 86400))
}

// A subscription reads like a small ledger: who it pays for, how long it
// lasts, and one meter per limit that applies to it.
export function SubscriptionCard(props: SubscriptionCardProps) {
  const { t } = useTranslation()
  const subscription = props.subscription
  const state = getSubscriptionState(subscription, props.nowSeconds)
  const badge = STATE_BADGE[state]
  const group = subscription.upgrade_group?.trim() || ''
  const meters = getUsageMeters(subscription, props.nowSeconds)
  const isActive = state === 'active'

  let validity = t('Ended {{time}}', {
    time: formatTimestampToDate(subscription.end_time),
  })
  if (isActive) {
    validity = `${t('{{count}} days remaining', {
      count: remainingDays(subscription.end_time, props.nowSeconds),
    })} · ${t('Until')} ${formatTimestampToDate(subscription.end_time)}`
  } else if (state === 'paused') {
    validity = t('Paused since {{time}}', {
      time: formatTimestampToDate(subscription.paused_at || 0),
    })
  }

  return (
    <article
      data-testid='subscription-card'
      data-state={state}
      aria-label={formatSubscriptionName(subscription, props.planTitle, t)}
      className={cn(
        'bg-card flex flex-col gap-2.5 rounded-xl border p-3 sm:p-4',
        state === 'paused' && 'border-warning/40 bg-warning/5',
        !isActive && state !== 'paused' && 'opacity-70'
      )}
    >
      <div className='flex items-start justify-between gap-3'>
        <div className='min-w-0'>
          <h4 className='truncate text-sm font-semibold'>
            {formatSubscriptionName(subscription, props.planTitle, t)}
          </h4>
          <p className='text-muted-foreground mt-0.5 text-xs'>{validity}</p>
        </div>
        <div className='flex shrink-0 items-center gap-2'>
          <span className='text-muted-foreground text-xs whitespace-nowrap'>
            {group ? t('Group: {{group}}', { group }) : t('Group: Any')}
          </span>
          <StatusBadge
            label={t(badge.label)}
            variant={badge.variant}
            copyable={false}
          />
        </div>
      </div>

      {meters.length > 0 ? (
        <div className='space-y-2'>
          {meters.map((meter) => (
            <UsageMeterRow key={meter.key} meter={meter} inactive={!isActive} />
          ))}
        </div>
      ) : (
        <p className='text-muted-foreground text-xs'>
          {t('Unlimited quota during the subscription period.')}
        </p>
      )}
    </article>
  )
}
