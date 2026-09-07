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
import { useState } from 'react'
import { describe, expect, test, vi } from 'vitest'

import { StationTabs } from '../components/station-tabs'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))
const stations = [
  { key: 'codex', label: 'Codex', count: 3 },
  { key: 'dsh', label: 'DSH', count: 1 },
]

describe('model radar station tabs', () => {
  test('renders station counts and supports clicks and arrow-key activation', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    function StationPicker() {
      const [value, setValue] = useState('all')
      return (
        <StationTabs
          stations={stations}
          total={5}
          value={value}
          onValueChange={(station) => {
            setValue(station)
            onChange(station)
          }}
        />
      )
    }
    render(<StationPicker />)
    expect(screen.getByRole('tablist', { name: 'Station' })).toBeVisible()
    expect(screen.getByRole('tab', { name: 'All 5' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    await user.click(screen.getByRole('tab', { name: 'Codex 3' }))
    expect(onChange).toHaveBeenLastCalledWith('codex')
    await user.keyboard('{ArrowRight}{Enter}')
    expect(onChange).toHaveBeenLastCalledWith('dsh')
    expect(screen.getByRole('tab', { name: 'DSH 1' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
  })

  test.each([{ items: [] }, { items: stations.slice(0, 1) }])(
    'omits tabs with at most one station and retains page content',
    ({ items }) => {
      render(
        <StationTabs
          stations={items}
          total={3}
          value='all'
          onValueChange={vi.fn()}
        >
          <p>content</p>
        </StationTabs>
      )
      expect(screen.queryByRole('tablist')).toBeNull()
      expect(screen.getByText('content')).toBeVisible()
    }
  )
})
