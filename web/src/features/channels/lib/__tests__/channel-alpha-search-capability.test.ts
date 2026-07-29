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

import type { Channel } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
  transformFormDataToUpdatePayload,
} from '../channel-form'

function openAIChannel(settings: string): Channel {
  return {
    id: 11,
    type: 1,
    key: '',
    status: 1,
    name: 'alpha-search-test',
    weight: 1,
    created_time: 0,
    test_time: 0,
    response_time: 0,
    base_url: 'https://openai-compatible.example',
    other: '',
    balance: 0,
    balance_updated_time: 0,
    models: 'gpt-5-codex',
    group: 'default',
    used_quota: 0,
    priority: 0,
    other_info: '',
    remark: '',
    inherit_aggregate_base_url: false,
    max_input_tokens: 0,
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: 'random',
    },
    schedule: {
      timezone: 'Asia/Shanghai',
      weekly_enabled: false,
      weekly_windows: {},
    },
    settings,
  }
}

describe('OpenAI Alpha Search capability form contract', () => {
  test('defaults to disabled and round-trips an explicit opt-in', () => {
    expect(CHANNEL_FORM_DEFAULT_VALUES.allow_alpha_search).toBe(false)

    const defaults = transformChannelToFormDefaults(
      openAIChannel(JSON.stringify({ allow_alpha_search: true }))
    )
    expect(defaults.allow_alpha_search).toBe(true)

    const payload = transformFormDataToUpdatePayload(defaults, 11)
    expect(JSON.parse(payload.settings || '{}').allow_alpha_search).toBe(true)
  })

  test('removes the capability when the channel is no longer OpenAI', () => {
    const payload = transformFormDataToUpdatePayload(
      {
        ...CHANNEL_FORM_DEFAULT_VALUES,
        type: 14,
        settings: JSON.stringify({ allow_alpha_search: true }),
        allow_alpha_search: true,
      },
      11
    )

    expect(
      JSON.parse(payload.settings || '{}').allow_alpha_search
    ).toBeUndefined()
  })
})
