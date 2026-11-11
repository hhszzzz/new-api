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
  ADVANCED_CUSTOM_INCOMING_PATH_OPTIONS,
  getAdvancedCustomTarget,
  getAdvancedCustomTargetDefaults,
  getAdvancedCustomTargetOptions,
  getAdvancedCustomIncomingPathLabel,
  validateAdvancedCustomConfig,
} from '../advanced-custom'

describe('advanced custom incoming path options', () => {
  test('the web search and web fetch paths use native protocol forwarding', () => {
    const values = ADVANCED_CUSTOM_INCOMING_PATH_OPTIONS.map(
      (option) => option.value
    )

    expect(values).toContain('/v1/tools/search')
    expect(values).toContain('/v1/tools/fetch')
    for (const path of ['/v1/tools/search', '/v1/tools/fetch']) {
      const route = {
        incoming_path: path,
        upstream_path: path,
        target_protocol: 'native',
      }
      expect(getAdvancedCustomTarget(route)).toBe('native')
      expect(getAdvancedCustomTargetOptions(path)).toEqual([
        { value: 'native', label: 'Native forwarding' },
      ])
      expect(
        validateAdvancedCustomConfig({ advanced_routes: [route] })
      ).toBeNull()
    }
  })

  test('the web search and web fetch paths are labelled after DeepSeek', () => {
    expect(getAdvancedCustomIncomingPathLabel('/v1/tools/search')).toBe(
      'DeepSeek Search'
    )
    expect(getAdvancedCustomIncomingPathLabel('/v1/tools/fetch')).toBe(
      'DeepSeek Fetch'
    )
  })

  test('native forwarding keeps each tool path and bearer authentication', () => {
    for (const path of ['/v1/tools/search', '/v1/tools/fetch']) {
      expect(getAdvancedCustomTargetDefaults('native', path)).toEqual({
        upstream_path: path,
        auth: {
          type: 'header',
          name: 'Authorization',
          value: 'Bearer {api_key}',
        },
      })
    }
  })

  test('the tool route labels remain provider-specific', () => {
    expect(getAdvancedCustomIncomingPathLabel('/v1/tools/search')).toBe(
      'DeepSeek Search'
    )
    expect(getAdvancedCustomIncomingPathLabel('/v1/tools/fetch')).toBe(
      'DeepSeek Fetch'
    )
  })
})
