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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Unlink2 } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { toIntlLocale } from '@/i18n/languages'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

import { detachChannelsFromAggregates } from '../../api'
import { channelsQueryKeys } from '../../lib'
import type { Channel } from '../../types'

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  selectedChannels: Array<Pick<Channel, 'id' | 'name'>>
  onSuccess: () => void
}

export function ChannelBatchDetachDialog(props: Props) {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const currentUser = useAuthStore((state) => state.auth.user)
  const canEditSensitive = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.SENSITIVE_WRITE
  )
  const selectedChannels = useMemo(
    () =>
      [...props.selectedChannels]
        .filter((channel) => channel.id > 0)
        .sort((left, right) => left.id - right.id)
        .filter(
          (channel, index, channels) =>
            index === 0 || channel.id !== channels[index - 1]?.id
        ),
    [props.selectedChannels]
  )
  const selectedNames = useMemo(
    () =>
      new Intl.ListFormat(
        toIntlLocale(i18n.resolvedLanguage || i18n.language) || 'en',
        {
          style: 'short',
          type: 'conjunction',
        }
      ).format(selectedChannels.map((channel) => channel.name)),
    [i18n.language, i18n.resolvedLanguage, selectedChannels]
  )

  const mutation = useMutation({
    mutationFn: async () => {
      if (!canEditSensitive) {
        throw new Error(
          t('You do not have permission to edit sensitive channel settings.')
        )
      }
      const response = await detachChannelsFromAggregates({
        ids: selectedChannels.map((channel) => channel.id),
      })
      if (!response.success || !response.data) {
        throw new Error(response.message || t('Operation failed'))
      }
      return response.data
    },
    onSuccess: async (data) => {
      toast.success(
        t('Detached {{count}} channels from aggregates', {
          count: data.updated,
        })
      )
      await queryClient.invalidateQueries({ queryKey: channelsQueryKeys.all })
      props.onSuccess()
      props.onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Operation failed'))
    },
  })

  const handleOpenChange = (open: boolean) => {
    if (!open && mutation.isPending) return
    props.onOpenChange(open)
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={handleOpenChange}
      title={t('Detach selected channels')}
      description={t('Selected {{names}}, {{count}} channels total', {
        names: selectedNames,
        count: selectedChannels.length,
      })}
      contentHeight='auto'
      footer={
        <>
          <Button
            variant='outline'
            onClick={() => handleOpenChange(false)}
            disabled={mutation.isPending}
          >
            {t('Cancel')}
          </Button>
          <Button
            onClick={() => mutation.mutate()}
            disabled={
              !canEditSensitive ||
              selectedChannels.length === 0 ||
              mutation.isPending
            }
          >
            {mutation.isPending ? (
              <Spinner data-icon='inline-start' />
            ) : (
              <Unlink2 data-icon='inline-start' aria-hidden='true' />
            )}
            {t('Detach')}
          </Button>
        </>
      }
    >
      {null}
    </Dialog>
  )
}
