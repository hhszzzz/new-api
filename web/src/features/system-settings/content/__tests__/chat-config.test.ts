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
import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import {
  buildChatGroupOptions,
  parseChatEntries,
  serializeChatEntry,
} from '../chat-config'

describe('chat preset group restrictions', () => {
  test('parses legacy public presets and group-restricted presets', () => {
    const entries = parseChatEntries(
      JSON.stringify([
        { Public: 'https://public.example.com' },
        {
          VIP: {
            url: 'https://vip.example.com',
            groups: [' vip ', 'vip', 'pro'],
          },
        },
      ])
    )

    assert.deepEqual(entries, [
      {
        name: 'Public',
        url: 'https://public.example.com',
        groups: [],
      },
      {
        name: 'VIP',
        url: 'https://vip.example.com',
        groups: ['vip', 'pro'],
      },
    ])
  })

  test('keeps unrestricted presets backward compatible when serializing', () => {
    assert.deepEqual(
      serializeChatEntry({
        name: 'Public',
        url: 'https://public.example.com',
        groups: [],
      }),
      { Public: 'https://public.example.com' }
    )
    assert.deepEqual(
      serializeChatEntry({
        name: 'VIP',
        url: 'https://vip.example.com',
        groups: [' vip ', 'vip'],
      }),
      {
        VIP: {
          url: 'https://vip.example.com',
          groups: ['vip'],
        },
      }
    )
  })

  test('offers only configured billing groups as restriction choices', () => {
    assert.deepEqual(
      buildChatGroupOptions(
        JSON.stringify({ vip: 2, default: 1, auto: 1 }),
        JSON.stringify({ vip: 'VIP users', legacy: 'Legacy users' })
      ),
      [
        { value: 'default', label: 'default' },
        { value: 'vip', label: 'vip — VIP users' },
      ]
    )
  })
})
