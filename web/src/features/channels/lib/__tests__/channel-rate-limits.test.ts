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
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
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
