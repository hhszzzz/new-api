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

import { disabledGroupsForModel, parseDisabledModels } from '../disabled-models'

describe('parseDisabledModels', () => {
  test('returns an empty list for missing or unparsable other_info', () => {
    expect(parseDisabledModels(undefined)).toEqual([])
    expect(parseDisabledModels('')).toEqual([])
    expect(parseDisabledModels('not-json')).toEqual([])
    expect(parseDisabledModels('{"disabled_models":"nope"}')).toEqual([])
  })

  test('keeps only entries with a non-empty model name', () => {
    const entries = parseDisabledModels(
      JSON.stringify({
        disabled_models: [
          { group: 'default', model: 'gpt-4o', source: 'manual' },
          { group: 'default', model: '', source: 'manual' },
          { group: 'default', source: 'auto' },
        ],
      })
    )

    expect(entries).toEqual([
      { group: 'default', model: 'gpt-4o', source: 'manual' },
    ])
  })
})

describe('disabledGroupsForModel', () => {
  const entries = [
    { group: 'vip', model: 'gpt-4o', source: 'manual' },
    { group: 'default', model: 'gpt-4o', source: 'auto' },
    { group: 'vip', model: 'claude', source: 'manual' },
  ]

  test('returns the groups where the exact model is disabled', () => {
    expect(
      disabledGroupsForModel(entries, 'gpt-4o', ['default', 'vip'])
    ).toEqual(['vip', 'default'])
  })

  test('returns nothing for a model with no disabled entry', () => {
    expect(
      disabledGroupsForModel(entries, 'gemini', ['default', 'vip'])
    ).toEqual([])
  })

  test('expands a group-less entry to every group of the channel', () => {
    const wildcard = [{ group: '', model: 'gpt-4o', source: 'auto' }]

    expect(
      disabledGroupsForModel(wildcard, 'gpt-4o', ['default', 'vip'])
    ).toEqual(['default', 'vip'])
  })

  test('trims surrounding whitespace before matching', () => {
    const padded = [{ group: ' vip ', model: ' gpt-4o ', source: 'manual' }]

    expect(disabledGroupsForModel(padded, 'gpt-4o', ['vip'])).toEqual(['vip'])
  })
})
