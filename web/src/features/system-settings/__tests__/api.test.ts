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

import {
  getSystemUpdateInfo,
  getSystemUpdateTriggerState,
  startSystemUpdate,
  updateModelPricingOptions,
} from '../api'

const { apiGet, apiPost, apiPut } = vi.hoisted(() => ({
  apiGet: vi.fn(),
  apiPost: vi.fn(),
  apiPut: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  api: {
    get: apiGet,
    post: apiPost,
    put: apiPut,
  },
}))

describe('system settings API', () => {
  beforeEach(() => {
    apiGet.mockReset()
    apiPost.mockReset()
    apiPut.mockReset()
    apiPut.mockResolvedValue({ data: { success: true, message: '' } })
  })

  test('checks the fork GHCR workflow through the root API', async () => {
    apiGet.mockResolvedValue({
      data: {
        success: true,
        message: '',
        data: {
          current_version: 'main-deadbeef',
          latest_version: 'main-9de2eea0',
          latest_revision: '9de2eea0ab7d1708e3708bb8eadc89bafa2744b7',
          update_available: true,
          update_enabled: true,
          image: 'ghcr.io/hhszzzz/new-api:main',
          trigger: { status: 'idle' },
        },
      },
    })

    const result = await getSystemUpdateInfo()

    expect(apiGet).toHaveBeenCalledWith('/api/system-update/check')
    expect(result.latest_version).toBe('main-9de2eea0')
  })

  test('triggers the configured one-click updater through the root API', async () => {
    apiPost.mockResolvedValue({
      data: {
        success: true,
        message: '',
        data: {
          started: true,
          update: {
            current_version: 'main-deadbeef',
            latest_version: 'main-9de2eea0',
            latest_revision: '9de2eea0ab7d1708e3708bb8eadc89bafa2744b7',
            update_available: true,
            update_enabled: true,
            image: 'ghcr.io/hhszzzz/new-api:main',
            trigger: { status: 'triggering' },
          },
        },
      },
    })

    const result = await startSystemUpdate()

    expect(apiPost).toHaveBeenCalledWith('/api/system-update/apply')
    expect(result.started).toBe(true)
  })

  test('reads the in-process update trigger state without another GHCR check', async () => {
    apiGet.mockResolvedValue({
      data: {
        success: true,
        message: '',
        data: { status: 'failed', error: 'connection refused' },
      },
    })

    const result = await getSystemUpdateTriggerState()

    expect(apiGet).toHaveBeenCalledWith('/api/system-update/state', {
      skipErrorHandler: true,
    })
    expect(result?.status).toBe('failed')
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
