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

const { getFreshModuleAccessMock } = vi.hoisted(() => ({
  getFreshModuleAccessMock: vi.fn(),
}))

vi.mock('@/features/model-status', () => ({
  ModelStatus: () => null,
}))

vi.mock('@/lib/nav-modules', () => ({
  getFreshModuleAccess: getFreshModuleAccessMock,
}))

const { guardModelStatusRoute } = await import('../index')

afterEach(() => {
  useAuthStore.getState().auth.reset()
})

describe('model status route access', () => {
  test('redirects to home when the module is disabled', async () => {
    getFreshModuleAccessMock.mockResolvedValue({
      enabled: false,
      requireAuth: false,
    })

    await expect(guardModelStatusRoute('/model-status')).rejects.toMatchObject({
      options: { to: '/' },
    })
  })

  test('redirects a visitor to sign in with the original URL when login is required', async () => {
    getFreshModuleAccessMock.mockResolvedValue({
      enabled: true,
      requireAuth: true,
    })

    await expect(
      guardModelStatusRoute('/model-status?from=header')
    ).rejects.toMatchObject({
      options: {
        to: '/sign-in',
        search: { redirect: '/model-status?from=header' },
      },
    })
  })

  test('allows public access without a user', async () => {
    getFreshModuleAccessMock.mockResolvedValue({
      enabled: true,
      requireAuth: false,
    })

    await expect(
      guardModelStatusRoute('/model-status')
    ).resolves.toBeUndefined()
  })

  test('allows an authenticated user when login is required', async () => {
    getFreshModuleAccessMock.mockResolvedValue({
      enabled: true,
      requireAuth: true,
    })
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'signed-in-user',
      role: 1,
    })

    await expect(
      guardModelStatusRoute('/model-status')
    ).resolves.toBeUndefined()
  })
})
