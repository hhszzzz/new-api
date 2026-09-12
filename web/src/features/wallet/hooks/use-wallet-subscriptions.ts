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
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  getPublicPlans,
  getSelfSubscriptionFull,
  updateBillingPreference,
} from '@/features/subscriptions/api'
import { getSubscriptionState } from '@/features/subscriptions/lib'
import type {
  PlanRecord,
  UserSubscriptionRecord,
} from '@/features/subscriptions/types'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

export const DEFAULT_BILLING_PREFERENCE = 'subscription_first'

// Loads the public plans and the user's own subscriptions for the wallet page
// and owns the billing preference round-trip.
export function useWalletSubscriptions() {
  const { t } = useTranslation()
  const [plans, setPlans] = useState<PlanRecord[]>([])
  const [subscriptions, setSubscriptions] = useState<UserSubscriptionRecord[]>(
    []
  )
  const [billingPreference, setBillingPreference] = useState(
    DEFAULT_BILLING_PREFERENCE
  )
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [nowSeconds, setNowSeconds] = useState(() => Date.now() / 1000)

  const fetchPlans = useCallback(async () => {
    try {
      const res = requireServerSuccess(await getPublicPlans())
      setPlans(res.data || [])
    } catch (error) {
      handleServerError(error)
      setPlans([])
    }
  }, [])

  const fetchSubscriptions = useCallback(async () => {
    try {
      const res = requireServerSuccess(await getSelfSubscriptionFull())
      if (res.data) {
        setBillingPreference(
          res.data.billing_preference || DEFAULT_BILLING_PREFERENCE
        )
        setSubscriptions(res.data.all_subscriptions || [])
        setNowSeconds(Date.now() / 1000)
      }
    } catch (error) {
      handleServerError(error)
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      await Promise.all([fetchPlans(), fetchSubscriptions()])
      if (!cancelled) setLoading(false)
    }
    void load()
    return () => {
      cancelled = true
    }
  }, [fetchPlans, fetchSubscriptions])

  const refresh = useCallback(async () => {
    setRefreshing(true)
    try {
      await fetchSubscriptions()
    } finally {
      setRefreshing(false)
    }
  }, [fetchSubscriptions])

  const changeBillingPreference = useCallback(
    async (preference: string) => {
      const previous = billingPreference
      setBillingPreference(preference)
      try {
        const res = await updateBillingPreference(preference)
        if (res.success) {
          toast.success(t('Updated successfully'))
          setBillingPreference(res.data?.billing_preference || preference)
          return
        }
        handleServerError(res, t('Update failed'))
        setBillingPreference(previous)
      } catch (error) {
        handleServerError(error, t('Request failed'))
        setBillingPreference(previous)
      }
    },
    [billingPreference, t]
  )

  const activeSubscriptions = useMemo(
    () =>
      subscriptions.filter(
        (record) =>
          getSubscriptionState(record.subscription, nowSeconds) === 'active'
      ),
    [subscriptions, nowSeconds]
  )

  const planPurchaseCountMap = useMemo(() => {
    const map = new Map<number, number>()
    for (const record of subscriptions) {
      const planId = record.subscription.plan_id
      if (!planId) continue
      map.set(planId, (map.get(planId) || 0) + 1)
    }
    return map
  }, [subscriptions])

  const planTitleMap = useMemo(() => {
    const map = new Map<number, string>()
    for (const record of plans) {
      if (record.plan.id) map.set(record.plan.id, record.plan.title || '')
    }
    return map
  }, [plans])

  return {
    loading,
    refreshing,
    nowSeconds,
    plans,
    subscriptions,
    activeSubscriptions,
    billingPreference,
    planPurchaseCountMap,
    planTitleMap,
    refresh,
    changeBillingPreference,
  }
}
