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
  StaticDataTable: () => null,
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
  model_name: 'providerless-model',
  quota_type: 0,
  model_ratio: 1,
  completion_ratio: 1,
  enable_groups: [],
}

describe('model details header', () => {
  test('does not show a dangling separator when the provider is absent', () => {
    render(
      <ModelDetailsContent
        model={model}
        groupRatio={{}}
        usableGroup={{}}
        endpointMap={{}}
        autoGroups={[]}
        priceRate={1}
        usdExchangeRate={1}
        tokenUnit='M'
      />
    )

    const header = screen.getByRole('banner')

    expect(within(header).getByText('Token-based')).toBeInTheDocument()
    expect(within(header).queryByText('·')).not.toBeInTheDocument()
  })
})
