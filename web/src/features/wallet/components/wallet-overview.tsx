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
import { Activity, BarChart3, Crown, Plus, Receipt } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { IconBadge, type IconBadgeTone } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { formatQuota } from '@/lib/format'

import type { UserWalletData } from '../types'

interface WalletOverviewProps {
  user: UserWalletData | null
  loading?: boolean
  activeSubscriptionCount: number
  /** Hide the subscription stat when the deployment has no plans at all. */
  showSubscriptions: boolean
  onAddFunds: () => void
  onOpenBilling: () => void
}

// The balance is the one number every visitor came for, so it gets the hero
// slot; everything else in the band is a supporting figure.
export function WalletOverview(props: WalletOverviewProps) {
  const { t } = useTranslation()

  const stats: {
    key: string
    label: string
    value: string
    icon: typeof BarChart3
    tone: IconBadgeTone
  }[] = [
    {
      key: 'usage',
      label: t('Total Usage'),
      value: formatQuota(props.user?.used_quota ?? 0),
      icon: BarChart3,
      tone: 'info',
    },
    {
      key: 'requests',
      label: t('API Requests'),
      value: (props.user?.request_count ?? 0).toLocaleString(),
      icon: Activity,
      tone: 'chart-4',
    },
  ]
  if (props.showSubscriptions) {
    stats.push({
      key: 'subscriptions',
      label: t('Active subscriptions'),
      value: String(props.activeSubscriptionCount),
      icon: Crown,
      tone: 'warning',
    })
  }

  return (
    <Card
      data-card-hover='false'
      data-testid='wallet-overview'
      className='gap-0 overflow-hidden py-0'
    >
      <CardContent className='grid p-0 lg:grid-cols-[minmax(0,1.15fr)_minmax(0,1fr)]'>
        <div className='flex flex-col justify-between gap-4 p-4 sm:p-5'>
          <div>
            <div className='text-muted-foreground text-[11px] font-medium tracking-[0.14em] uppercase'>
              {t('Available balance')}
            </div>
            {props.loading ? (
              <Skeleton className='mt-2 h-9 w-40 sm:h-10' />
            ) : (
              <div
                data-testid='wallet-balance'
                className='text-foreground mt-1 font-mono text-2xl font-semibold tracking-tight break-all tabular-nums sm:text-4xl'
              >
                {formatQuota(props.user?.quota ?? 0)}
              </div>
            )}
          </div>
          <div className='flex flex-wrap gap-2'>
            <Button onClick={props.onAddFunds} className='gap-2'>
              <Plus className='size-4' />
              {t('Add Funds')}
            </Button>
            <Button
              variant='outline'
              onClick={props.onOpenBilling}
              className='gap-2'
            >
              <Receipt className='size-4' />
              {t('Order History')}
            </Button>
          </div>
        </div>

        <div className='bg-muted/30 grid grid-cols-3 divide-x border-t lg:border-t-0 lg:border-l'>
          {stats.map((item) => (
            <div
              key={item.key}
              className='flex min-w-0 flex-col justify-center gap-1.5 px-3 py-3 sm:px-4 sm:py-4'
            >
              <div className='flex items-center gap-2'>
                <IconBadge tone={item.tone} size='stat'>
                  <item.icon />
                </IconBadge>
                <span className='text-muted-foreground truncate text-[11px] font-medium tracking-wider uppercase'>
                  {item.label}
                </span>
              </div>
              {props.loading ? (
                <Skeleton className='h-6 w-20' />
              ) : (
                <div className='text-foreground truncate font-mono text-sm font-semibold tabular-nums sm:text-lg'>
                  {item.value}
                </div>
              )}
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}
