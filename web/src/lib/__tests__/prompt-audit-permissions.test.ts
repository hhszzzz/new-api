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
import { describe, expect, test } from 'vitest'

import type { AuthUser } from '@/stores/auth-store'

import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '../admin-permissions'
import { ROLE } from '../roles'

function adminWith(grants: Record<string, boolean>): AuthUser {
  return {
    id: 7,
    username: 'auditor',
    role: ROLE.ADMIN,
    permissions: {
      admin_permissions: {
        [ADMIN_PERMISSION_RESOURCES.PROMPT_AUDIT]: grants,
      },
    },
  }
}

describe('prompt audit frontend permission visibility', () => {
  test('lets an ordinary administrator receive each capability independently', () => {
    const user = adminWith({
      read: true,
      view_full_prompt: false,
      manage: true,
      delete: false,
    })

    expect(
      hasPermission(
        user,
        ADMIN_PERMISSION_RESOURCES.PROMPT_AUDIT,
        ADMIN_PERMISSION_ACTIONS.READ
      )
    ).toBe(true)
    expect(
      hasPermission(
        user,
        ADMIN_PERMISSION_RESOURCES.PROMPT_AUDIT,
        ADMIN_PERMISSION_ACTIONS.MANAGE
      )
    ).toBe(true)
    expect(
      hasPermission(
        user,
        ADMIN_PERMISSION_RESOURCES.PROMPT_AUDIT,
        ADMIN_PERMISSION_ACTIONS.VIEW_FULL_PROMPT
      )
    ).toBe(false)
    expect(
      hasPermission(
        user,
        ADMIN_PERMISSION_RESOURCES.PROMPT_AUDIT,
        ADMIN_PERMISSION_ACTIONS.DELETE
      )
    ).toBe(false)
  })

  test('honors an explicit read revocation for an ordinary administrator', () => {
    const user = adminWith({ read: false })
    expect(
      hasPermission(
        user,
        ADMIN_PERMISSION_RESOURCES.PROMPT_AUDIT,
        ADMIN_PERMISSION_ACTIONS.READ
      )
    ).toBe(false)
  })

  test('keeps root as the superuser for every prompt audit action', () => {
    const root: AuthUser = { id: 1, username: 'root', role: ROLE.SUPER_ADMIN }
    for (const action of [
      ADMIN_PERMISSION_ACTIONS.READ,
      ADMIN_PERMISSION_ACTIONS.VIEW_FULL_PROMPT,
      ADMIN_PERMISSION_ACTIONS.MANAGE,
      ADMIN_PERMISSION_ACTIONS.DELETE,
    ]) {
      expect(
        hasPermission(root, ADMIN_PERMISSION_RESOURCES.PROMPT_AUDIT, action)
      ).toBe(true)
    }
  })
})
