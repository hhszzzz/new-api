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
import { render, screen, within } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import type { UserSubscription } from '@/features/subscriptions/types'

import { SubscriptionCard } from '../subscription-card'

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

function subscription(
  overrides: Partial<UserSubscription> = {}
): UserSubscription {
  return {
    id: 5,
    user_id: 1,
    plan_id: 2,
    status: 'active',
    start_time: NOW - 86400,
    end_time: NOW + 3 * 86400,
    amount_total: 0,
    amount_used: 0,
    ...overrides,
  }
}

describe('SubscriptionCard', () => {
  test('renders one meter per configured limit with the remaining amount', () => {
    render(
      <SubscriptionCard
        nowSeconds={NOW}
        planTitle='Pro'
        subscription={subscription({
          amount_total: 1000,
          amount_used: 400,
          window_5h_amount: 100,
          window_5h_used: 30,
          window_5h_end_time: NOW + 600,
        })}
      />
    )

    const card = screen.getByTestId('subscription-card')
    expect(card).toHaveAttribute('data-state', 'active')
    expect(within(card).getByTestId('usage-meter-monthly')).toBeInTheDocument()
    expect(within(card).getByTestId('usage-meter-5h')).toBeInTheDocument()
    expect(within(card).queryByTestId('usage-meter-weekly')).toBeNull()
    expect(
      within(card).getByRole('progressbar', { name: '5-hour limit' })
    ).toHaveAttribute('aria-valuenow', '30')
    expect(
      within(card).getByText('3 days remaining', { exact: false })
    ).toBeInTheDocument()
  })

  test('labels the group a subscription funds, or Any when it is generic', () => {
    const { unmount } = render(
      <SubscriptionCard
        nowSeconds={NOW}
        subscription={subscription({ upgrade_group: 'vip' })}
      />
    )
    expect(screen.getByText('Group: vip')).toBeInTheDocument()
    expect(
      screen.getByText('Unlimited quota during the subscription period.')
    ).toBeInTheDocument()
    unmount()

    render(<SubscriptionCard nowSeconds={NOW} subscription={subscription()} />)
    expect(screen.getByText('Group: Any')).toBeInTheDocument()
  })

  test('marks a paused subscription and shows when it was paused', () => {
    render(
      <SubscriptionCard
        nowSeconds={NOW}
        subscription={subscription({ status: 'paused', paused_at: NOW - 60 })}
      />
    )

    const card = screen.getByTestId('subscription-card')
    expect(card).toHaveAttribute('data-state', 'paused')
    expect(within(card).getByText('Paused')).toBeInTheDocument()
    expect(
      within(card).getByText('Paused since', { exact: false })
    ).toBeInTheDocument()
  })
})
