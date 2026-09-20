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
import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { useTopNavLinks } from '../use-top-nav-links'

const { useStatusMock } = vi.hoisted(() => ({
  useStatusMock: vi.fn(),
}))

vi.mock('@/hooks/use-status', () => ({
  useStatus: () => useStatusMock(),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

const status = {
  HeaderNavModules: JSON.stringify({
    home: false,
    console: false,
    pricing: { enabled: true, requireAuth: false },
    modelStatus: { enabled: true, requireAuth: true },
    modelRadar: { enabled: true, requireAuth: false },
    rankings: { enabled: true, requireAuth: false },
    docs: false,
    about: false,
  }),
}

afterEach(() => {
  useAuthStore.getState().auth.reset()
})

describe('top navigation model status link', () => {
  test('omits home and keeps every visible built-in link text-only', () => {
    useStatusMock.mockReturnValue({ status: { HeaderNavModules: '{}' } })

    const { result } = renderHook(() => useTopNavLinks())
    const linksByHref = new Map(
      result.current.map((link) => [link.href, link] as const)
    )

    expect(linksByHref.has('/')).toBe(false)
    expect(linksByHref.get('/dashboard')?.icon).toBeUndefined()
    expect(linksByHref.get('/pricing')?.icon).toBeUndefined()
    expect(linksByHref.has('/model-status')).toBe(false)
    expect(linksByHref.get('/model-radar')?.icon).toBeUndefined()
    expect(linksByHref.get('/rankings')?.icon).toBeUndefined()
    expect(linksByHref.get('/docs')?.icon).toBeUndefined()
    expect(linksByHref.get('/about')?.icon).toBeUndefined()
  })

  test('inherits model square access when legacy status omits model status', () => {
    useStatusMock.mockReturnValue({
      status: {
        HeaderNavModules: JSON.stringify({
          home: false,
          console: false,
          pricing: { enabled: true, requireAuth: true },
          rankings: false,
          docs: false,
          about: false,
        }),
      },
    })

    const { result } = renderHook(() => useTopNavLinks())

    expect(
      result.current.map(({ title, href, requiresAuth }) => ({
        title,
        href,
        requiresAuth,
      }))
    ).toEqual([
      {
        title: 'Models',
        href: '/pricing',
        requiresAuth: true,
      },
      {
        title: 'Model Radar',
        href: '/model-radar',
        requiresAuth: true,
      },
    ])
    expect(result.current.every((link) => link.icon === undefined)).toBe(true)
  })

  test('omits the old status entry even when its legacy setting is enabled', () => {
    useStatusMock.mockReturnValue({ status })

    const { result } = renderHook(() => useTopNavLinks())

    expect(result.current.map((link) => link.href)).toEqual([
      '/pricing',
      '/model-radar',
      '/rankings',
    ])
  })

  test('marks the model square link as requiring login only for visitors', () => {
    useStatusMock.mockReturnValue({
      status: {
        HeaderNavModules: JSON.stringify({
          pricing: { enabled: true, requireAuth: true },
        }),
      },
    })

    const { result } = renderHook(() => useTopNavLinks())

    expect(
      result.current.find((link) => link.href === '/pricing')
    ).toMatchObject({ requiresAuth: true })

    act(() => {
      useAuthStore.getState().auth.setUser({
        id: 1,
        username: 'signed-in-user',
        role: 1,
      })
    })

    expect(
      result.current.find((link) => link.href === '/pricing')
    ).toMatchObject({ requiresAuth: false })
  })

  test('preserves the independent model radar access', () => {
    useStatusMock.mockReturnValue({ status })

    const { result } = renderHook(() => useTopNavLinks())

    expect(
      result.current.find((link) => link.href === '/model-radar')
    ).toMatchObject({
      title: 'Model Radar',
      href: '/model-radar',
      requiresAuth: false,
    })
    expect(
      result.current.find((link) => link.href === '/model-radar')?.icon
    ).toBeUndefined()
  })

  test('places custom iframe navigation at its configured position', () => {
    useStatusMock.mockReturnValue({
      status: {
        HeaderNavModules: JSON.stringify({
          home: false,
          console: false,
          pricing: false,
          modelStatus: false,
          modelRadar: false,
          rankings: false,
          docs: false,
          about: false,
          custom: [
            {
              id: 'portal',
              title: 'Team Portal',
              url: 'https://portal.example.com',
              icon: 'LuRadar',
              enabled: true,
            },
          ],
          order: ['custom:portal', 'home'],
        }),
      },
    })

    const { result } = renderHook(() => useTopNavLinks())

    expect(result.current).toEqual([
      {
        title: 'Team Portal',
        href: '/custom/portal',
      },
    ])
  })
})
