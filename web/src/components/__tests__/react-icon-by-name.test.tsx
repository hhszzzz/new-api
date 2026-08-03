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
import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { ReactIconByName } from '../react-icon-by-name'

describe('ReactIconByName', () => {
  test('loads an installed React Icons export by its exact name', async () => {
    render(
      <ReactIconByName
        name='LuRadar'
        data-testid='dynamic-icon'
        fallback={<span data-testid='fallback-icon' />}
      />
    )

    expect(screen.getByTestId('fallback-icon')).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByTestId('dynamic-icon')).toBeInTheDocument()
    })
  })

  test('keeps the fallback for an invalid or unknown icon name', async () => {
    const { rerender } = render(
      <ReactIconByName
        name='Radar01Icon'
        fallback={<span data-testid='fallback-icon' />}
      />
    )

    expect(screen.getByTestId('fallback-icon')).toBeInTheDocument()

    rerender(
      <ReactIconByName
        name='LuMissingForTest'
        fallback={<span data-testid='fallback-icon' />}
      />
    )

    await waitFor(() => {
      expect(screen.getByTestId('fallback-icon')).toBeInTheDocument()
    })
  })
})
