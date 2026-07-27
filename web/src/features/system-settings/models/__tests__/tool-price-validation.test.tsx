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
import { describe, expect, it, vi } from 'vitest'

import { ToolPriceSettings } from '../tool-price-settings'

vi.mock('../../hooks/use-update-option', () => ({
  useUpdateOption: () => ({
    isPending: false,
    mutateAsync: vi.fn(),
  }),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

describe('tool price validation', () => {
  it('blocks an empty price without converting it to zero', async () => {
    const user = userEvent.setup()
    render(<ToolPriceSettings defaultValue='{"web_search":10}' />)

    const priceInput = screen.getByRole('spinbutton', {
      name: 'Price ($/1K calls): web_search',
    })
    const saveButton = screen.getByRole('button', { name: 'Save tool prices' })

    await user.clear(priceInput)

    expect(priceInput).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByText('Please enter a valid number')).toBeInTheDocument()
    expect(saveButton).toBeDisabled()

    await user.type(priceInput, '0')

    expect(priceInput).toHaveAttribute('aria-invalid', 'false')
    expect(saveButton).toBeEnabled()
  })
})
