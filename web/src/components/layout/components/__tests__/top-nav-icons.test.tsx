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
import { DashboardBrowsingIcon } from '@hugeicons/core-free-icons'
import { render } from '@testing-library/react'
import type { ComponentProps } from 'react'
import { describe, expect, test, vi } from 'vitest'

import { TopNav } from '../top-nav'

vi.mock('@tanstack/react-router', () => ({
  Link: (props: ComponentProps<'a'> & { to: string }) => (
    <a {...props} href={props.to} />
  ),
}))

describe('TopNav icons', () => {
  test('renders the supplied line icon while keeping home text-only', () => {
    const view = render(
      <TopNav
        links={[
          { title: 'Home', href: '/' },
          {
            title: 'Console',
            href: '/dashboard',
            icon: DashboardBrowsingIcon,
          },
        ]}
      />
    )

    expect(
      view.container.querySelector('a[href="/"] [data-top-nav-icon]')
    ).toBeNull()
    expect(
      view.container.querySelector('a[href="/dashboard"] [data-top-nav-icon]')
    ).not.toBeNull()
  })
})
