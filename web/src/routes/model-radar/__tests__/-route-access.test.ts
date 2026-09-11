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
import { afterEach, describe, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

const { getModuleAccessForGuardMock } = vi.hoisted(() => ({
  getModuleAccessForGuardMock: vi.fn(),
}))

vi.mock('@/features/model-radar', () => ({ ModelRadar: () => null }))
vi.mock('@/lib/nav-modules', () => ({
  getModuleAccessForGuard: getModuleAccessForGuardMock,
}))

const { guardModelRadarRoute } = await import('../index')

afterEach(() => {
  useAuthStore.getState().auth.reset()
})

describe('model radar route access', () => {
  test('redirects to home when disabled', async () => {
    getModuleAccessForGuardMock.mockResolvedValue({
      enabled: false,
      requireAuth: false,
    })

    await expect(guardModelRadarRoute({} as never, '/model-radar')).rejects.toMatchObject({
      options: { to: '/' },
    })
  })

  test('redirects visitors to sign in when login is required', async () => {
    getModuleAccessForGuardMock.mockResolvedValue({
      enabled: true,
      requireAuth: true,
    })

    await expect(
      guardModelRadarRoute({} as never, '/model-radar?from=header')
    ).rejects.toMatchObject({
      options: {
        to: '/sign-in',
        search: { redirect: '/model-radar?from=header' },
      },
    })
  })

  test('allows public and authenticated access when permitted', async () => {
    getModuleAccessForGuardMock.mockResolvedValue({
      enabled: true,
      requireAuth: false,
    })
    await expect(guardModelRadarRoute({} as never, '/model-radar')).resolves.toBeUndefined()

    getModuleAccessForGuardMock.mockResolvedValue({
      enabled: true,
      requireAuth: true,
    })
    useAuthStore.getState().auth.setUser({ id: 1, username: 'user', role: 1 })
    await expect(guardModelRadarRoute({} as never, '/model-radar')).resolves.toBeUndefined()
  })
})
