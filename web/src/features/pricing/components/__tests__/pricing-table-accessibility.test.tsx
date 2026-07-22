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
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import type { PricingModel } from '../../types'
import { PricingTable } from '../pricing-table'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: () => null,
}))

const model: PricingModel = {
  id: 1,
  model_name: 'keyboard-accessible-model',
  vendor_name: 'Example Provider',
  quota_type: 0,
  model_ratio: 1,
  completion_ratio: 1,
  enable_groups: ['default'],
}

describe('pricing table details action', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'matchMedia',
      vi.fn().mockImplementation((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }))
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  test('opens model details from a keyboard-focusable button', async () => {
    const user = userEvent.setup()
    const onModelClick = vi.fn()

    render(<PricingTable models={[model]} onModelClick={onModelClick} />)

    const detailsButton = screen.getByRole('button', {
      name: 'View details: keyboard-accessible-model',
    })

    detailsButton.focus()
    expect(detailsButton).toHaveFocus()

    await user.keyboard('{Enter}')

    expect(onModelClick).toHaveBeenCalledOnce()
    expect(onModelClick).toHaveBeenCalledWith('keyboard-accessible-model')
  })

  test('does not trigger the row click a second time when using the details button', async () => {
    const user = userEvent.setup()
    const onModelClick = vi.fn()

    render(<PricingTable models={[model]} onModelClick={onModelClick} />)

    await user.click(
      screen.getByRole('button', {
        name: 'View details: keyboard-accessible-model',
      })
    )

    expect(onModelClick).toHaveBeenCalledOnce()
  })
})
