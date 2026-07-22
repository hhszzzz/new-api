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
import { ModelCard } from '../model-card'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: () => null,
}))

const model: PricingModel = {
  id: 1,
  model_name: 'claude-test',
  description: '   ',
  vendor_name: 'Anthropic',
  vendor_description: 'Vendor fallback must stay hidden.',
  quota_type: 0,
  model_ratio: 1,
  completion_ratio: 2,
  enable_groups: ['premium', 'standard'],
  supported_endpoint_types: ['/v1/chat/completions'],
  tags: 'vision,reasoning',
}

describe('model card presentation', () => {
  test('keeps the original compact card metadata layout', () => {
    const { rerender } = render(
      <ModelCard model={model} tokenUnit='M' onClick={() => undefined} />
    )

    const title = screen.getByRole('heading', { name: 'claude-test' })
    const tokenUnit = screen.getByText('1M')
    const billingMode = screen.getByText('Token-based')
    const inputPrice = screen.getByText('Input')

    expect(
      title.compareDocumentPosition(inputPrice) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(
      billingMode.compareDocumentPosition(tokenUnit) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()

    rerender(
      <ModelCard model={model} tokenUnit='K' onClick={() => undefined} />
    )

    expect(screen.getByText('1K')).toBeInTheDocument()
    expect(screen.queryByText('1M')).not.toBeInTheDocument()
  })

  test('hides the provider, group, and empty description text while preserving card layout', () => {
    const { container } = render(
      <ModelCard model={model} tokenUnit='M' onClick={() => undefined} />
    )

    expect(screen.queryByText('Anthropic')).not.toBeInTheDocument()
    expect(
      screen.queryByText('Vendor fallback must stay hidden.')
    ).not.toBeInTheDocument()
    expect(screen.queryByText('premium')).not.toBeInTheDocument()
    expect(screen.queryByText('standard')).not.toBeInTheDocument()
    expect(screen.queryByText('/v1/chat/completions')).toBeInTheDocument()
    expect(screen.queryByText('vision')).toBeInTheDocument()
    expect(screen.queryByText('reasoning')).toBeInTheDocument()
    expect(
      screen.queryByText('No description available.')
    ).not.toBeInTheDocument()
    expect(container.querySelector('p.text-muted-foreground')).toHaveClass(
      'flex-1',
      'sm:min-h-[2.5rem]'
    )
    expect(
      screen.getByRole('heading', { name: 'claude-test' }).closest('.group')
    ).not.toHaveClass('self-start')
  })
})
