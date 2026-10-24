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
import { describe, expect, it, vi } from 'vitest'

import { getUserModels } from '../api'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/lib/api', () => ({ api: { get } }))

describe('getUserModels', () => {
  it('asks for the models the playground client may use in the group', async () => {
    get.mockResolvedValueOnce({
      data: { success: true, data: ['kimi-k3.1-pro'] },
    })

    await expect(getUserModels('稳定渠道')).resolves.toEqual([
      { label: 'kimi-k3.1-pro', value: 'kimi-k3.1-pro' },
    ])
    expect(get).toHaveBeenCalledWith('/api/user/models', {
      params: { group: '稳定渠道', client: 'unknown' },
    })
  })
})
