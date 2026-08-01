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
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import { ComboboxInput } from '@/components/ui/combobox-input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { useDebounce } from '@/hooks'

import { batchAddUserModelRoutes, getUserModelRouteCandidates } from '../../api'
import type { UserBatchSkip } from '../../types'

type UserBatchRouteDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  userIds: number[]
  onSuccess?: () => void
}

// Mirrors UserModelRouteMaxInjectPrompt in model/user_model_route.go.
const INJECT_PROMPT_MAX_LENGTH = 8000

export function UserBatchRouteDialog(props: UserBatchRouteDialogProps) {
  const { t } = useTranslation()
  const [sourceModel, setSourceModel] = useState('')
  const [targetModel, setTargetModel] = useState('')
  const [selectedExecutionGroups, setSelectedExecutionGroups] = useState<
    string[]
  >([])
  const [allGroups, setAllGroups] = useState(true)
  const [groups, setGroups] = useState<string[]>([])
  const [channelIds, setChannelIds] = useState<string[]>([])
  const [injectPrompt, setInjectPrompt] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [skipped, setSkipped] = useState<UserBatchSkip[]>([])

  // Candidate metadata (public models, groups, channels) is user-independent
  // except for the applicable-group authorization, which is validated per user
  // on the backend anyway; the first selected user's view drives the pickers.
  const anchorUserId = props.userIds[0] ?? null

  const candidatesQuery = useQuery({
    queryKey: ['user-model-route-candidates', anchorUserId],
    queryFn: () => getUserModelRouteCandidates(anchorUserId as number),
    enabled: props.open && anchorUserId !== null,
    staleTime: 60 * 1000,
  })
  const debouncedTargetModel = useDebounce(targetModel.trim(), 350)
  const channelsQuery = useQuery({
    queryKey: [
      'user-model-route-candidate-channels',
      anchorUserId,
      debouncedTargetModel,
      selectedExecutionGroups,
    ],
    queryFn: () =>
      getUserModelRouteCandidates(anchorUserId as number, {
        target_model: debouncedTargetModel,
        execution_groups: selectedExecutionGroups.join(','),
      }),
    enabled:
      props.open &&
      anchorUserId !== null &&
      debouncedTargetModel.length > 0 &&
      selectedExecutionGroups.length > 0,
    staleTime: 30 * 1000,
  })

  const candidateData =
    candidatesQuery.data?.success === true
      ? candidatesQuery.data.data
      : undefined
  const channelData =
    channelsQuery.data?.success === true ? channelsQuery.data.data : undefined

  const availableExecutionGroups = useMemo(
    () => candidateData?.execution_groups ?? [],
    [candidateData?.execution_groups]
  )
  useEffect(() => {
    if (
      selectedExecutionGroups.length === 0 &&
      availableExecutionGroups.length > 0
    ) {
      setSelectedExecutionGroups([availableExecutionGroups[0]])
    }
  }, [availableExecutionGroups, selectedExecutionGroups.length])

  const sourceModelOptions = useMemo(
    () =>
      (candidateData?.source_models ?? []).map((model) => ({
        value: model,
        label: model,
      })),
    [candidateData?.source_models]
  )
  const targetModelOptions = useMemo(
    () =>
      (candidateData?.target_models ?? []).map((model) => ({
        value: model,
        label: model,
      })),
    [candidateData?.target_models]
  )
  const groupOptions = useMemo(
    () =>
      (candidateData?.applicable_groups ?? []).map((group) => ({
        value: group,
        label: group,
      })),
    [candidateData?.applicable_groups]
  )
  const executionGroupOptions = useMemo(
    () =>
      availableExecutionGroups.map((group) => ({ value: group, label: group })),
    [availableExecutionGroups]
  )
  const channelOptions = useMemo(
    () =>
      (channelData?.channels ?? []).map((channel) => ({
        value: String(channel.id),
        label: `${channel.name || `#${channel.id}`} (#${channel.id}) · ${t(
          'Execution groups'
        )}: ${(channel.execution_groups ?? []).join(', ')}`,
      })),
    [channelData?.channels, t]
  )

  const resetState = () => {
    setSourceModel('')
    setTargetModel('')
    setSelectedExecutionGroups([])
    setAllGroups(true)
    setGroups([])
    setChannelIds([])
    setInjectPrompt('')
    setEnabled(true)
    setSkipped([])
  }

  const handleOpenChange = (open: boolean) => {
    if (!open) resetState()
    props.onOpenChange(open)
  }

  const handleSubmit = async () => {
    if (!sourceModel.trim() || !targetModel.trim()) {
      toast.error(t('Source and target models are required'))
      return
    }
    if (selectedExecutionGroups.length === 0) {
      toast.error(t('Select at least one execution group'))
      return
    }
    if (!allGroups && groups.length === 0) {
      toast.error(t('Select at least one applicable group'))
      return
    }
    if (channelIds.length === 0) {
      toast.error(t('Select at least one channel'))
      return
    }
    if (injectPrompt.length > INJECT_PROMPT_MAX_LENGTH) {
      toast.error(t('Inject prompt is too long'))
      return
    }
    setSubmitting(true)
    try {
      const result = await batchAddUserModelRoutes(props.userIds, {
        source_model: sourceModel.trim(),
        target_model: targetModel.trim(),
        pool_name: '',
        inject_prompt: injectPrompt.trim(),
        execution_group: selectedExecutionGroups[0],
        execution_groups: selectedExecutionGroups,
        all_groups: allGroups,
        groups: allGroups ? [] : groups,
        channel_ids: channelIds.map(Number).filter((id) => id > 0),
        enabled,
      })
      if (!result.success || !result.data) {
        toast.error(result.message || t('Operation failed'))
        return
      }
      const { created, skipped: skippedUsers } = result.data
      setSkipped(skippedUsers)
      if (skippedUsers.length === 0) {
        toast.success(
          t('Model route added for {{count}} users', { count: created })
        )
        handleOpenChange(false)
      } else {
        toast.warning(
          t('{{count}} users updated, {{skipped}} skipped', {
            count: created,
            skipped: skippedUsers.length,
          })
        )
      }
      props.onSuccess?.()
    } catch {
      toast.error(t('Operation failed'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={handleOpenChange}
      title={t('Batch add model route')}
      description={t(
        'Create the same model route for {{count}} selected user(s). Users with a conflicting route are skipped and reported.',
        { count: props.userIds.length }
      )}
      contentHeight='auto'
      bodyClassName='space-y-3'
      footer={
        <>
          <Button variant='outline' onClick={() => handleOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleSubmit} disabled={submitting}>
            {t('Add route')}
          </Button>
        </>
      }
    >
      <div className='space-y-3'>
        <div className='grid grid-cols-2 gap-2'>
          <div className='space-y-1'>
            <Label>{t('Source model')}</Label>
            <ComboboxInput
              value={sourceModel}
              onValueChange={setSourceModel}
              options={sourceModelOptions}
              placeholder={t('Model requested by the client')}
            />
          </div>
          <div className='space-y-1'>
            <Label>{t('Target model')}</Label>
            <ComboboxInput
              value={targetModel}
              onValueChange={setTargetModel}
              options={targetModelOptions}
              placeholder={t('Model actually executed')}
            />
          </div>
        </div>

        <div className='space-y-1'>
          <Label htmlFor='batch-route-execution-groups'>
            {t('Execution groups')}
          </Label>
          <MultiSelect
            id='batch-route-execution-groups'
            options={executionGroupOptions}
            selected={selectedExecutionGroups}
            onChange={setSelectedExecutionGroups}
            placeholder={t('Select execution groups')}
            maxVisibleChips={5}
          />
        </div>

        <div className='flex items-center justify-between gap-3'>
          <Label htmlFor='batch-route-all-groups'>
            {t('Apply to all groups')}
          </Label>
          <Switch
            id='batch-route-all-groups'
            checked={allGroups}
            onCheckedChange={setAllGroups}
          />
        </div>
        {!allGroups && (
          <MultiSelect
            id='batch-route-groups'
            options={groupOptions}
            selected={groups}
            onChange={setGroups}
            placeholder={t('Select applicable groups')}
            maxVisibleChips={5}
          />
        )}

        <div className='space-y-1'>
          <Label>{t('Channels')}</Label>
          <MultiSelect
            id='batch-route-channels'
            options={channelOptions}
            selected={channelIds}
            onChange={setChannelIds}
            placeholder={
              debouncedTargetModel && selectedExecutionGroups.length > 0
                ? t('Select at least one channel')
                : t('Pick a target model and execution groups first')
            }
            maxVisibleChips={5}
          />
        </div>

        <div className='space-y-1'>
          <Label htmlFor='batch-route-inject-prompt'>
            {t('Inject prompt (optional)')}
          </Label>
          <Textarea
            id='batch-route-inject-prompt'
            value={injectPrompt}
            onChange={(e) => setInjectPrompt(e.target.value)}
            rows={3}
            maxLength={INJECT_PROMPT_MAX_LENGTH}
          />
        </div>

        <div className='flex items-center justify-between gap-3'>
          <Label htmlFor='batch-route-enabled'>{t('Enabled')}</Label>
          <Switch
            id='batch-route-enabled'
            checked={enabled}
            onCheckedChange={setEnabled}
          />
        </div>

        {skipped.length > 0 && (
          <div className='space-y-1 rounded-md border border-amber-300 p-3 text-xs'>
            <div className='font-medium'>{t('Skipped users')}</div>
            {skipped.map((entry) => (
              <div key={entry.id}>
                #{entry.id} {entry.username || ''} — {entry.reason}
              </div>
            ))}
          </div>
        )}
      </div>
    </Dialog>
  )
}
