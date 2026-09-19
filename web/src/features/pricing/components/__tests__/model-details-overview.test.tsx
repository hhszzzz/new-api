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
import type { PropsWithChildren } from 'react'
import { describe, expect, test, vi } from 'vitest'

import type { PricingModel } from '../../types'
import { ModelDetailsContent } from '../model-details'

vi.mock('@tanstack/react-query', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-query')>()),
  useQuery: () => ({ data: undefined }),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key, i18n: { language: 'en' } }),
}))

vi.mock('@/components/copy-button', () => ({
  CopyButton: () => null,
}))

vi.mock('@/components/data-table', () => ({
  StaticDataTable: (props: {
    data: unknown[]
    columns?: {
      id: string
      header?: unknown
      cell?: (item: never, index: number) => unknown
    }[]
    getRowKey?: (item: never, index: number) => string
  }) => (
    <table>
      <thead>
        <tr>
          {(props.columns ?? []).map((column) => (
            <th key={column.id}>{column.header as never}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {props.data.map((item, index) => (
          <tr key={props.getRowKey?.(item as never, index) ?? index}>
            {(props.columns ?? []).map((column) => (
              <td key={column.id}>
                {column.cell?.(item as never, index) as never}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  ),
}))

vi.mock('@/components/group-badge', () => ({
  GroupBadge: (props: { group?: string | null }) => <span>{props.group}</span>,
}))

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: () => null,
}))

vi.mock('@/components/ui/tabs', () => ({
  Tabs: (props: PropsWithChildren) => <div>{props.children}</div>,
  TabsContent: (props: PropsWithChildren) => <div>{props.children}</div>,
  TabsList: (props: PropsWithChildren) => <div>{props.children}</div>,
  TabsTrigger: (props: PropsWithChildren) => (
    <button type='button'>{props.children}</button>
  ),
}))

vi.mock('../model-billing-mode-badge', () => ({
  ModelBillingModeBadge: () => <span>Token-based</span>,
}))

vi.mock('../model-details-api', () => ({
  ModelDetailsApi: () => null,
}))

vi.mock('../model-details-performance', () => ({
  ModelDetailsPerformance: () => null,
}))

const model: PricingModel = {
  id: 1,
  model_name: 'grouped-model',
  vendor_name: 'Example Provider',
  parameter_count: '7B',
  quota_type: 0,
  model_ratio: 1,
  completion_ratio: 1,
  enable_groups: ['premium'],
  supported_endpoint_types: ['/v1/chat/completions'],
  tags: 'reasoning,tools',
}

describe('model details overview', () => {
  test('keeps group pricing after removing the redundant model information section', () => {
    render(
      <ModelDetailsContent
        model={model}
        groupRatio={{ premium: 1 }}
        usableGroup={{ premium: { desc: 'Premium', ratio: 1 } }}
        endpointMap={{}}
        autoGroups={[]}
        priceRate={1}
        usdExchangeRate={1}
        tokenUnit='M'
      />
    )

    expect(
      screen.queryByRole('heading', { level: 2, name: 'Model' })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('heading', { level: 2, name: 'Pricing by Group' })
    ).toBeInTheDocument()
    expect(screen.getByText('premium')).toBeInTheDocument()
  })

  test('shows only the group and its ratio for time-priced models', () => {
    const timePricedModel: PricingModel = {
      ...model,
      model_name: 'time-priced-model',
      enable_groups: ['vip'],
      billing_mode: 'tiered_expr',
      billing_expr:
        'hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 18 ? tier("peak", p * 8 + c * 24) : tier("off_peak", p * 3 + c * 12)',
    }

    render(
      <ModelDetailsContent
        model={timePricedModel}
        groupRatio={{ vip: 1 }}
        usableGroup={{ vip: { desc: 'VIP', ratio: 1 } }}
        endpointMap={{}}
        autoGroups={[]}
        priceRate={1}
        usdExchangeRate={1}
        tokenUnit='M'
      />
    )

    const section = screen
      .getByRole('heading', { level: 2, name: 'Pricing by Group' })
      .closest('section') as HTMLElement

    // The peak markup lives in the tiered price table above, so the group
    // table must not restate the peak/off-peak tiers per group.
    expect(within(section).getByText('vip')).toBeInTheDocument()
    expect(within(section).getByText('1x')).toBeInTheDocument()
    expect(within(section).queryByText(/\$/)).not.toBeInTheDocument()
  })

  test('keeps the per-tier prices for models priced by size, not by clock', () => {
    const sizeTieredModel: PricingModel = {
      ...model,
      model_name: 'size-tiered-model',
      enable_groups: ['vip'],
      billing_mode: 'tiered_expr',
      billing_expr:
        'len <= 272000 ? tier("standard", p * 8 + c * 24) : tier("long_context", p * 3 + c * 12)',
    }

    render(
      <ModelDetailsContent
        model={sizeTieredModel}
        groupRatio={{ vip: 1 }}
        usableGroup={{ vip: { desc: 'VIP', ratio: 1 } }}
        endpointMap={{}}
        autoGroups={[]}
        priceRate={1}
        usdExchangeRate={1}
        tokenUnit='M'
      />
    )

    const section = screen
      .getByRole('heading', { level: 2, name: 'Pricing by Group' })
      .closest('section') as HTMLElement

    expect(within(section).getByText('vip')).toBeInTheDocument()
    expect(within(section).getByText('1x')).toBeInTheDocument()
    expect(within(section).getByText('$8')).toBeInTheDocument()
    expect(within(section).getByText('$12')).toBeInTheDocument()
  })
})
