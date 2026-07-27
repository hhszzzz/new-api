/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, it } from 'vitest'

import { isToolPriceRecord, isValidToolPriceEntry } from '../tool-price'

describe('tool price validation', () => {
  it('accepts zero and finite positive prices', () => {
    expect(
      isToolPriceRecord({
        web_search: 0,
        'web_search_preview:gpt-4o*': 25,
      })
    ).toBe(true)
  })

  it.each([null, [], 'prices', 1])('rejects non-object input %#', (value) => {
    expect(isToolPriceRecord(value)).toBe(false)
  })

  it.each([
    ['', 1],
    ['   ', 1],
    [' web_search', 1],
    [':gpt-4o*', 1],
    ['web_search:', 1],
    ['web_search', '10'],
    ['web_search', -1],
    ['web_search', Number.NaN],
    ['web_search', Number.POSITIVE_INFINITY],
  ])('rejects invalid entry %s=%s', (identifier, price) => {
    expect(isValidToolPriceEntry(identifier as string, price)).toBe(false)
  })
})
