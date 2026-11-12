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
  channelSchema,
  type Channel,
  type ChannelUserRateLimit,
} from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
} from '../channel-form'

const BASE_CHANNEL_FORM = {
  ...CHANNEL_FORM_DEFAULT_VALUES,
  name: 'limited-channel',
  key: 'sk-test',
  models: 'gpt-4.1',
  group: ['default'],
}

const PER_USER_LIMITS: ChannelUserRateLimit[] = [
  { user_id: 7, username: 'alice', rpm_limit: 30, priority: 100 },
  { user_id: 8, concurrency_limit: 2 },
]

function channelWithUserRateLimits(
  userRateLimits: ChannelUserRateLimit[]
): Channel {
  return channelSchema.parse({
    id: 9,
    type: 1,
    key: '',
    status: 1,
    concurrency_limit: 3,
    name: 'limited-channel',
    created_time: 0,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    models: 'gpt-4.1',
    group: 'default',
    user_rate_limits: userRateLimits,
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: 'random',
    },
  })
}

describe('channel request-limit form contract', () => {
  test('accepts only positive int32 channel limits', () => {
    expect(
      channelFormSchema.safeParse({
        ...BASE_CHANNEL_FORM,
        rpm_limit: 1,
        concurrency_limit: 2_147_483_647,
      }).success
    ).toBe(true)

    for (const value of [0, -1, 1.5, 2_147_483_648]) {
      expect(
        channelFormSchema.safeParse({
          ...BASE_CHANNEL_FORM,
          rpm_limit: value,
        }).success
      ).toBe(false)
    }
  })

  test('sends custom values and explicit nulls when limits are cleared', () => {
    const custom = transformFormDataToCreatePayload({
      ...BASE_CHANNEL_FORM,
      rpm_limit: 60,
      concurrency_limit: 2,
    })
    expect(custom.channel).toMatchObject({
      rpm_limit: 60,
      concurrency_limit: 2,
    })

    const cleared = transformFormDataToUpdatePayload(BASE_CHANNEL_FORM, 9)
    expect(cleared).toMatchObject({
      id: 9,
      rpm_limit: null,
      concurrency_limit: null,
    })
  })
})

describe('channel per-user limit form contract', () => {
  test('loads stored overrides into the form and sends them back unchanged', () => {
    const defaults = transformChannelToFormDefaults(
      channelWithUserRateLimits(PER_USER_LIMITS)
    )
    expect(defaults.user_rate_limits).toEqual(PER_USER_LIMITS)

    const values = { ...defaults, user_rate_limits: PER_USER_LIMITS }
    expect(channelFormSchema.safeParse(values).success).toBe(true)

    const created = transformFormDataToCreatePayload(values)
    expect(created.channel.user_rate_limits).toEqual(PER_USER_LIMITS)

    const updated = transformFormDataToUpdatePayload(values, 9)
    expect(updated.user_rate_limits).toEqual(PER_USER_LIMITS)
  })

  test('sends an empty array when every override is removed', () => {
    const cleared = transformFormDataToUpdatePayload(BASE_CHANNEL_FORM, 9)
    expect(cleared.user_rate_limits).toEqual([])
  })

  test('rejects an override that configures neither RPM nor concurrency', () => {
    expect(
      channelFormSchema.safeParse({
        ...BASE_CHANNEL_FORM,
        user_rate_limits: [{ user_id: 7 }],
      }).success
    ).toBe(false)
  })

  test('accepts a priority-only override when channel concurrency is limited', () => {
    const values = {
      ...BASE_CHANNEL_FORM,
      concurrency_limit: 3,
      user_rate_limits: [{ user_id: 7, priority: 100 }],
    }
    expect(channelFormSchema.safeParse(values).success).toBe(true)
    expect(
      transformFormDataToUpdatePayload(values, 9).user_rate_limits
    ).toEqual([{ user_id: 7, priority: 100 }])
  })

  test('rejects priorities outside the supported numeric range', () => {
    for (const priority of [-2_147_483_648, 2_147_483_648]) {
      expect(
        channelFormSchema.safeParse({
          ...BASE_CHANNEL_FORM,
          concurrency_limit: 3,
          user_rate_limits: [{ user_id: 7, priority }],
        }).success
      ).toBe(false)
    }
  })

  test('requires channel concurrency when a user priority is configured', () => {
    const priorityOnly = {
      ...BASE_CHANNEL_FORM,
      user_rate_limits: [{ user_id: 7, priority: 100 }],
    }
    expect(channelFormSchema.safeParse(priorityOnly).success).toBe(false)
    expect(
      channelFormSchema.safeParse({ ...priorityOnly, concurrency_limit: 3 })
        .success
    ).toBe(true)
  })

  test('rejects non-positive user ids and limit values', () => {
    for (const override of [
      { user_id: 0, rpm_limit: 10 },
      { user_id: -1, concurrency_limit: 1 },
      { user_id: 7, rpm_limit: 0 },
      { user_id: 7, concurrency_limit: -1 },
    ]) {
      expect(
        channelFormSchema.safeParse({
          ...BASE_CHANNEL_FORM,
          user_rate_limits: [override],
        }).success
      ).toBe(false)
    }
  })
})
