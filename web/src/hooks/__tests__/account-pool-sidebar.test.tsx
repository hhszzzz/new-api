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
import { Timeline } from 'lucide-react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { useSidebarData } from '@/hooks/use-sidebar-data'
import { useAuthStore } from '@/stores/auth-store'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

afterEach(() => useAuthStore.getState().auth.reset())

describe('account pool sidebar capability', () => {
  test('adds a resources group between personal and admin only for capable users', () => {
    const { result } = renderHook(() => useSidebarData())
    const generalBefore = result.current.navGroups.find(
      (group) => group.id === 'general'
    )
    expect(
      generalBefore?.items.some((item) => item.url === '/account-pool')
    ).toBe(false)
    expect(
      result.current.navGroups.some((group) => group.id === 'resources')
    ).toBe(false)

    act(() => {
      useAuthStore.getState().auth.setUser({
        id: 1,
        username: 'allowed-user',
        role: 1,
        permissions: { account_pool: true },
      })
    })

    const groups = result.current.navGroups
    const generalAfter = groups.find((group) => group.id === 'general')
    const resources = groups.find((group) => group.id === 'resources')
    expect(
      generalAfter?.items.some((item) => item.url === '/account-pool')
    ).toBe(false)
    expect(groups.map((group) => group.id)).toEqual([
      'chat',
      'general',
      'personal',
      'resources',
      'admin',
    ])
    expect(resources?.title).toBe('Resources')
    expect(resources?.items).toHaveLength(1)
    expect(resources?.items[0]).toMatchObject({
      title: 'Account Pool',
      url: '/account-pool',
      icon: Timeline,
    })
  })
})
