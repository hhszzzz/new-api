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
  getAdvancedCustomConverterDefaults,
  getAdvancedCustomConverterOptions,
  getDefaultAdvancedCustomIncomingPath,
  isAdvancedCustomIncomingPathAllowed,
} from '../advanced-custom'

describe('Advanced Custom protocol bridge converters', () => {
  test('offers Messages to Responses only for the Messages entrypoint', () => {
    const converter = 'claude_messages_to_openai_responses' as const

    expect(getDefaultAdvancedCustomIncomingPath(converter)).toBe('/v1/messages')
    expect(isAdvancedCustomIncomingPathAllowed('/v1/messages', converter)).toBe(
      true
    )
    expect(
      isAdvancedCustomIncomingPathAllowed('/v1/responses', converter)
    ).toBe(false)
    expect(
      getAdvancedCustomConverterOptions('/v1/messages').map(
        (option) => option.value
      )
    ).toContain(converter)
    expect(
      getAdvancedCustomConverterDefaults(converter, '/v1/messages')
    ).toEqual({
      upstream_path: '/v1/responses',
      auth: {
        type: 'header',
        name: 'Authorization',
        value: 'Bearer {api_key}',
      },
    })
  })

  test('offers Responses to Messages only for the Responses entrypoint', () => {
    const converter = 'openai_responses_to_claude_messages' as const

    expect(getDefaultAdvancedCustomIncomingPath(converter)).toBe(
      '/v1/responses'
    )
    expect(
      isAdvancedCustomIncomingPathAllowed('/v1/responses', converter)
    ).toBe(true)
    expect(isAdvancedCustomIncomingPathAllowed('/v1/messages', converter)).toBe(
      false
    )
    expect(
      getAdvancedCustomConverterOptions('/v1/responses').map(
        (option) => option.value
      )
    ).toContain(converter)
    expect(
      getAdvancedCustomConverterDefaults(converter, '/v1/responses')
    ).toEqual({
      upstream_path: '/v1/messages',
      auth: {
        type: 'header',
        name: 'x-api-key',
        value: '{api_key}',
      },
    })
  })
})
