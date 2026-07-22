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
import type { PropsWithChildren } from 'react'
import { describe, expect, test, vi } from 'vitest'

import type { PricingModel } from '../../types'
import { ModelDetailsContent } from '../model-details'

vi.mock('@tanstack/react-query', () => ({
  useQuery: () => ({ data: undefined }),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('@/components/copy-button', () => ({
  CopyButton: () => null,
}))

vi.mock('@/components/data-table', () => ({
  StaticDataTable: (props: { data: unknown[] }) => (
    <div>
      {props.data.map((item) => (
        <span key={String(item)}>{String(item)}</span>
      ))}
    </div>
  ),
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
})
