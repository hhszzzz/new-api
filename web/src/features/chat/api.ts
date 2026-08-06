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
import { api } from '@/lib/api'

import {
  detectChatLinkType,
  type ChatPreset,
  type ServerChatPreset,
} from './lib/chat-links'

type ChatPresetsResponse = {
  success: boolean
  message?: string
  data?: ServerChatPreset[]
}

export async function getUserChatPresets(): Promise<ChatPreset[]> {
  const response = await api.get('/api/user/chat-presets')
  const payload = response.data as ChatPresetsResponse
  if (!payload.success) {
    throw new Error(payload.message || 'Failed to load chat presets')
  }

  if (!Array.isArray(payload.data)) return []
  return payload.data.flatMap((item) => {
    if (
      !item ||
      typeof item.id !== 'string' ||
      typeof item.name !== 'string' ||
      typeof item.url !== 'string' ||
      item.name.trim() === '' ||
      item.url.trim() === ''
    ) {
      return []
    }
    const url = item.url.trim()
    return [
      {
        id: item.id,
        name: item.name.trim(),
        url,
        type: detectChatLinkType(url),
      },
    ]
  })
}
