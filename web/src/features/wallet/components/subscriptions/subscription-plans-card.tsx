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
import { Package } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'
import { SubscriptionPurchaseDialog } from '@/features/subscriptions/components/dialogs/subscription-purchase-dialog'
import type { PlanRecord } from '@/features/subscriptions/types'

import type { PaymentMethod, TopupInfo } from '../../types'
import { PlanCard } from './plan-card'

interface SubscriptionPlansCardProps {
  loading?: boolean
  plans: PlanRecord[]
  planPurchaseCountMap: Map<number, number>
  topupInfo: TopupInfo | null
  userQuota?: number
  onPurchaseSuccess?: () => void | Promise<void>
  onPurchaseDialogClose?: () => void
}

function getEpayMethods(payMethods: PaymentMethod[] = []): PaymentMethod[] {
  return payMethods.filter(
    (m) => m?.type && m.type !== 'stripe' && m.type !== 'creem'
  )
}

export function SubscriptionPlansCard(props: SubscriptionPlansCardProps) {
  const { t } = useTranslation()
  const [purchaseOpen, setPurchaseOpen] = useState(false)
  const [selectedPlan, setSelectedPlan] = useState<PlanRecord | null>(null)

  const epayMethods = useMemo(
    () => getEpayMethods(props.topupInfo?.pay_methods),
    [props.topupInfo?.pay_methods]
  )
  const selectedCount = selectedPlan?.plan?.id
    ? props.planPurchaseCountMap.get(selectedPlan.plan.id)
    : undefined
  const selectedLimit = Number(selectedPlan?.plan?.max_purchase_per_user || 0)

  return (
    <>
      <TitledCard
        title={t('Subscription Plans')}
        description={t('Subscribe to a plan for model access')}
        icon={<Package className='h-4 w-4' />}
        iconTone='primary'
        disableHoverEffect
        compact
      >
        {props.loading ? (
          <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-3'>
            {['first', 'second', 'third'].map((key) => (
              <Skeleton key={key} className='h-64 rounded-xl' />
            ))}
          </div>
        ) : null}
        {!props.loading && props.plans.length === 0 ? (
          <p className='text-muted-foreground py-6 text-center text-sm'>
            {t('No plans available')}
          </p>
        ) : null}
        {!props.loading && props.plans.length > 0 ? (
          <div
            data-testid='plan-grid'
            className='grid gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4'
          >
            {props.plans.map((record, index) => (
              <PlanCard
                key={record.plan.id}
                plan={record.plan}
                recommended={index === 0 && props.plans.length > 1}
                purchaseCount={
                  props.planPurchaseCountMap.get(record.plan.id) || 0
                }
                onSubscribe={() => {
                  setSelectedPlan(record)
                  setPurchaseOpen(true)
                }}
              />
            ))}
          </div>
        ) : null}
      </TitledCard>

      <SubscriptionPurchaseDialog
        open={purchaseOpen}
        onOpenChange={(open) => {
          setPurchaseOpen(open)
          if (!open) props.onPurchaseDialogClose?.()
        }}
        plan={selectedPlan}
        enableStripe={!!props.topupInfo?.enable_stripe_topup}
        enableCreem={!!props.topupInfo?.enable_creem_topup}
        enableWaffoPancake={!!props.topupInfo?.enable_waffo_pancake_topup}
        enableOnlineTopUp={!!props.topupInfo?.enable_online_topup}
        epayMethods={epayMethods}
        userQuota={props.userQuota}
        onPurchaseSuccess={props.onPurchaseSuccess}
        purchaseLimit={selectedLimit > 0 ? selectedLimit : undefined}
        purchaseCount={selectedCount}
      />
    </>
  )
}
