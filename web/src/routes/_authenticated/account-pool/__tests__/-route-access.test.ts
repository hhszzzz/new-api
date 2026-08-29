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

vi.mock('@/features/account-pool', () => ({ AccountPool: () => null }))

const { ensureAccountPoolAccess } = await import('../index')

afterEach(() => useAuthStore.getState().auth.reset())

describe('account pool route access', () => {
  test('allows a user with the server-issued account-pool capability', () => {
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'allowed-user',
      role: 1,
      permissions: { account_pool: true },
    })

    expect(ensureAccountPoolAccess()).toBeUndefined()
  })

  test('redirects when the account-pool capability is absent', () => {
    useAuthStore.getState().auth.setUser({
      id: 2,
      username: 'denied-user',
      role: 1,
      permissions: { account_pool: false },
    })

    expect(() => ensureAccountPoolAccess()).toThrowError()
    try {
      ensureAccountPoolAccess()
    } catch (error) {
      expect(error).toMatchObject({ options: { to: '/403' } })
    }
  })
})
