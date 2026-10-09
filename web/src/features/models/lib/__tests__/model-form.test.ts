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

import type { Model } from '../../types'
import {
  modelFormSchema,
  transformFormDataToModelPayload,
  transformModelToFormDefaults,
} from '../model-form'

function makeModel(overrides: Partial<Model>): Model {
  return {
    id: 7,
    model_name: 'kimi-k3',
    status: 1,
    sync_official: 1,
    created_time: 0,
    updated_time: 0,
    name_rule: 0,
    ...overrides,
  }
}

describe('model form context_limit field', () => {
  test('hydrates the declared context limit into form defaults', () => {
    const values = transformModelToFormDefaults(
      makeModel({ context_limit: 272000 })
    )
    expect(values.context_limit).toBe(272000)

    const unset = transformModelToFormDefaults(makeModel({}))
    expect(unset.context_limit).toBeUndefined()
  })

  test('writes the context limit back into the mutation payload', () => {
    const values = transformModelToFormDefaults(
      makeModel({ context_limit: 272000 })
    )
    expect(transformFormDataToModelPayload(values).context_limit).toBe(272000)

    const cleared = transformFormDataToModelPayload({
      ...values,
      context_limit: undefined,
    })
    expect(cleared.context_limit).toBeUndefined()
  })

  test('accepts positive integers and rejects non-positive limits', () => {
    const base = transformModelToFormDefaults(makeModel({}))
    expect(
      modelFormSchema.safeParse({ ...base, context_limit: 272000 }).success
    ).toBe(true)
    expect(modelFormSchema.safeParse({ ...base }).success).toBe(true)
    expect(
      modelFormSchema.safeParse({ ...base, context_limit: 0 }).success
    ).toBe(false)
    expect(
      modelFormSchema.safeParse({ ...base, context_limit: -5 }).success
    ).toBe(false)
    expect(
      modelFormSchema.safeParse({ ...base, context_limit: 1.5 }).success
    ).toBe(false)
  })
})
