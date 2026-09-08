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
import { afterEach, describe, expect, test, vi } from 'vitest'

import { VendorTabs } from '../components/vendor-tabs'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))
const vendors = [
  { key: 'openai', label: 'OpenAI', icon: 'OpenAI.Color', modelCount: 3 },
  { key: 'deepseek', label: 'DeepSeek', icon: 'DeepSeek.Color', modelCount: 1 },
]

afterEach(() => vi.restoreAllMocks())

describe('model radar vendor tabs', () => {
  test('scrolls a selected vendor into the visible tab strip', async () => {
    const user = userEvent.setup()
    function VendorPicker() {
      const [value, setValue] = useState('all')
      return (
        <VendorTabs
          vendors={vendors}
          totalModels={4}
          value={value}
          onValueChange={setValue}
        />
      )
    }
    render(<VendorPicker />)
    const scroller = screen.getByRole('tablist').parentElement
    if (!scroller) throw new Error('Vendor tabs must have a scroll container')
    const dsh = screen.getByRole('tab', { name: 'DeepSeek 1' })
    vi.spyOn(scroller, 'getBoundingClientRect').mockReturnValue(
      new DOMRect(0, 0, 200, 40)
    )
    vi.spyOn(dsh, 'getBoundingClientRect').mockReturnValue(
      new DOMRect(250, 0, 80, 40)
    )
    await user.click(dsh)
    expect(dsh).toHaveAttribute('aria-selected', 'true')
    expect(scroller.scrollLeft).toBe(130)
  })
  test('renders vendor counts and supports clicks and arrow-key activation', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    function VendorPicker() {
      const [value, setValue] = useState('all')
      return (
        <VendorTabs
          vendors={vendors}
          totalModels={5}
          value={value}
          onValueChange={(vendor) => {
            setValue(vendor)
            onChange(vendor)
          }}
        />
      )
    }
    render(<VendorPicker />)
    expect(screen.getByRole('tablist', { name: 'Vendor' })).toBeVisible()
    expect(screen.getByRole('tab', { name: 'All 5' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    await user.click(screen.getByRole('tab', { name: 'OpenAI 3' }))
    expect(onChange).toHaveBeenLastCalledWith('openai')
    await user.keyboard('{ArrowRight}{Enter}')
    expect(onChange).toHaveBeenLastCalledWith('deepseek')
    expect(screen.getByRole('tab', { name: 'DeepSeek 1' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
  })

  test.each([{ items: [] }, { items: vendors.slice(0, 1) }])(
    'omits tabs with at most one vendor and retains page content',
    ({ items }) => {
      render(
        <VendorTabs
          vendors={items}
          totalModels={3}
          value='all'
          onValueChange={vi.fn()}
        >
          <p>content</p>
        </VendorTabs>
      )
      expect(screen.queryByRole('tablist')).toBeNull()
      expect(screen.getByText('content')).toBeVisible()
    }
  )
})
