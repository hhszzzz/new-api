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
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { updateModelPricingOptions } from '../api'

const apiPut = vi.hoisted(() => vi.fn())

vi.mock('@/lib/api', () => ({
  api: {
    put: apiPut,
  },
}))

describe('updateModelPricingOptions', () => {
  beforeEach(() => {
    apiPut.mockReset()
    apiPut.mockResolvedValue({ data: { success: true, message: '' } })
  })

  test('submits related pricing options in one request', async () => {
    const request = {
      options: [
        { key: 'ModelPrice', value: '{"model-a":1}' },
        { key: 'ModelRatio', value: '{"model-b":2}' },
      ],
    }

    await updateModelPricingOptions(request)

    expect(apiPut).toHaveBeenCalledTimes(1)
    expect(apiPut).toHaveBeenCalledWith('/api/option/model-pricing', request)
  })
})
