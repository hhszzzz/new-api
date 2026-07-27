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

import { formatLogQuota } from '@/lib/format'

import { LogCostDisplay } from '../log-cost-display'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

function normalizedText(value: string | null): string {
  return (value ?? '').replaceAll(/\s/g, '')
}

describe('log cost display', () => {
  test('keeps the regular cost visible and adds an accessible surcharge marker', () => {
    const { container } = render(
      <LogCostDisplay
        quota={12500}
        other={{
          tool_surcharges: [{ name: 'lookup_customer', count: 1, price: 5 }],
        }}
      />
    )

    expect(normalizedText(container.textContent)).toContain(
      normalizedText(formatLogQuota(12500))
    )
    const marker = screen.getByRole('img', {
      name: 'Includes tool-call surcharge',
    })
    expect(marker).toHaveAttribute('data-tool-surcharge-indicator', 'true')
    expect(marker).toHaveAttribute('tabindex', '0')
  })

  test('preserves the subscription badge and adds the same legacy surcharge marker', () => {
    render(
      <LogCostDisplay
        quota={5000}
        other={{
          billing_source: 'subscription',
          web_search: true,
          web_search_call_count: 1,
          web_search_price: 10,
        }}
      />
    )

    expect(screen.getByText('Subscription')).toBeVisible()
    expect(
      screen.getByRole('img', { name: 'Includes tool-call surcharge' })
    ).toHaveAttribute('data-tool-surcharge-indicator', 'true')
  })
})
