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

import { useSidebarData } from '@/hooks/use-sidebar-data'
import { useAuthStore } from '@/stores/auth-store'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

afterEach(() => useAuthStore.getState().auth.reset())

describe('account pool sidebar capability', () => {
  test('adds the account pool after overview only for capable users', () => {
    const { result } = renderHook(() => useSidebarData())
    const generalBefore = result.current.navGroups.find(
      (group) => group.id === 'general'
    )
    expect(
      generalBefore?.items.some((item) => item.url === '/account-pool')
    ).toBe(false)

    act(() => {
      useAuthStore.getState().auth.setUser({
        id: 1,
        username: 'allowed-user',
        role: 1,
        permissions: { account_pool: true },
      })
    })

    const generalAfter = result.current.navGroups.find(
      (group) => group.id === 'general'
    )
    const urls = generalAfter?.items.flatMap((item) => item.url ?? []) ?? []
    expect(urls.slice(0, 3)).toEqual([
      '/dashboard/overview',
      '/account-pool',
      '/dashboard/models',
    ])
  })
})
