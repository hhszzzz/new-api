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
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { Pricing } from '../index'

const testState = vi.hoisted(() => ({ isLoading: false }))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('@/components/layout', () => ({
  PublicLayout: (props: PropsWithChildren) => <div>{props.children}</div>,
}))

vi.mock('@/components/page-transition', () => ({
  PageTransition: (props: PropsWithChildren) => <div>{props.children}</div>,
}))

vi.mock('../hooks/use-pricing-data', () => ({
  usePricingData: () => ({
    models: [
      {
        id: 1,
        model_name: 'layout-model',
        quota_type: 0,
        model_ratio: 1,
        completion_ratio: 1,
        enable_groups: [],
      },
    ],
    vendors: [],
    groupRatio: {},
    usableGroup: {},
    endpointMap: {},
    autoGroups: [],
    isLoading: testState.isLoading,
    priceRate: 1,
    usdExchangeRate: 1,
  }),
}))

vi.mock('../hooks/use-filters', () => ({
  useFilters: (models: unknown[]) => ({
    searchInput: '',
    sortBy: 'name',
    vendorFilter: 'all',
    groupFilter: 'all',
    quotaTypeFilter: 'all',
    endpointTypeFilter: 'all',
    tagFilter: 'all',
    tokenUnit: 'M',
    viewMode: 'card',
    showRechargePrice: false,
    setSearchInput: vi.fn(),
    setSortBy: vi.fn(),
    setVendorFilter: vi.fn(),
    setGroupFilter: vi.fn(),
    setQuotaTypeFilter: vi.fn(),
    setEndpointTypeFilter: vi.fn(),
    setTagFilter: vi.fn(),
    setTokenUnit: vi.fn(),
    setViewMode: vi.fn(),
    setShowRechargePrice: vi.fn(),
    filteredModels: models,
    hasActiveFilters: false,
    activeFilterCount: 0,
    availableTags: [],
    clearFilters: vi.fn(),
    clearSearch: vi.fn(),
  }),
}))

vi.mock('../components', () => ({
  LoadingSkeleton: () => <div data-testid='loading-skeleton' />,
  EmptyState: () => <div data-testid='empty-state' />,
  SearchBar: (props: { className?: string }) => (
    <div data-testid='model-search' className={props.className} />
  ),
  PricingTable: () => <div data-testid='pricing-table' />,
  PricingSidebar: () => <div data-testid='pricing-sidebar' />,
  PricingToolbar: () => <div data-testid='pricing-toolbar' />,
  ModelCardGrid: () => <div data-testid='model-card-grid' />,
  ModelDetailsDrawer: () => <div data-testid='model-details' />,
}))

describe('pricing page landmarks', () => {
  beforeEach(() => {
    testState.isLoading = false
  })

  test('keeps the hidden page heading and catalog controls inside the main landmark', () => {
    render(<Pricing />)

    const main = screen.getByRole('main')
    const heading = within(main).getByRole('heading', {
      level: 1,
      name: 'Model Square',
    })

    expect(heading).toHaveClass('sr-only')
    expect(within(main).getByTestId('model-search')).toBeInTheDocument()
    expect(within(main).getByTestId('model-search')).toHaveClass('mx-auto')
    expect(within(main).getByTestId('pricing-sidebar')).toBeInTheDocument()
    expect(within(main).getByTestId('pricing-toolbar')).toBeInTheDocument()
    expect(within(main).getByTestId('model-card-grid')).toBeInTheDocument()
  })

  test('keeps a named main landmark while the catalog is loading', () => {
    testState.isLoading = true

    render(<Pricing />)

    const main = screen.getByRole('main')
    const heading = within(main).getByRole('heading', {
      level: 1,
      name: 'Model Square',
    })

    expect(heading).toHaveClass('sr-only')
    expect(within(main).getByTestId('loading-skeleton')).toBeInTheDocument()
  })
})
