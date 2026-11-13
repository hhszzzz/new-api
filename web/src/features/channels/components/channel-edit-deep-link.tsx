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
import { getRouteApi } from '@tanstack/react-router'
import { useEffect, useRef } from 'react'

import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { getChannel } from '../api'
import { useChannels } from './channels-provider'

const route = getRouteApi('/_authenticated/channels/')

/**
 * Allows other pages to deep-link straight into a channel's edit drawer with
 * /channels?edit=<id>. The parameter is consumed and cleared after opening so
 * closing the drawer does not immediately reopen it.
 */
export function ChannelEditDeepLink() {
  const editChannelId = route.useSearch({ select: (search) => search.edit })
  const navigate = route.useNavigate()
  const { setCurrentRow, setOpen } = useChannels()
  const openingRef = useRef<number | null>(null)

  useEffect(() => {
    if (!editChannelId || openingRef.current === editChannelId) {
      return
    }
    openingRef.current = editChannelId
    let cancelled = false
    void (async () => {
      try {
        const channel = requireServerSuccess(
          await getChannel(editChannelId)
        ).data
        if (!cancelled && channel) {
          setCurrentRow(channel)
          setOpen('update-channel')
        }
      } catch (error) {
        handleServerError(error)
      } finally {
        openingRef.current = null
        if (!cancelled) {
          void navigate({
            search: (previous) => ({ ...previous, edit: undefined }),
            replace: true,
          })
        }
      }
    })()
    return () => {
      cancelled = true
    }
  }, [editChannelId, navigate, setCurrentRow, setOpen])

  return null
}
