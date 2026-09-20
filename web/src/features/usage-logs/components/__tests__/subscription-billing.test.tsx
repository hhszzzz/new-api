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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import i18next from 'i18next'
import { afterEach, beforeAll, describe, expect, test } from 'vitest'

import type { UsageLog } from '../../data/schema'
import type { LogOtherData } from '../../types'
import { DetailsDialog } from '../dialogs/details-dialog'

const i18nKeys = {
  'Log Details': 'Log Details',
  Consume: 'Consume',
  'Billing Details': 'Billing Details',
  'Total Cost': 'Total Cost',
  'Subscription Billing': 'Subscription Billing',
  Subscription: 'Subscription',
  'Deducted by subscription': 'Deducted by subscription',
  'Conditional multipliers': 'Conditional multipliers',
  Matched: 'Matched',
  '{{group}} group subscription': '{{group}} group subscription',
}

function makeLog(other: LogOtherData): UsageLog {
  return {
    id: 1,
    user_id: 1,
    created_at: 1,
    type: 2,
    content: '',
    username: 'user',
    token_name: 'token',
    model_name: 'gpt-4o',
    quota: 5000,
    prompt_tokens: 0,
    completion_tokens: 0,
    use_time: 0,
    is_stream: false,
    channel: 1,
    channel_name: '',
    token_id: 1,
    group: 'default',
    ip: '',
    other: JSON.stringify(other),
    request_id: 'req-1',
    upstream_request_id: '',
  }
}

function renderDetails(other: LogOtherData): QueryClient {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const freshAt = Date.now() + 60_000
  queryClient.setQueryData(['status'], {}, { updatedAt: freshAt })
  queryClient.setQueryData(
    ['pricing'],
    { data: [], vendors: [] },
    { updatedAt: freshAt }
  )

  render(
    <QueryClientProvider client={queryClient}>
      <DetailsDialog
        log={makeLog(other)}
        isAdminView={false}
        isRoot={false}
        canViewModelRoute={false}
        open
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
  return queryClient
}

function rowValue(label: string): string | null {
  return screen.getByText(label).nextElementSibling?.textContent ?? null
}

describe('billing details', () => {
  const queryClients: QueryClient[] = []

  beforeAll(() => {
    i18next.addResourceBundle('en', 'translation', i18nKeys)
  })

  afterEach(() => {
    for (const queryClient of queryClients) {
      queryClient.clear()
    }
    queryClients.length = 0
  })

  test('names the subscription after its granted group and reports the deduction', () => {
    queryClients.push(
      renderDetails({
        group_ratio: 1,
        billing_source: 'subscription',
        subscription_group: 'pro',
        subscription_plan_title: 'Pro Plan',
      })
    )

    expect(screen.getByText('Subscription Billing')).toBeInTheDocument()
    expect(rowValue('Subscription')).toBe('pro group subscription')
    expect(rowValue('Deducted by subscription')).toBe(rowValue('Total Cost'))
  })

  test('falls back to the plan title when the subscription grants no group', () => {
    queryClients.push(
      renderDetails({
        group_ratio: 1,
        billing_source: 'subscription',
        subscription_plan_title: 'Pro Plan',
      })
    )

    expect(rowValue('Subscription')).toBe('Pro Plan')
  })

  test('never exposes internal subscription accounting rows', () => {
    queryClients.push(
      renderDetails({
        group_ratio: 1,
        billing_source: 'subscription',
        subscription_group: 'pro',
        subscription_plan_title: 'Pro Plan',
      })
    )

    expect(screen.queryByText('Pre-consumed')).toBeNull()
    expect(screen.queryByText('Post Delta')).toBeNull()
    expect(screen.queryByText('Final Consumed')).toBeNull()
    expect(screen.queryByText('Instance')).toBeNull()
  })

  test('shows the conditional multiplier trace used for settlement', () => {
    queryClients.push(
      renderDetails({
        billing_mode: 'tiered_expr',
        request_rules: [
          {
            cond: 'param("service_tier") == "priority"',
            multiplier: 2,
            matched: true,
          },
        ],
      })
    )

    const condition = 'param("service_tier") == "priority"'
    expect(screen.getByText('Conditional multipliers')).toBeInTheDocument()
    expect(rowValue(condition)).toBe('2x · Matched')
  })

  test('omits the subscription section for wallet-billed logs', () => {
    queryClients.push(
      renderDetails({
        group_ratio: 1,
        billing_source: 'wallet',
      })
    )

    expect(screen.queryByText('Subscription Billing')).toBeNull()
    expect(screen.getByText('Total Cost')).toBeInTheDocument()
  })
})
