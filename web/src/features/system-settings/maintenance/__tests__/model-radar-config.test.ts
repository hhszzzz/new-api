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
  createModelRadarSchema,
  parseModelRadarSettings,
  serializeModelRadarSettings,
} from '../model-radar-form'

const modelRadarSchema = createModelRadarSchema(
  (alias, model) => `Alias "${alias}" conflicts with model "${model}".`
)

describe('model radar settings serialization', () => {
  test.each(['', 'invalid', 'null', '[]', '{}'])(
    'uses safe defaults for %s',
    (raw) => {
      expect(parseModelRadarSettings(raw)).toEqual({
        autoEffortEnabled: true,
        defaultVendor: 'openai',
        showDegradationAlerts: true,
        models: [],
      })
    }
  )

  test('normalizes partial overrides and retains an explicit disabled alert switch', () => {
    expect(
      parseModelRadarSettings(
        '{"show_degradation_alerts":false,"models":{"k3":{"hidden":true}}}'
      )
    ).toEqual({
      autoEffortEnabled: true,
      defaultVendor: 'openai',
      showDegradationAlerts: false,
      models: [
        {
          model: 'k3',
          displayName: '',
          vendor: '',
          hidden: true,
          autoEffort: false,
          aliases: '',
        },
      ],
    })
  })

  test('drops empty overrides and false hidden values while serializing deterministically', () => {
    const serialized = serializeModelRadarSettings({
      autoEffortEnabled: false,
      defaultVendor: 'anthropic',
      showDegradationAlerts: false,
      models: [
        {
          model: 'unused',
          displayName: '',
          vendor: '',
          hidden: false,
          autoEffort: false,
          aliases: '',
        },
        {
          model: 'k3',
          displayName: ' Kimi K3 ',
          vendor: 'moonshot',
          hidden: false,
          autoEffort: true,
          aliases: ' K3-Turbo ,k3 ',
        },
        {
          model: 'a',
          displayName: '',
          vendor: '',
          hidden: true,
          autoEffort: false,
          aliases: '',
        },
      ],
    })
    expect(serialized).toBe(
      '{"auto_effort_enabled":false,"default_vendor":"anthropic","show_degradation_alerts":false,"models":{"a":{"hidden":true},"k3":{"display_name":"Kimi K3","vendor":"moonshot","auto_effort":true,"aliases":["k3-turbo"]}}}'
    )
    expect(
      serializeModelRadarSettings(parseModelRadarSettings(serialized))
    ).toBe(serialized)
  })

  test('rejects invalid vendor names, duplicate rows, long names and more than 256 overrides', () => {
    const defaults = parseModelRadarSettings('')
    expect(modelRadarSchema.safeParse(defaults).success).toBe(true)
    expect(
      modelRadarSchema.safeParse({ ...defaults, defaultVendor: 'Bad Vendor' })
        .success
    ).toBe(false)
    const row = {
      model: 'k3',
      displayName: '',
      vendor: '',
      hidden: false,
      autoEffort: false,
      aliases: '',
    }
    for (const rows of [
      [{ ...row, vendor: 'bad_vendor' }],
      [{ ...row, vendor: 'all' }],
      [{ ...row, displayName: 'a'.repeat(129) }],
      [{ ...row, model: '' }],
      [row, row],
      Array.from({ length: 257 }, (_, index) => ({
        ...row,
        model: `model-${index}`,
        hidden: true,
      })),
    ]) {
      expect(
        modelRadarSchema.safeParse({ ...defaults, models: rows }).success
      ).toBe(false)
    }
    expect(
      modelRadarSchema.safeParse({
        ...defaults,
        models: Array.from({ length: 512 }, (_, index) => ({
          ...row,
          model: `model-${index}`,
          hidden: index < 256,
        })),
      }).success
    ).toBe(true)
  })

  test('validates alias lists against per-model limits and cross-model collisions', () => {
    const row = {
      model: 'k3',
      displayName: '',
      vendor: '',
      hidden: false,
      autoEffort: false,
      aliases: '',
    }
    const accepts = (models: (typeof row)[]) =>
      modelRadarSchema.safeParse({ ...parseModelRadarSettings(''), models })
        .success
    for (const models of [
      [
        { ...row, aliases: 'beta' },
        { ...row, model: 'beta' },
      ],
      [
        { ...row, aliases: 'shared' },
        { ...row, model: 'other', aliases: 'shared' },
      ],
      [
        {
          ...row,
          aliases: Array.from({ length: 17 }, (_, index) => `a${index}`).join(
            ','
          ),
        },
      ],
      [{ ...row, aliases: 'a'.repeat(129) }],
    ]) {
      expect(accepts(models)).toBe(false)
    }
    // A model may repeat its own name, and duplicate spellings within one list
    // collapse instead of failing validation.
    expect(accepts([{ ...row, aliases: ' K3 , k3-turbo, K3-TURBO ' }])).toBe(
      true
    )
    // Hiding a model releases its name and its aliases for other models.
    expect(
      accepts([
        { ...row, model: 'deepseek-v4.1-flash', hidden: true },
        { ...row, model: 'k3', aliases: 'deepseek-v4.1-flash' },
      ])
    ).toBe(true)
    expect(
      accepts([
        {
          ...row,
          model: 'deepseek-v4-flash-0731',
          aliases: 'deepseek-v4.1-flash',
        },
        { ...row, model: 'deepseek-v4.1-flash', aliases: '' },
      ])
    ).toBe(false)
    expect(
      accepts([
        { ...row, aliases: 'shared' },
        { ...row, model: 'other', aliases: 'shared' },
      ])
    ).toBe(false)
    expect(
      accepts([
        { ...row, hidden: true, aliases: 'shared' },
        { ...row, model: 'other', aliases: 'shared' },
      ])
    ).toBe(true)
  })

  test('names the conflicting model in the alias collision message', () => {
    const row = {
      model: 'k3',
      displayName: '',
      vendor: '',
      hidden: false,
      autoEffort: false,
      aliases: '',
    }
    const result = modelRadarSchema.safeParse({
      ...parseModelRadarSettings(''),
      models: [
        { ...row, model: 'k3', aliases: 'shared' },
        { ...row, model: 'deepseek-v4.1-flash', aliases: 'shared' },
      ],
    })
    expect(result.success).toBe(false)
    if (!result.success) {
      expect(result.error.issues.at(-1)?.message).toBe(
        'Alias "shared" conflicts with model "k3".'
      )
    }
  })
})
