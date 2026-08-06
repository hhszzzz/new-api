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

import { getUserChatPresets } from '../api'

const { apiGetMock } = vi.hoisted(() => ({
  apiGetMock: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  api: { get: apiGetMock },
}))

describe('authenticated chat presets API', () => {
  beforeEach(() => {
    apiGetMock.mockReset()
  })

  test('preserves server preset IDs when restricted entries make them sparse', async () => {
    apiGetMock.mockResolvedValue({
      data: {
        success: true,
        data: [
          { id: '0', name: 'Public', url: 'https://public.example.com' },
          { id: '2', name: 'Pro', url: 'pro-client://configure' },
          { id: '3', name: '', url: 'https://invalid.example.com' },
        ],
      },
    })

    await expect(getUserChatPresets()).resolves.toEqual([
      {
        id: '0',
        name: 'Public',
        url: 'https://public.example.com',
        type: 'web',
      },
      {
        id: '2',
        name: 'Pro',
        url: 'pro-client://configure',
        type: 'custom-protocol',
      },
    ])
    expect(apiGetMock).toHaveBeenCalledWith('/api/user/chat-presets')
  })

  test('rejects an unsuccessful preset response', async () => {
    apiGetMock.mockResolvedValue({
      data: { success: false, message: 'not authorized' },
    })

    await expect(getUserChatPresets()).rejects.toThrow('not authorized')
  })
})
