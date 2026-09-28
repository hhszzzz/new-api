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
import { describe, expect, test, vi } from 'vitest'

import type { PricingModel } from '../../types'
import { ModelBillingModeBadge } from '../model-billing-mode-badge'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

const base: PricingModel = {
  id: 1,
  model_name: 'priced-model',
  quota_type: 0,
  model_ratio: 1,
  completion_ratio: 1,
  enable_groups: ['default'],
}

/** The color class sits on the badge root, not on the label's own element. */
function badgeFor(label: string): HTMLElement {
  const badge = screen.getByText(label).closest('[data-slot="status-badge"]')
  if (!badge) {
    throw new Error(`no badge rendered for ${label}`)
  }
  return badge as HTMLElement
}

describe('model billing mode badge', () => {
  test('colors an expression model by what it charges, not by how it is stored', () => {
    const expressionToken: PricingModel = {
      ...base,
      quota_type: 1,
      model_price: 0,
      billing_mode: 'tiered_expr',
      billing_expr: 'tier("base", p * 4 + c * 20)',
    }
    render(<ModelBillingModeBadge model={expressionToken} />)

    const badge = badgeFor('Token-based')
    expect(badge).toHaveClass('text-info')
    expect(badge).not.toHaveClass('text-warning')
  })

  test('gives the same color to the same badge whichever mode stores the price', () => {
    const legacyToken: PricingModel = { ...base, model_ratio: 2 }
    const expressionRequest: PricingModel = {
      ...base,
      quota_type: 1,
      model_price: 0.01,
      billing_mode: 'tiered_expr',
      billing_expr: 'tier("base", fixed(0.01))',
    }
    const expressionDynamic: PricingModel = {
      ...base,
      billing_mode: 'tiered_expr',
      billing_expr:
        'len <= 272000 ? tier("standard", p * 4) : tier("long", p * 8)',
    }
    const taskBilled: PricingModel = {
      ...base,
      billing_usage_schema: { seconds: { type: 'number', unit: 'second' } },
    }

    render(
      <>
        <ModelBillingModeBadge model={legacyToken} />
        <ModelBillingModeBadge model={expressionRequest} />
        <ModelBillingModeBadge model={expressionDynamic} />
        <ModelBillingModeBadge model={taskBilled} />
      </>
    )

    expect(badgeFor('Token-based')).toHaveClass('text-info')
    expect(badgeFor('Per Request')).toHaveClass('text-chart-4')
    expect(badgeFor('Dynamic Pricing')).toHaveClass('text-warning')
    expect(badgeFor('Task billing')).toHaveClass('text-chart-4')
  })
})
