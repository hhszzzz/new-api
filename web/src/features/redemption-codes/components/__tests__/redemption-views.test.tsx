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
import { fireEvent, render, screen } from '@testing-library/react'
import { expect, test, vi } from 'vitest'

import { RedemptionViews } from '../redemption-views'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('../redemptions-table', () => ({
  RedemptionsTable: () => <div>Code list view</div>,
}))

vi.mock('../redemption-batches', () => ({
  RedemptionBatches: () => <div>Batch management view</div>,
}))

test('keeps code management as the default and opens batch management on demand', () => {
  render(<RedemptionViews />)

  expect(screen.getByText('Code list view')).toBeInTheDocument()
  expect(screen.queryByText('Batch management view')).not.toBeInTheDocument()

  fireEvent.click(screen.getByRole('tab', { name: 'Redemption batches' }))

  expect(screen.getByText('Batch management view')).toBeInTheDocument()
  expect(screen.queryByText('Code list view')).not.toBeInTheDocument()
})
