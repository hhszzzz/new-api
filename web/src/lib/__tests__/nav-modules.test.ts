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

import { describe, test, vi } from 'vitest'

vi.mock('@/lib/api', () => ({
  getStatus: async () => null,
}))

const {
  getCustomHeaderNavItemFromStatus,
  getModuleAccessFromStatus,
  parseHeaderNavModules,
} = await import('../nav-modules.ts')

describe('runtime header navigation model status configuration', () => {
  test('defaults model status to enabled and public when no navigation configuration exists', () => {
    assert.deepEqual(getModuleAccessFromStatus(null, 'modelStatus'), {
      enabled: true,
      requireAuth: false,
    })
  })

  test('inherits model square access when a legacy configuration omits model status', () => {
    const modelStatus = getModuleAccessFromStatus(
      {
        HeaderNavModules: JSON.stringify({
          pricing: { enabled: false, requireAuth: true },
        }),
      },
      'modelStatus'
    )

    assert.deepEqual(modelStatus, {
      enabled: false,
      requireAuth: true,
    })
  })

  test('inherits the model square login requirement for a legacy model-status boolean', () => {
    const config = parseHeaderNavModules(
      JSON.stringify({
        pricing: { enabled: true, requireAuth: true },
        modelStatus: true,
      })
    )

    assert.deepEqual(config.modelStatus, {
      enabled: true,
      requireAuth: true,
    })
  })

  test('inherits omitted model-status fields from model square access', () => {
    const config = parseHeaderNavModules(
      JSON.stringify({
        pricing: { enabled: false, requireAuth: true },
        modelStatus: { requireAuth: false },
      })
    )

    assert.deepEqual(config.modelStatus, {
      enabled: false,
      requireAuth: false,
    })
  })

  test('parses model status independently from model square access', () => {
    const config = parseHeaderNavModules(
      JSON.stringify({
        pricing: { enabled: false, requireAuth: false },
        modelStatus: { enabled: true, requireAuth: true },
      })
    )

    assert.deepEqual(config.pricing, {
      enabled: false,
      requireAuth: false,
    })
    assert.deepEqual(config.modelStatus, {
      enabled: true,
      requireAuth: true,
    })
  })

  test('uses safe defaults for unsupported numeric access values', () => {
    const config = parseHeaderNavModules(
      JSON.stringify({
        modelStatus: { enabled: 2, requireAuth: -1 },
      })
    )

    assert.deepEqual(config.modelStatus, {
      enabled: true,
      requireAuth: false,
    })
  })

  test('model radar inherits model status for legacy configurations and supports overrides', () => {
    const inherited = parseHeaderNavModules(
      JSON.stringify({
        pricing: { enabled: true, requireAuth: false },
        modelStatus: { enabled: false, requireAuth: true },
      })
    )
    assert.deepEqual(inherited.modelRadar, {
      enabled: false,
      requireAuth: true,
    })

    const overridden = parseHeaderNavModules(
      JSON.stringify({
        modelStatus: { enabled: false, requireAuth: true },
        modelRadar: { enabled: true, requireAuth: false },
      })
    )
    assert.deepEqual(overridden.modelRadar, {
      enabled: true,
      requireAuth: false,
    })
  })

  test('parses custom iframe links and restores a complete navigation order', () => {
    const config = parseHeaderNavModules(
      JSON.stringify({
        custom: [
          {
            id: 'docs-hub',
            title: ' Docs Hub ',
            url: ' https://docs.example.com/app ',
            enabled: true,
          },
          {
            id: 'unsafe',
            title: 'Unsafe',
            url: 'javascript:alert(1)',
            enabled: true,
          },
        ],
        order: ['custom:docs-hub', 'home', 'custom:missing'],
      })
    )

    assert.deepEqual(config.custom, [
      {
        id: 'docs-hub',
        title: 'Docs Hub',
        url: 'https://docs.example.com/app',
        enabled: true,
      },
    ])
    assert.deepEqual(config.order.slice(0, 3), [
      'custom:docs-hub',
      'home',
      'console',
    ])
  })

  test('resolves only enabled custom navigation from public status', () => {
    const status = {
      HeaderNavModules: JSON.stringify({
        custom: [
          {
            id: 'portal',
            title: 'Portal',
            url: 'https://portal.example.com',
            enabled: true,
          },
          {
            id: 'disabled',
            title: 'Disabled',
            url: 'https://disabled.example.com',
            enabled: false,
          },
        ],
      }),
    }

    assert.equal(
      getCustomHeaderNavItemFromStatus(status, 'portal')?.title,
      'Portal'
    )
    assert.equal(getCustomHeaderNavItemFromStatus(status, 'disabled'), null)
  })
})
