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

import { protocolCatalogFixture } from '@/features/protocols/__tests__/fixtures'

import {
  getAdvancedCustomTargetDefaults,
  getAdvancedCustomTargetOptions,
  parseAdvancedCustomConfig,
  stringifyAdvancedCustomConfig,
  validateAdvancedCustomConfig,
} from '../advanced-custom'

const catalog = protocolCatalogFixture.catalog

describe('Advanced Custom catalog routes', () => {
  test('offers only registered destinations for the incoming operation', () => {
    expect(
      getAdvancedCustomTargetOptions('/v1/messages', catalog).map(
        (option) => option.value
      )
    ).toEqual(['native', 'messages', 'responses'])
    expect(
      getAdvancedCustomTargetOptions('/v1/responses/compact', catalog).map(
        (option) => option.value
      )
    ).toEqual(['native', 'messages', 'responses'])
    expect(
      getAdvancedCustomTargetDefaults('messages', '/v1/responses', catalog)
    ).toEqual({
      upstream_path: '/v1/messages',
      auth: { type: 'header', name: 'x-api-key', value: '{api_key}' },
    })
  })

  test('imports legacy aliases without changing paths, model ordering, or authentication', () => {
    const route = {
      incoming_path: '/v1/messages',
      upstream_path: '/proxy/responses',
      converter: 'claude_messages_to_openai_responses',
      models: ['re:^private-', 'public-model'],
      auth: { type: 'header', name: 'X-Provider-Key', value: '{api_key}' },
    }
    const config = parseAdvancedCustomConfig(
      JSON.stringify({ advanced_routes: [route] }),
      catalog
    )
    expect(config?.advanced_routes).toEqual([
      {
        incoming_path: route.incoming_path,
        upstream_path: route.upstream_path,
        target_protocol: 'responses',
        models: route.models,
        auth: route.auth,
      },
    ])
    expect(validateAdvancedCustomConfig(config, catalog)).toBeNull()
    if (!config) throw new Error('Imported route configuration is missing')
    expect(JSON.parse(stringifyAdvancedCustomConfig(config, catalog))).toEqual(
      config
    )
  })

  test('preserves an unknown legacy converter for backend validation instead of changing it to native', () => {
    const config = parseAdvancedCustomConfig(
      '{"advanced_routes":[{"incoming_path":"/v1/messages","upstream_path":"/proxy","converter":"unrecognized"}]}',
      catalog
    )
    expect(config?.advanced_routes?.[0].converter).toBe('unrecognized')
    expect(validateAdvancedCustomConfig(config, catalog)?.message).toBe(
      'Converter is not registered'
    )
  })

  test('rejects conflicting converter targets and permits model summary compaction', () => {
    expect(
      validateAdvancedCustomConfig(
        {
          advanced_routes: [
            {
              incoming_path: '/v1/messages',
              upstream_path: '/proxy',
              converter: 'claude_messages_to_openai_responses',
              target_protocol: 'native',
            },
          ],
        },
        catalog
      )?.message
    ).toBe('Target protocol conflicts with legacy converter')
    expect(
      validateAdvancedCustomConfig(
        {
          advanced_routes: [
            {
              incoming_path: '/v1/responses/compact',
              upstream_path: '/v1/messages',
              target_protocol: 'messages',
            },
          ],
        },
        catalog
      )
    ).toBeNull()
  })
})
