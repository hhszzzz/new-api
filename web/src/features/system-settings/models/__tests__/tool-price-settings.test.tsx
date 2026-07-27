/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

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
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ToolPriceSettings } from '../tool-price-settings'

const { mutateAsyncMock } = vi.hoisted(() => ({
  mutateAsyncMock: vi.fn(),
}))

vi.mock('../../hooks/use-update-option', () => ({
  useUpdateOption: () => ({
    isPending: false,
    mutateAsync: mutateAsyncMock,
  }),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

describe('tool price settings validation', () => {
  beforeEach(() => {
    mutateAsyncMock.mockReset()
    mutateAsyncMock.mockResolvedValue({ success: true })
  })

  it('blocks saving an empty tool identifier until it is completed', async () => {
    const user = userEvent.setup()
    render(<ToolPriceSettings defaultValue='{"web_search":10}' />)

    await user.click(screen.getByRole('button', { name: 'Add' }))

    const saveButton = screen.getByRole('button', { name: 'Save tool prices' })
    expect(saveButton).toBeDisabled()
    expect(
      screen.getByText(
        'Tool prices require non-empty identifiers and non-negative finite numbers'
      )
    ).toBeInTheDocument()

    const identifiers = screen.getAllByPlaceholderText(
      'web_search_preview:gpt-4o*'
    )
    const customIdentifier = identifiers.at(-1)
    if (!customIdentifier) {
      throw new Error('missing custom tool identifier input')
    }
    await user.type(customIdentifier, 'custom_tool')

    expect(saveButton).toBeEnabled()
    await user.click(saveButton)
    expect(mutateAsyncMock).toHaveBeenCalledTimes(1)
    const request = mutateAsyncMock.mock.calls[0][0] as {
      key: string
      value: string
    }
    expect(request.key).toBe('tool_price_setting.prices')
    expect(JSON.parse(request.value)).toEqual(
      expect.objectContaining({
        web_search: 10,
        image_generation: 150,
        custom_tool: 0,
      })
    )
  })
})
