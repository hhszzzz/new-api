/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or (at your option) any later version.

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
  parseModelPricingNumber,
  reconcileModelPricingMaps,
  type ModelPricingMaps,
} from '../model-pricing-migration'

function emptyPricingMaps(): ModelPricingMaps {
  return {
    price: {},
    ratio: {},
    cache: {},
    completion: {},
    image: {},
    audio: {},
    audioCompletion: {},
  }
}

describe('reconcileModelPricingMaps', () => {
  test('preserves target pricing when a priced model is renamed onto it', () => {
    const maps: ModelPricingMaps = {
      ...emptyPricingMaps(),
      price: { source: 1, target: 9 },
      ratio: { source: 2 },
      cache: { target: 0 },
    }

    const result = reconcileModelPricingMaps({
      maps,
      draft: { ratio: '2', completionRatio: '3' },
      mode: 'per-token',
      isEditing: true,
      sourceName: 'source',
      targetName: 'target',
      loadedPricingName: 'source',
    })

    expect(result.price).toEqual({ target: 9 })
    expect(result.ratio).toEqual({})
    expect(result.cache).toEqual({ target: 0 })
    expect(result.completion).toEqual({})
    expect(maps.price).toEqual({ source: 1, target: 9 })
  })

  test('migrates source pricing when the rename target has no pricing', () => {
    const result = reconcileModelPricingMaps({
      maps: {
        ...emptyPricingMaps(),
        ratio: { source: 2 },
        cache: { source: 0 },
      },
      draft: { ratio: '2', cacheRatio: '0' },
      mode: 'per-token',
      isEditing: true,
      sourceName: 'source',
      targetName: 'target',
      loadedPricingName: 'source',
    })

    expect(result.ratio).toEqual({ target: 2 })
    expect(result.cache).toEqual({ target: 0 })
  })

  test('keeps existing pricing for an unpriced create form', () => {
    const result = reconcileModelPricingMaps({
      maps: { ...emptyPricingMaps(), price: { target: 9 } },
      draft: {},
      mode: 'per-token',
      isEditing: false,
      sourceName: '',
      targetName: 'target',
      loadedPricingName: '',
    })

    expect(result.price).toEqual({ target: 9 })
  })

  test('removes pricing when the loaded model is saved with empty fields', () => {
    const result = reconcileModelPricingMaps({
      maps: { ...emptyPricingMaps(), price: { current: 9 } },
      draft: {},
      mode: 'per-request',
      isEditing: true,
      sourceName: 'current',
      targetName: 'current',
      loadedPricingName: 'current',
    })

    expect(result.price).toEqual({})
  })

  test('preserves pricing when a draft contains an invalid numeric value', () => {
    const maps = { ...emptyPricingMaps(), ratio: { current: 2 } }

    const result = reconcileModelPricingMaps({
      maps,
      draft: { ratio: '2abc' },
      mode: 'per-token',
      isEditing: true,
      sourceName: 'current',
      targetName: 'renamed',
      loadedPricingName: 'current',
    })

    expect(result).toEqual(maps)
  })
})

describe('parseModelPricingNumber', () => {
  test.each([
    ['0', 0],
    ['1.', 1],
    ['.25', 0.25],
    ['1e-3', 0.001],
  ])('parses %s as a non-negative finite number', (value, expected) => {
    expect(parseModelPricingNumber(value)).toBe(expected)
  })

  test.each(['', '-1', '1abc', 'Infinity', '0x10', '  '])(
    'rejects %s',
    (value) => {
      expect(parseModelPricingNumber(value)).toBeUndefined()
    }
  )
})
