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

import { AccountStatusBadge } from '../account-status-badge'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

describe('account pool status badge', () => {
  test('shows quota exhaustion as limited instead of an error', () => {
    render(<AccountStatusBadge status='limited' />)

    expect(screen.getByText('Quota exhausted')).toBeInTheDocument()
    expect(screen.queryByText('Error')).not.toBeInTheDocument()
  })

  test('shows a real quota request failure as an error', () => {
    render(<AccountStatusBadge status='error' />)

    expect(screen.getByText('Error')).toBeInTheDocument()
    expect(screen.queryByText('Quota exhausted')).not.toBeInTheDocument()
  })
})
