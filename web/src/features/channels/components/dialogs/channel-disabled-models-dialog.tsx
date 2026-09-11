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
import { Loader2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { updateChannelModelStatus } from '../../api'
import { parseGroupsList, parseModelsList } from '../../lib/channel-utils'
import {
  disabledGroupsForModel,
  parseDisabledModels,
} from '../../lib/disabled-models'
import type { Channel } from '../../types'

type ChannelDisabledModelsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channel: Channel | null
  onChanged?: () => void | Promise<void>
}

export function ChannelDisabledModelsDialog({
  open,
  onOpenChange,
  channel,
  onChanged,
}: ChannelDisabledModelsDialogProps) {
  const { t } = useTranslation()
  const groups = useMemo(
    () => (channel ? parseGroupsList(channel.group) : []),
    [channel]
  )
  const [group, setGroup] = useState('')
  const [pendingModel, setPendingModel] = useState<string | null>(null)
  const activeGroup = group || groups[0] || ''
  const models = useMemo(
    () => (channel ? parseModelsList(channel.models) : []),
    [channel]
  )
  const entries = useMemo(
    () => (channel ? parseDisabledModels(channel.other_info) : []),
    [channel]
  )

  const handleToggle = async (model: string, disabled: boolean) => {
    if (!channel) return
    setPendingModel(model)
    try {
      const result = await updateChannelModelStatus(channel.id, {
        group: activeGroup,
        model,
        disabled,
      })
      if (result.success === false) {
        toast.error(result.message || t('Operation failed'))
        return
      }
      toast.success(
        disabled
          ? t('Model disabled: {{model}}', { model })
          : t('Model enabled: {{model}}', { model })
      )
      await onChanged?.()
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('Operation failed')
      )
    } finally {
      setPendingModel(null)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Disabled models')}
      description={t(
        'Models disabled on this channel by automatic or manual actions. Re-enable them here to restore routing.'
      )}
      contentClassName='sm:max-w-xl'
      footer={
        <Button variant='outline' onClick={() => onOpenChange(false)}>
          {t('Close')}
        </Button>
      }
    >
      {groups.length > 1 && (
        <div className='mb-3 flex items-center gap-2'>
          <span className='text-muted-foreground text-xs'>{t('Group')}</span>
          <Select
            items={groups.map((item) => ({ value: item, label: item }))}
            value={activeGroup}
            onValueChange={(value) => value !== null && setGroup(value)}
          >
            <SelectTrigger aria-label={t('Group')} className='w-48' size='sm'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {groups.map((item) => (
                  <SelectItem key={item} value={item}>
                    {item}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>
      )}

      <div className='space-y-1'>
        {models.map((model) => {
          const disabledGroups = disabledGroupsForModel(entries, model, groups)
          const disabledHere =
            activeGroup === '' || disabledGroups.includes(activeGroup)
          const entry = disabledHere
            ? entries.find((item) => {
                if (item.model.trim() !== model.trim()) return false
                const entryGroup = (item.group || '').trim()
                return entryGroup === activeGroup || entryGroup === ''
              })
            : undefined
          return (
            <div
              key={model}
              role='group'
              aria-label={model}
              className='flex items-center justify-between gap-3 rounded-md border px-3 py-2'
            >
              <div className='min-w-0 space-y-0.5'>
                <div className='flex items-center gap-2'>
                  <span
                    className={
                      disabledHere
                        ? 'text-muted-foreground truncate font-mono text-sm line-through'
                        : 'truncate font-mono text-sm'
                    }
                  >
                    {model}
                  </span>
                  {entry && (
                    <Badge
                      variant={
                        entry.source === 'manual' ? 'outline' : 'secondary'
                      }
                    >
                      {entry.source === 'manual'
                        ? t('Manually disabled')
                        : t('Auto disabled')}
                    </Badge>
                  )}
                </div>
                {entry?.reason && (
                  <p className='text-muted-foreground truncate text-xs'>
                    {entry.reason}
                  </p>
                )}
              </div>
              <Button
                variant={disabledHere ? 'outline' : 'ghost'}
                size='sm'
                disabled={pendingModel === model}
                onClick={() => handleToggle(model, !disabledHere)}
              >
                {pendingModel === model && (
                  <Loader2 className='mr-1.5 h-3.5 w-3.5 animate-spin' />
                )}
                {disabledHere ? t('Enable') : t('Disable')}
              </Button>
            </div>
          )
        })}
      </div>
    </Dialog>
  )
}
