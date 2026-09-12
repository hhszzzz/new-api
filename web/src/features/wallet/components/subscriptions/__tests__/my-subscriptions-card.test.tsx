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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import type {
  UserSubscription,
  UserSubscriptionRecord,
} from '@/features/subscriptions/types'

import { MySubscriptionsCard } from '../my-subscriptions-card'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, options?: Record<string, unknown>) =>
      options
        ? key.replaceAll(/\{\{(\w+)\}\}/g, (_, name: string) =>
            String(options[name] ?? '')
          )
        : key,
  }),
}))

const NOW = 1_800_000_000

function record(
  id: number,
  overrides: Partial<UserSubscription> = {}
): UserSubscriptionRecord {
  return {
    subscription: {
      id,
      user_id: 1,
      plan_id: 1,
      status: 'active',
      start_time: NOW - 86400,
      end_time: NOW + 86400,
      amount_total: 0,
      amount_used: 0,
      ...overrides,
    },
  }
}

function renderCard(subscriptions: UserSubscriptionRecord[]) {
  const onBillingPreferenceChange = vi.fn()
  const onBrowsePlans = vi.fn()
  render(
    <MySubscriptionsCard
      nowSeconds={NOW}
      subscriptions={subscriptions}
      planTitleMap={new Map([[1, 'Pro']])}
      billingPreference='subscription_first'
      onBillingPreferenceChange={onBillingPreferenceChange}
      onRefresh={() => undefined}
      onBrowsePlans={onBrowsePlans}
    />
  )
  return { onBillingPreferenceChange, onBrowsePlans }
}

describe('MySubscriptionsCard', () => {
  test('hides the billing preference when every active subscription is group-bound', () => {
    renderCard([record(1, { upgrade_group: 'vip' })])

    expect(
      screen.queryByRole('combobox', { name: 'Billing preference' })
    ).toBeNull()
    expect(screen.getAllByTestId('subscription-card')).toHaveLength(1)
  })

  test('shows the billing preference while a generic subscription is active', () => {
    renderCard([record(1, { upgrade_group: 'vip' }), record(2)])

    expect(
      screen.getByRole('combobox', { name: 'Billing preference' })
    ).toBeInTheDocument()
  })

  test('keeps paused subscriptions in the current list and folds ended ones into history', async () => {
    const user = userEvent.setup()
    renderCard([
      record(1, { status: 'paused', paused_at: NOW - 10 }),
      record(2, { status: 'cancelled', end_time: NOW - 100 }),
      record(3, { end_time: NOW - 5 }),
    ])

    expect(screen.getAllByTestId('subscription-card')).toHaveLength(1)
    expect(screen.queryByTestId('subscription-history')).toBeNull()

    await user.click(
      screen.getByRole('button', { name: 'Subscription history' })
    )

    const history = await screen.findByTestId('subscription-history')
    expect(history.querySelectorAll('li')).toHaveLength(2)
  })

  test('offers the plan list when the user has nothing active', async () => {
    const user = userEvent.setup()
    const { onBrowsePlans } = renderCard([])

    expect(screen.getByText('No subscription yet')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Browse plans' }))
    expect(onBrowsePlans).toHaveBeenCalledTimes(1)
  })
})
