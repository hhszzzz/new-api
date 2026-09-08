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
  modelRadarSchema,
  parseModelRadarSettings,
  serializeModelRadarSettings,
} from '../model-radar-form'

describe('model radar settings serialization', () => {
  test.each(['', 'invalid', 'null', '[]', '{}'])(
    'uses safe defaults for %s',
    (raw) => {
      expect(parseModelRadarSettings(raw)).toEqual({
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
      defaultVendor: 'openai',
      showDegradationAlerts: false,
      models: [{ model: 'k3', displayName: '', vendor: '', hidden: true }],
    })
  })

  test('drops empty overrides and false hidden values while serializing deterministically', () => {
    const serialized = serializeModelRadarSettings({
      defaultVendor: 'anthropic',
      showDegradationAlerts: false,
      models: [
        { model: 'unused', displayName: '', vendor: '', hidden: false },
        {
          model: 'k3',
          displayName: ' Kimi K3 ',
          vendor: 'moonshot',
          hidden: false,
        },
        { model: 'a', displayName: '', vendor: '', hidden: true },
      ],
    })
    expect(serialized).toBe(
      '{"default_vendor":"anthropic","show_degradation_alerts":false,"models":{"a":{"hidden":true},"k3":{"display_name":"Kimi K3","vendor":"moonshot"}}}'
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
    const row = { model: 'k3', displayName: '', vendor: '', hidden: false }
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
})
