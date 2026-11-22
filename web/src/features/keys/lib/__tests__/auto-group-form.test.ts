/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import type { TFunction } from 'i18next'
import { describe, expect, test } from 'vitest'

import type { ApiKey } from '../../types'
import {
  getApiKeyFormDefaultValues,
  getApiKeyFormSchema,
  transformApiKeyToFormDefaults,
  transformFormDataToPayload,
} from '../api-key-form'

const t = ((key: string) => key) as TFunction

const baseApiKey: ApiKey = {
  id: 1,
  name: 'existing key',
  key: 'secret',
  status: 1,
  remain_quota: 0,
  used_quota: 0,
  unlimited_quota: true,
  expired_time: -1,
  created_time: 0,
  accessed_time: 0,
  group: 'auto',
  auto_groups: ['vip', 'default'],
  cross_group_retry: true,
  model_limits_enabled: false,
  model_limits: '',
  allow_ips: '',
  allow_user_quota_query: true,
}

describe('API key Auto group configuration', () => {
  test('defaults Auto tokens to every authorized candidate in order', () => {
    const defaults = getApiKeyFormDefaultValues(true, 'default', [
      'default',
      'vip',
      'team',
    ])

    expect(defaults.group).toBe('auto')
    expect(defaults.auto_groups).toEqual(['default', 'vip', 'team'])
    expect(defaults.cross_group_retry).toBe(true)
    expect(transformFormDataToPayload(defaults).auto_groups).toEqual([
      'default',
      'vip',
      'team',
    ])
  })

  test('defaults manual tokens to the primary group', () => {
    const defaults = getApiKeyFormDefaultValues(false, 'premium', ['premium'])

    expect(defaults.group).toBe('premium')
    expect(defaults.auto_groups).toEqual(['premium'])
    expect(transformFormDataToPayload(defaults).auto_groups).toEqual([])
    expect(transformFormDataToPayload(defaults).cross_group_retry).toBe(false)
  })

  test('preserves the selected Auto sequence when editing and saving', () => {
    const defaults = transformApiKeyToFormDefaults(baseApiKey, [
      'default',
      'vip',
      'team',
    ])

    expect(defaults.auto_groups).toEqual(['vip', 'default'])
    expect(transformFormDataToPayload(defaults).auto_groups).toEqual([
      'vip',
      'default',
    ])
  })

  test('filters groups that are no longer authorized and requires a candidate', () => {
    const defaults = transformApiKeyToFormDefaults(
      { ...baseApiKey, auto_groups: ['revoked'] },
      ['default']
    )

    expect(defaults.auto_groups).toEqual([])
    const result = getApiKeyFormSchema(t).safeParse(defaults)
    expect(result.success).toBe(false)
    if (result.success) return
    expect(result.error.issues[0]?.path).toEqual(['auto_groups'])
    expect(result.error.issues[0]?.message).toBe(
      'Select at least one Auto group.'
    )
  })

  test('accepts a long candidate list and rejects duplicates', () => {
    const groups = Array.from({ length: 20 }, (_, index) => `group-${index}`)
    const defaults = {
      ...getApiKeyFormDefaultValues(true, 'default', groups),
      name: 'large list',
    }
    expect(getApiKeyFormSchema(t).safeParse(defaults).success).toBe(true)

    const duplicated = { ...defaults, auto_groups: ['vip', 'vip'] }
    const result = getApiKeyFormSchema(t).safeParse(duplicated)
    expect(result.success).toBe(false)
    if (result.success) return
    expect(result.error.issues[0]?.message).toBe(
      'Auto groups must not contain duplicates'
    )
  })
})

describe('API key user quota query form mapping', () => {
  test('lets a new key query the user quota by default', () => {
    const defaults = getApiKeyFormDefaultValues(true, 'default', ['default'])
    expect(defaults.allow_user_quota_query).toBe(true)
    expect(transformFormDataToPayload(defaults).allow_user_quota_query).toBe(
      true
    )
  })

  test.each([true, false])(
    'keeps the stored value %s when an existing key is edited and saved',
    (stored) => {
      const defaults = transformApiKeyToFormDefaults({
        ...baseApiKey,
        allow_user_quota_query: stored,
      })

      expect(defaults.allow_user_quota_query).toBe(stored)
      expect(transformFormDataToPayload(defaults).allow_user_quota_query).toBe(
        stored
      )
    }
  )
})
