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

    expect(result.current).toEqual([
      {
        title: 'Model Square',
        href: '/pricing',
        requiresAuth: true,
      },
      {
        title: 'Model Status',
        href: '/model-status',
        requiresAuth: true,
      },
      {
        title: 'Model Radar',
        href: '/model-radar',
        requiresAuth: true,
      },
    ])
  })

  test('places model status immediately after model square', () => {
    useStatusMock.mockReturnValue({ status })

    const { result } = renderHook(() => useTopNavLinks())

    expect(result.current.map((link) => link.href)).toEqual([
      '/pricing',
      '/model-status',
      '/model-radar',
      '/rankings',
    ])
  })

  test('marks the model status link as requiring login only for visitors', () => {
    useStatusMock.mockReturnValue({ status })

    const { result } = renderHook(() => useTopNavLinks())

    expect(
      result.current.find((link) => link.href === '/model-status')
    ).toMatchObject({ requiresAuth: true })

    act(() => {
      useAuthStore.getState().auth.setUser({
        id: 1,
        username: 'signed-in-user',
        role: 1,
      })
    })

    expect(
      result.current.find((link) => link.href === '/model-status')
    ).toMatchObject({ requiresAuth: false })
  })

  test('places model radar after model status and applies its independent access', () => {
    useStatusMock.mockReturnValue({ status })

    const { result } = renderHook(() => useTopNavLinks())

    expect(result.current.find((link) => link.href === '/model-radar')).toEqual(
      {
        title: 'Model Radar',
        href: '/model-radar',
        requiresAuth: false,
      }
    )
  })
})
