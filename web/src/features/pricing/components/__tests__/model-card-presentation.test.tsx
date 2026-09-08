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
  test('shows vendor name, unit label, and metadata rows', () => {
    const { rerender } = render(
      <ModelCard model={model} tokenUnit='M' onClick={() => undefined} />
    )

    expect(
      screen.getByRole('heading', { name: 'claude-test' })
    ).toBeInTheDocument()
    expect(screen.getByText('Anthropic')).toBeVisible()
    expect(screen.getByText('Token-based')).toBeVisible()
    expect(screen.getAllByText('/ 1M').length).toBeGreaterThan(0)
    expect(screen.getByText('Groups')).toBeVisible()
    expect(screen.getByText('premium')).toBeVisible()
    expect(screen.getByText('+1')).toBeVisible()
    expect(screen.getByText('/v1/chat/completions')).toBeVisible()
    expect(screen.getByText('vision, reasoning')).toBeVisible()

    rerender(
      <ModelCard model={model} tokenUnit='K' onClick={() => undefined} />
    )

    expect(screen.getAllByText('/ 1K').length).toBeGreaterThan(0)
    expect(screen.queryByText('/ 1M')).not.toBeInTheDocument()
  })

  test('hides blank descriptions entirely instead of reserving space', () => {
    const { container, rerender } = render(
      <ModelCard model={model} tokenUnit='M' onClick={() => undefined} />
    )

    expect(
      screen.queryByText('Vendor fallback must stay hidden.')
    ).not.toBeInTheDocument()
    expect(
      screen.queryByText('No description available.')
    ).not.toBeInTheDocument()
    expect(container.querySelector('p.line-clamp-2')).toBeNull()

    rerender(
      <ModelCard
        model={{ ...model, description: 'A concise model description.' }}
        tokenUnit='M'
        onClick={() => undefined}
      />
    )

    expect(screen.getByText('A concise model description.')).toHaveClass(
      'line-clamp-2'
    )
  })
})
