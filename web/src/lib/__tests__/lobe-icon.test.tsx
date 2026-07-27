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
import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { getLobeIcon } from '../lobe-icon'

vi.mock('@lobehub/icons/es/icons.js', () => ({
  LmStudio: (props: { size?: number }) => (
    <svg aria-label='LM Studio' height={props.size} width={props.size} />
  ),
}))

describe('getLobeIcon', () => {
  test('renders common base and color variants without loading the icon barrel', () => {
    const { container, rerender } = render(getLobeIcon('OpenAI', 24))

    expect(container.querySelector('svg')).not.toBeNull()

    rerender(getLobeIcon('Claude.Color', 24))

    expect(container.querySelector('svg')).not.toBeNull()
  })

  test('keeps the branded avatar shape and icon scale for common icons', () => {
    const { container } = render(
      getLobeIcon('Claude.Avatar.type={"platform"}', 32)
    )

    expect(screen.getByLabelText('Claude')).toHaveStyle({
      background: '#D97757',
      borderRadius: '50%',
      height: '32px',
      width: '32px',
    })
    expect(container.querySelector('svg')).toHaveStyle({
      transform: 'scale(0.75)',
    })
  })

  test('preserves OpenAI avatar type backgrounds', () => {
    render(getLobeIcon('OpenAI.Avatar.type={"platform"}', 20))

    expect(screen.getByLabelText('OpenAI')).toHaveStyle({
      background: '#0000FE',
    })
  })

  test('renders a fixed-size fallback when no icon is configured', () => {
    render(getLobeIcon(null, 18))

    expect(screen.getByText('?')).toHaveStyle({ width: '18px', height: '18px' })
  })

  test('loads uncommon icons from the lazy fallback module', async () => {
    const { container } = render(getLobeIcon('LmStudio', 24))

    await waitFor(() => expect(container.querySelector('svg')).not.toBeNull())
    expect(screen.getByLabelText('LM Studio')).toHaveAttribute('width', '24')
  })
})
