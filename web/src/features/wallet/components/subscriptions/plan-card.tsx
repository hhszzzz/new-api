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
import { Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { GroupBadge } from '@/components/group-badge'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { USAGE_WINDOW_LABELS } from '@/features/subscriptions/constants'
import {
  formatDuration,
  formatResetPeriod,
  getPlanUsageLimits,
} from '@/features/subscriptions/lib'
import type { SubscriptionPlan } from '@/features/subscriptions/types'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'

interface PlanCardProps {
  plan: SubscriptionPlan
  recommended?: boolean
  purchaseCount: number
  onSubscribe: () => void
}

// Price and duration lead; the quota ledger underneath mirrors the meters the
// user will see on the subscription once they own it.
export function PlanCard(props: PlanCardProps) {
  const { t } = useTranslation()
  const plan = props.plan
  const price = Number(plan.price_amount || 0).toFixed(2)
  const limits = getPlanUsageLimits(plan)
  const limit = Number(plan.max_purchase_per_user || 0)
  const reached = limit > 0 && props.purchaseCount >= limit
  const resetPeriod = formatResetPeriod(plan, t)
  const group = plan.upgrade_group?.trim() || ''

  return (
    <article
      data-testid='plan-card'
      aria-label={plan.title}
      className={cn(
        'bg-card flex h-full flex-col rounded-xl border p-4 transition-shadow',
        props.recommended && 'border-primary/60 shadow-sm'
      )}
    >
      <div className='flex items-start justify-between gap-3'>
        <div className='min-w-0'>
          <h4 className='truncate font-semibold'>{plan.title}</h4>
          {plan.subtitle ? (
            <p className='text-muted-foreground mt-0.5 line-clamp-2 text-xs'>
              {plan.subtitle}
            </p>
          ) : null}
        </div>
        {props.recommended ? (
          <StatusBadge variant='info' copyable={false} className='shrink-0'>
            <Sparkles className='h-3 w-3' />
            {t('Recommended')}
          </StatusBadge>
        ) : null}
      </div>

      <div className='mt-3 flex items-baseline gap-1.5'>
        <span className='text-primary font-mono text-3xl font-semibold tabular-nums'>
          ${price}
        </span>
        <span className='text-muted-foreground text-xs'>
          / {formatDuration(plan, t)}
        </span>
      </div>

      <dl className='mt-4 flex-1 space-y-1.5 text-xs'>
        {limits.length === 0 ? (
          <div className='flex justify-between gap-3'>
            <dt className='text-muted-foreground'>
              {t(USAGE_WINDOW_LABELS.monthly)}
            </dt>
            <dd className='font-medium'>{t('Unlimited')}</dd>
          </div>
        ) : null}
        {limits.map((item) => (
          <div key={item.key} className='flex justify-between gap-3'>
            <dt className='text-muted-foreground'>
              {t(USAGE_WINDOW_LABELS[item.key])}
            </dt>
            <dd className='font-medium tabular-nums'>
              {formatQuota(item.amount)}
            </dd>
          </div>
        ))}
        {resetPeriod !== t('No Reset') ? (
          <div className='flex justify-between gap-3'>
            <dt className='text-muted-foreground'>{t('Quota Reset')}</dt>
            <dd className='font-medium'>{resetPeriod}</dd>
          </div>
        ) : null}
        {group ? (
          <div className='flex items-center justify-between gap-3'>
            <dt className='text-muted-foreground'>{t('Group')}</dt>
            <dd>
              <GroupBadge group={group} />
            </dd>
          </div>
        ) : null}
        {limit > 0 ? (
          <div className='flex justify-between gap-3'>
            <dt className='text-muted-foreground'>{t('Purchase Limit')}</dt>
            <dd className='font-medium tabular-nums'>
              {props.purchaseCount}/{limit}
            </dd>
          </div>
        ) : null}
      </dl>

      {reached ? (
        <Tooltip>
          <TooltipTrigger render={<div className='mt-4' />}>
            <Button variant='outline' className='w-full' disabled>
              {t('Limit Reached')}
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            {t('Purchase limit reached')} ({props.purchaseCount}/{limit})
          </TooltipContent>
        </Tooltip>
      ) : (
        <Button
          variant={props.recommended ? 'default' : 'outline'}
          className='mt-4 w-full'
          onClick={props.onSubscribe}
        >
          {t('Subscribe Now')}
        </Button>
      )}
    </article>
  )
}
