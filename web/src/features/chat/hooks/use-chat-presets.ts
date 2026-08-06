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
import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'

import type { SystemStatus } from '@/features/auth/types'
import { useStatus } from '@/hooks/use-status'
import { useAuthStore } from '@/stores/auth-store'

import { getUserChatPresets } from '../api'
import type { ChatPreset } from '../lib/chat-links'

function extractServerAddress(status: SystemStatus | null) {
  const fromStatus =
    (status?.server_address as string | undefined) ??
    (status?.serverAddress as string | undefined) ??
    status?.data?.server_address ??
    (status?.data as Record<string, unknown> | undefined)?.serverAddress

  if (fromStatus && typeof fromStatus === 'string') {
    return fromStatus
  }

  if (typeof window !== 'undefined') {
    return window.location.origin
  }

  return ''
}

export function useChatPresets(): {
  chatPresets: ChatPreset[]
  serverAddress: string
  isLoading: boolean
  error: Error | null
} {
  const { status } = useStatus()
  const user = useAuthStore((state) => state.auth.user)
  const userGroupKey = user?.groups?.length ? user.groups : (user?.group ?? '')
  const chatPresetsQuery = useQuery({
    queryKey: ['chat-presets', user?.id, userGroupKey],
    queryFn: getUserChatPresets,
    enabled: Boolean(user?.id),
    staleTime: 5 * 60 * 1000,
  })

  const serverAddress = useMemo(() => extractServerAddress(status), [status])

  return {
    chatPresets: chatPresetsQuery.data ?? [],
    serverAddress,
    isLoading: chatPresetsQuery.isLoading,
    error: chatPresetsQuery.error,
  }
}
