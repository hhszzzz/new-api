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
import { ChevronDown, Crown, RefreshCw } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'
import {
  formatSubscriptionName,
  getSubscriptionState,
} from '@/features/subscriptions/lib'
import type { UserSubscriptionRecord } from '@/features/subscriptions/types'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { BillingPreferenceSelect } from './billing-preference-select'
import { SubscriptionCard } from './subscription-card'

interface MySubscriptionsCardProps {
  loading?: boolean
  refreshing?: boolean
  nowSeconds: number
  subscriptions: UserSubscriptionRecord[]
  planTitleMap: Map<number, string>
  billingPreference: string
  onBillingPreferenceChange: (value: string) => void
  onRefresh: () => void
  onBrowsePlans?: () => void
}

const HISTORY_LABEL: Record<string, string> = {
  cancelled: 'Cancelled',
  expired: 'Expired',
}

export function MySubscriptionsCard(props: MySubscriptionsCardProps) {
  const { t } = useTranslation()
  const [historyOpen, setHistoryOpen] = useState(false)

  const { current, history, showPreference } = useMemo(() => {
    const currentList: UserSubscriptionRecord[] = []
    const historyList: UserSubscriptionRecord[] = []
    let hasGenericActive = false
    for (const record of props.subscriptions) {
      const state = getSubscriptionState(record.subscription, props.nowSeconds)
      if (state === 'active' || state === 'paused') {
        currentList.push(record)
      } else {
        historyList.push(record)
      }
      if (state === 'active' && !record.subscription.upgrade_group?.trim()) {
        hasGenericActive = true
      }
    }
    return {
      current: currentList,
      history: historyList,
      showPreference: hasGenericActive,
    }
  }, [props.subscriptions, props.nowSeconds])

  const activeCount = current.filter(
    (record) =>
      getSubscriptionState(record.subscription, props.nowSeconds) === 'active'
  ).length

  return (
    <TitledCard
      title={t('My Subscriptions')}
      description={
        activeCount > 0
          ? t('{{count}} active', { count: activeCount })
          : t('No active subscription')
      }
      icon={<Crown className='h-4 w-4' />}
      iconTone='warning'
      disableHoverEffect
      compact
      action={
        <div className='flex items-center justify-end gap-2 sm:justify-start'>
          {showPreference ? (
            <BillingPreferenceSelect
              value={props.billingPreference}
              onChange={props.onBillingPreferenceChange}
            />
          ) : null}
          <Button
            variant='ghost'
            size='icon'
            className='h-8 w-8'
            onClick={props.onRefresh}
            disabled={props.refreshing}
            aria-label={t('Refresh')}
          >
            <RefreshCw
              className={cn('h-3.5 w-3.5', props.refreshing && 'animate-spin')}
            />
          </Button>
        </div>
      }
      contentClassName='space-y-4'
    >
      {props.loading ? (
        <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-3'>
          {['first', 'second', 'third'].map((key) => (
            <Skeleton key={key} className='h-44 rounded-xl' />
          ))}
        </div>
      ) : null}

      {!props.loading && current.length > 0 ? (
        <div
          data-testid='current-subscriptions'
          className='grid gap-3 md:grid-cols-2 xl:grid-cols-3'
        >
          {current.map((record) => (
            <SubscriptionCard
              key={record.subscription.id}
              subscription={record.subscription}
              planTitle={props.planTitleMap.get(record.subscription.plan_id)}
              nowSeconds={props.nowSeconds}
            />
          ))}
        </div>
      ) : null}

      {!props.loading && current.length === 0 ? (
        <EmptyState
          icon={Crown}
          title={t('No subscription yet')}
          description={t(
            'Subscribe to a plan below to unlock its group and quota.'
          )}
          action={
            props.onBrowsePlans ? (
              <Button variant='outline' size='sm' onClick={props.onBrowsePlans}>
                {t('Browse plans')}
              </Button>
            ) : undefined
          }
        />
      ) : null}

      {!props.loading && history.length > 0 ? (
        <Collapsible open={historyOpen} onOpenChange={setHistoryOpen}>
          <CollapsibleTrigger
            className='text-muted-foreground hover:text-foreground flex w-full items-center justify-between gap-2 rounded-md border border-dashed px-3 py-2 text-xs transition-colors'
            aria-label={t('Subscription history')}
          >
            <span>
              {t('Subscription history')} · {history.length}
            </span>
            <ChevronDown
              className={cn(
                'size-3.5 transition-transform',
                historyOpen && 'rotate-180'
              )}
              aria-hidden='true'
            />
          </CollapsibleTrigger>
          <CollapsibleContent>
            <ul
              data-testid='subscription-history'
              className='divide-y rounded-md border text-xs'
            >
              {history.map((record) => {
                const subscription = record.subscription
                const state = getSubscriptionState(
                  subscription,
                  props.nowSeconds
                )
                return (
                  <li
                    key={subscription.id}
                    className='flex flex-wrap items-center justify-between gap-2 px-3 py-2'
                  >
                    <span className='flex min-w-0 items-center gap-2'>
                      <span className='truncate font-medium'>
                        {formatSubscriptionName(
                          subscription,
                          props.planTitleMap.get(subscription.plan_id),
                          t
                        )}
                      </span>
                      <StatusBadge
                        label={t(HISTORY_LABEL[state] ?? 'Expired')}
                        variant='neutral'
                        copyable={false}
                      />
                    </span>
                    <span className='text-muted-foreground tabular-nums'>
                      {t('Ended {{time}}', {
                        time: formatTimestampToDate(subscription.end_time),
                      })}
                    </span>
                  </li>
                )
              })}
            </ul>
          </CollapsibleContent>
        </Collapsible>
      ) : null}
    </TitledCard>
  )
}
