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

import {
  transformFormDataToPayload,
  transformUserToFormDefaults,
  type UserFormValues,
} from '../user-form'

const BASE_FORM: UserFormValues = {
  username: 'policy-user',
  display_name: 'Policy User',
  password: '',
  role: 1,
  quota_dollars: 0,
  group: 'legacy',
  groups: ['legacy', 'premium', 'batch'],
  primary_group: 'premium',
  model_limits_enabled: false,
  model_limits: [],
  model_blocklist_enabled: false,
  model_blocklist: [],
  remark: '',
  admin_permissions: {},
}

describe('user policy form transformations', () => {
  test('keeps selected groups and the explicit primary group without sending legacy top-up state', () => {
    const payload = transformFormDataToPayload(BASE_FORM, 42)

    expect(payload).toMatchObject({
      id: 42,
      group: 'premium',
      groups: ['legacy', 'premium', 'batch'],
      primary_group: 'premium',
    })
    expect(payload).not.toHaveProperty('role')
    expect(payload).not.toHaveProperty('topup_group')
  })

  test('preserves an enabled empty model allowlist as deny-all', () => {
    const payload = transformFormDataToPayload({
      ...BASE_FORM,
      model_limits_enabled: true,
      model_limits: [],
    })

    expect(payload.model_limits_enabled).toBe(true)
    expect(payload.model_limits).toEqual([])
  })

  test('preserves the selected model blocklist independently from the allowlist', () => {
    const payload = transformFormDataToPayload({
      ...BASE_FORM,
      model_limits_enabled: true,
      model_limits: ['gpt-5.4', 'gpt-5.5'],
      model_blocklist_enabled: true,
      model_blocklist: ['gpt-5.5'],
    })

    expect(payload.model_limits).toEqual(['gpt-5.4', 'gpt-5.5'])
    expect(payload.model_blocklist_enabled).toBe(true)
    expect(payload.model_blocklist).toEqual(['gpt-5.5'])
  })

  test('hydrates policy fields without falling back to the legacy group', () => {
    const defaults = transformUserToFormDefaults({
      id: 7,
      username: 'hydrated-user',
      display_name: 'Hydrated User',
      quota: 0,
      used_quota: 0,
      request_count: 0,
      group: 'premium',
      groups: ['premium', 'legacy'],
      topup_group: 'legacy',
      model_limits_enabled: true,
      model_limits: ['gpt-5.4'],
      model_blocklist_enabled: true,
      model_blocklist: ['gpt-5.5'],
      status: 1,
      role: 1,
    })

    expect(defaults.groups).toEqual(['premium', 'legacy'])
    expect(defaults.primary_group).toBe('premium')
    expect(defaults).not.toHaveProperty('topup_group')
    expect(defaults.model_limits_enabled).toBe(true)
    expect(defaults.model_limits).toEqual(['gpt-5.4'])
    expect(defaults.model_blocklist_enabled).toBe(true)
    expect(defaults.model_blocklist).toEqual(['gpt-5.5'])
  })
})
