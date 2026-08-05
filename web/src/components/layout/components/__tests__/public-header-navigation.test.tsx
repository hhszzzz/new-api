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
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ComponentProps } from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import type { TopNavLink } from '../../types'
import { PublicHeader } from '../public-header'

const { topNavLinksMock, translateMock } = vi.hoisted(() => ({
  topNavLinksMock: vi.fn<() => TopNavLink[]>(() => [
    { title: 'Models', href: '/pricing' },
  ]),
  translateMock: vi.fn((key: string) => key),
}))

vi.mock('@tanstack/react-router', () => ({
  Link: (props: ComponentProps<'a'> & { to: string }) => (
    <a {...props} href={props.to} />
  ),
  useNavigate: () => vi.fn(),
  useRouterState: () => ({ location: { pathname: '/' } }),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: translateMock }),
}))

vi.mock('@/components/dialog', () => ({
  Dialog: () => null,
}))

vi.mock('@/hooks/use-notifications', () => ({
  useNotifications: () => ({
    popoverOpen: false,
    setPopoverOpen: vi.fn(),
    unreadCount: 0,
    activeTab: 'notice',
    setActiveTab: vi.fn(),
    notice: [],
    announcements: [],
    loading: false,
  }),
}))

vi.mock('@/hooks/use-system-config', () => ({
  useSystemConfig: () => ({
    systemName: 'Test',
    logo: '',
    loading: false,
    logoLoaded: true,
  }),
}))

vi.mock('@/hooks/use-top-nav-links', () => ({
  useTopNavLinks: () => topNavLinksMock(),
}))

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: () => ({ auth: { user: null } }),
}))

vi.mock('../header-logo', () => ({
  HeaderLogo: () => <span aria-hidden='true' />,
}))

const mediaQueryListeners = new Set<(event: MediaQueryListEvent) => void>()

beforeEach(() => {
  mediaQueryListeners.clear()
  topNavLinksMock.mockReturnValue([{ title: 'Models', href: '/pricing' }])
  translateMock.mockImplementation((key: string) => key)
  vi.stubGlobal(
    'matchMedia',
    vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: (
        eventName: string,
        listener: (event: MediaQueryListEvent) => void
      ) => {
        if (eventName === 'change') mediaQueryListeners.add(listener)
      },
      removeEventListener: (
        eventName: string,
        listener: (event: MediaQueryListEvent) => void
      ) => {
        if (eventName === 'change') mediaQueryListeners.delete(listener)
      },
      dispatchEvent: vi.fn(),
    }))
  )
})

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.style.overflow = ''
})

function renderPublicHeader() {
  return render(
    <PublicHeader
      showAuthButtons={false}
      showLanguageSwitcher={false}
      showNotifications={false}
      showThemeSwitch={false}
    />
  )
}

describe('public header navigation', () => {
  test('exposes the collapsed menu state and controlled region', async () => {
    const user = userEvent.setup()
    renderPublicHeader()

    const toggle = screen.getByRole('button', {
      name: 'Toggle navigation menu',
    })
    const controls = toggle.getAttribute('aria-controls')

    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    if (!controls) {
      throw new Error('Navigation toggle must identify its controlled menu')
    }
    const menu = document.querySelector(`[id="${controls}"]`)
    expect(menu).toBeInTheDocument()
    expect(menu).toHaveAttribute('aria-hidden', 'true')
    expect(menu).toHaveAttribute('inert')

    await user.click(toggle)

    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(menu).toHaveAttribute('aria-hidden', 'false')
    expect(menu).not.toHaveAttribute('inert')
  })

  test('centers desktop links in the right-aligned header group', () => {
    topNavLinksMock.mockReturnValue([
      { title: 'Models', href: '/pricing' },
      {
        title: 'Docs',
        href: 'https://docs.example.com',
        external: true,
      },
    ])
    const view = renderPublicHeader()

    expect(
      screen
        .getByRole('link', { name: 'Models' })
        .closest('[data-slot="public-header-actions"]')
    ).toHaveClass('ml-auto')
    expect(
      view.container.querySelector('header a[href="/pricing"]')
    ).toHaveClass('inline-flex', 'items-center')
    expect(
      view.container.querySelector('header a[href="https://docs.example.com"]')
    ).toHaveClass('inline-flex', 'items-center')
  })

  test('does not translate already-localized dynamic navigation titles again', () => {
    topNavLinksMock.mockReturnValue([{ title: '模型广场', href: '/pricing' }])
    translateMock.mockImplementation((key: string) => `missing:${key}`)

    renderPublicHeader()

    expect(screen.getByRole('link', { name: '模型广场' })).toBeInTheDocument()
    expect(translateMock).not.toHaveBeenCalledWith('模型广场')
  })

  test('shows icons on non-home links in desktop and collapsed navigation', () => {
    topNavLinksMock.mockReturnValue([
      { title: 'Home', href: '/' },
      {
        title: 'Models',
        href: '/pricing',
        icon: DashboardBrowsingIcon,
      },
    ])

    const view = renderPublicHeader()

    expect(view.container.querySelectorAll('[data-top-nav-icon]')).toHaveLength(
      2
    )
    for (const homeLink of screen.getAllByRole('link', { name: 'Home' })) {
      expect(homeLink.querySelector('[data-top-nav-icon]')).toBeNull()
    }
  })

  test('closes the collapsed menu and unlocks scrolling at the desktop breakpoint', async () => {
    const user = userEvent.setup()
    renderPublicHeader()
    const toggle = screen.getByRole('button', {
      name: 'Toggle navigation menu',
    })

    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(document.body).toHaveStyle({ overflow: 'hidden' })

    act(() => {
      for (const listener of mediaQueryListeners) {
        listener({ matches: true } as MediaQueryListEvent)
      }
    })

    await waitFor(() =>
      expect(toggle).toHaveAttribute('aria-expanded', 'false')
    )
    expect(document.body).not.toHaveStyle({ overflow: 'hidden' })
  })
})
