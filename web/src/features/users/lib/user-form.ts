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
import { z } from 'zod'

import {
  type PermissionCatalog,
  type AdminPermissionMatrix,
  normalizeAdminPermissions,
} from '@/lib/admin-permissions'
import { quotaUnitsToDollars } from '@/lib/format'
import { ROLE } from '@/lib/roles'

import { DEFAULT_GROUP } from '../constants'
import type { User, UserFormData } from '../types'

// ============================================================================
// Form Schema
// ============================================================================

export const userFormSchema = z.object({
  username: z.string().min(1, 'Username is required'),
  display_name: z.string().optional(),
  password: z.string().optional(),
  role: z.number().optional(),
  quota_dollars: z.number().min(0).optional(),
  group: z.string().optional(),
  groups: z.array(z.string()).optional(),
  primary_group: z.string().optional(),
  model_limits_enabled: z.boolean().optional(),
  model_limits: z.array(z.string()).optional(),
  model_blocklist_enabled: z.boolean().optional(),
  model_blocklist: z.array(z.string()).optional(),
  remark: z.string().optional(),
  admin_permissions: z
    .record(z.string(), z.record(z.string(), z.boolean()))
    .optional(),
})

export type UserFormValues = z.infer<typeof userFormSchema>

// ============================================================================
// Form Defaults
// ============================================================================

export const USER_FORM_DEFAULT_VALUES: UserFormValues = {
  username: '',
  display_name: '',
  password: '',
  role: 1, // Default to common user
  quota_dollars: 0,
  group: DEFAULT_GROUP,
  groups: [DEFAULT_GROUP],
  primary_group: DEFAULT_GROUP,
  model_limits_enabled: false,
  model_limits: [],
  model_blocklist_enabled: false,
  model_blocklist: [],
  remark: '',
  // Filled against the backend catalog at render time; see UsersMutateDrawer.
  admin_permissions: {},
}

// ============================================================================
// Form Data Transformation
// ============================================================================

/**
 * Transform form data to API payload
 */
export function transformFormDataToPayload(
  data: UserFormValues,
  userId?: number,
  catalog?: PermissionCatalog
): UserFormData & { id?: number } {
  const payload: UserFormData & { id?: number } = {
    username: data.username,
    display_name: data.display_name || data.username,
    password: data.password || undefined,
  }

  const role = userId === undefined ? data.role || 1 : (data.role ?? 0)

  // Only send the permission matrix when the target is an admin and the catalog
  // is available; without the catalog we cannot build a full matrix, so we omit
  // the field (the backend then leaves existing permissions untouched).
  if (role >= ROLE.ADMIN && catalog) {
    payload.admin_permissions = normalizeAdminPermissions(
      data.admin_permissions as AdminPermissionMatrix | undefined,
      catalog
    )
  }

  payload.groups = data.groups || []
  payload.primary_group = data.primary_group || data.groups?.[0] || data.group
  payload.model_limits_enabled = data.model_limits_enabled === true
  payload.model_limits = data.model_limits || []
  payload.model_blocklist_enabled = data.model_blocklist_enabled === true
  payload.model_blocklist = data.model_blocklist || []

  // Profile and policy fields are accepted by both create and update APIs so
  // the backend can persist them atomically.
  if (userId === undefined) {
    payload.role = role
    payload.group = payload.primary_group
  } else {
    // For update: quota is adjusted atomically via /api/user/manage, not sent here
    payload.group = payload.primary_group
    payload.remark = data.remark || undefined
    payload.id = userId
  }

  return payload
}

/**
 * Transform user data to form defaults. The admin permission matrix is passed
 * through as-is (the backend already returns a full matrix); it is filled against
 * the catalog at render time in UsersMutateDrawer.
 */
export function transformUserToFormDefaults(user: User): UserFormValues {
  const groups = user.groups?.length
    ? user.groups
    : [user.group || DEFAULT_GROUP]
  return {
    username: user.username,
    display_name: user.display_name,
    password: '',
    role: user.role,
    quota_dollars: quotaUnitsToDollars(user.quota),
    group: user.group || DEFAULT_GROUP,
    groups,
    primary_group: user.group || groups[0] || DEFAULT_GROUP,
    model_limits_enabled: user.model_limits_enabled ?? false,
    model_limits: user.model_limits ?? [],
    model_blocklist_enabled: user.model_blocklist_enabled ?? false,
    model_blocklist: user.model_blocklist ?? [],
    remark: user.remark || '',
    admin_permissions: user.admin_permissions ?? {},
  }
}
