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
  HEADER_NAV_DEFAULT,
  parseHeaderNavModules,
  serializeHeaderNavModules,
} from '../config.ts'

describe('header navigation model status configuration', () => {
  test('defaults model status to enabled and public when no navigation configuration exists', () => {
    const config = parseHeaderNavModules(null)

    assert.deepEqual(config.modelStatus, {
      enabled: true,
      requireAuth: false,
    })
  })

  test('inherits model square access when a legacy configuration omits model status', () => {
    const config = parseHeaderNavModules(
      JSON.stringify({ pricing: { enabled: false, requireAuth: true } })
    )

    assert.deepEqual(config.modelStatus, {
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
        pricing: { enabled: true, requireAuth: false },
        modelStatus: { enabled: false, requireAuth: true },
      })
    )

    assert.deepEqual(config.pricing, {
      enabled: true,
      requireAuth: false,
    })
    assert.deepEqual(config.modelStatus, {
      enabled: false,
      requireAuth: true,
    })
  })

  test('preserves model status access when serializing settings', () => {
    const serialized = serializeHeaderNavModules({
      ...HEADER_NAV_DEFAULT,
      modelStatus: { enabled: true, requireAuth: true },
    })

    assert.deepEqual(JSON.parse(serialized).modelStatus, {
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

  test('model radar inherits model status and serializes independent access', () => {
    const inherited = parseHeaderNavModules(
      JSON.stringify({
        modelStatus: { enabled: false, requireAuth: true },
      })
    )
    assert.deepEqual(inherited.modelRadar, {
      enabled: false,
      requireAuth: true,
    })

    const serialized = serializeHeaderNavModules({
      ...HEADER_NAV_DEFAULT,
      modelRadar: { enabled: true, requireAuth: true },
    })
    assert.deepEqual(JSON.parse(serialized).modelRadar, {
      enabled: true,
      requireAuth: true,
    })
  })

  test('preserves custom navigation and its position when parsing and serializing', () => {
    const config = parseHeaderNavModules(
      JSON.stringify({
        custom: [
          {
            id: 'portal',
            title: 'Team Portal',
            url: 'https://portal.example.com',
            enabled: true,
          },
        ],
        order: ['home', 'custom:portal', 'console'],
      })
    )

    assert.deepEqual(config.custom, [
      {
        id: 'portal',
        title: 'Team Portal',
        url: 'https://portal.example.com',
        enabled: true,
      },
    ])
    assert.deepEqual(config.order.slice(0, 3), [
      'home',
      'custom:portal',
      'console',
    ])
    assert.deepEqual(
      JSON.parse(serializeHeaderNavModules(config)).custom,
      config.custom
    )
  })
})
