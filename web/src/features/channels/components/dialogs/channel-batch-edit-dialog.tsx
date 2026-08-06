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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Pencil, RefreshCw } from 'lucide-react'
import {
  type ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { type FieldErrors, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ClientMultiSelect } from '@/components/client-multi-select'
import { Dialog } from '@/components/dialog'
import { MultiSelect } from '@/components/multi-select'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { CLIENT_POLICY_MODE_LABEL_KEYS } from '@/lib/client-policy'

import {
  batchUpdateChannels,
  getAllModels,
  getGroups,
  previewChannelBatch,
} from '../../api'
import {
  CHANNEL_BATCH_EDIT_DEFAULT_VALUES,
  buildChannelBatchUpdates,
  channelBatchEditSchema,
  channelsQueryKeys,
  parseChannelBatchClientValues,
  parseChannelBatchListValues,
  type ChannelBatchEditValues,
} from '../../lib'
import type {
  ChannelBatchFilter,
  ChannelBatchListMode,
  ChannelBatchRateLimitMode,
  ChannelBatchTarget,
} from '../../types'
import { WeeklyScheduleEditor } from '../channel-schedule-editor'

type ChannelBatchEditDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  selectedIds: number[]
  filter: ChannelBatchFilter
  onSuccess: () => void
}

type BatchFieldProps = {
  id: string
  label: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  children: ReactNode
}

function BatchField(props: BatchFieldProps) {
  return (
    <Field orientation='horizontal' className='items-start py-3'>
      <Checkbox
        id={props.id}
        checked={props.checked}
        onCheckedChange={props.onCheckedChange}
        className='mt-0.5'
      />
      <FieldContent className='min-w-0 gap-2'>
        <FieldLabel htmlFor={props.id}>{props.label}</FieldLabel>
        {props.checked && props.children}
      </FieldContent>
    </Field>
  )
}

type ListModeControlProps = {
  value: ChannelBatchListMode
  onChange: (value: ChannelBatchListMode) => void
}

function ListModeControl(props: ListModeControlProps) {
  const { t } = useTranslation()
  const options: Array<{ value: ChannelBatchListMode; label: string }> = [
    { value: 'replace', label: t('Replace') },
    { value: 'add', label: t('Add') },
    { value: 'remove', label: t('Remove') },
  ]

  return (
    <ToggleGroup
      value={[props.value]}
      onValueChange={(values) => {
        const next = values.find((value) => value !== props.value)
        if (next === 'replace' || next === 'add' || next === 'remove') {
          props.onChange(next)
        }
      }}
      variant='outline'
      size='sm'
      spacing={1}
      aria-label={t('List update mode')}
    >
      {options.map((option) => (
        <ToggleGroupItem key={option.value} value={option.value}>
          {option.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}

type BatchRateLimitFieldProps = {
  id: string
  label: string
  mode: ChannelBatchRateLimitMode
  value: string
  onModeChange: (mode: ChannelBatchRateLimitMode) => void
  onValueChange: (value: string) => void
}

function BatchRateLimitField(props: BatchRateLimitFieldProps) {
  const { t } = useTranslation()

  return (
    <Field className='py-3'>
      <FieldLabel htmlFor={`${props.id}-mode`}>{props.label}</FieldLabel>
      <div className='grid gap-2 sm:grid-cols-2'>
        <Select
          value={props.mode}
          onValueChange={(value) =>
            props.onModeChange(value as ChannelBatchRateLimitMode)
          }
        >
          <SelectTrigger id={`${props.id}-mode`} className='w-full'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='keep'>{t('Keep unchanged')}</SelectItem>
            <SelectItem value='clear'>{t('Clear limit')}</SelectItem>
            <SelectItem value='custom'>{t('Custom limit')}</SelectItem>
          </SelectContent>
        </Select>
        {props.mode === 'custom' && (
          <Input
            id={`${props.id}-value`}
            type='number'
            min={1}
            max={2_147_483_647}
            step={1}
            value={props.value}
            onChange={(event) => props.onValueChange(event.target.value)}
            placeholder={t('Limit value')}
            aria-label={`${props.label} ${t('Limit value')}`}
          />
        )}
      </div>
    </Field>
  )
}

function getApiErrorMessage(error: unknown): string | undefined {
  if (error instanceof Error) return error.message
  if (!error || typeof error !== 'object') return undefined
  const response = 'response' in error ? error.response : undefined
  if (!response || typeof response !== 'object' || !('data' in response)) {
    return undefined
  }
  const data = response.data
  if (!data || typeof data !== 'object' || !('message' in data)) {
    return undefined
  }
  return typeof data.message === 'string' ? data.message : undefined
}

function getApiErrorStatus(error: unknown): number | undefined {
  if (!error || typeof error !== 'object' || !('response' in error)) {
    return undefined
  }
  const response = error.response
  if (!response || typeof response !== 'object' || !('status' in response)) {
    return undefined
  }
  return typeof response.status === 'number' ? response.status : undefined
}

function getFirstFormError(
  errors: FieldErrors<ChannelBatchEditValues>
): string | undefined {
  for (const error of Object.values(errors)) {
    if (!error) continue
    if ('message' in error && typeof error.message === 'string') {
      return error.message
    }
  }
  return undefined
}

export function ChannelBatchEditDialog(props: ChannelBatchEditDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [preview, setPreview] = useState<{
    count: number
    fingerprint: string
  } | null>(null)

  const form = useForm<ChannelBatchEditValues>({
    resolver: zodResolver(channelBatchEditSchema),
    defaultValues: CHANNEL_BATCH_EDIT_DEFAULT_VALUES,
  })

  const targetMode = form.watch('targetMode')
  const applyGroup = form.watch('applyGroup')
  const applyPriority = form.watch('applyPriority')
  const applyWeight = form.watch('applyWeight')
  const rpmLimitMode = form.watch('rpmLimitMode')
  const rpmLimitValue = form.watch('rpmLimitValue')
  const concurrencyLimitMode = form.watch('concurrencyLimitMode')
  const concurrencyLimitValue = form.watch('concurrencyLimitValue')
  const applyTag = form.watch('applyTag')
  const applyModels = form.watch('applyModels')
  const applyModelMapping = form.watch('applyModelMapping')
  const applyAutoBan = form.watch('applyAutoBan')
  const applyTestModel = form.watch('applyTestModel')
  const applyRemark = form.watch('applyRemark')
  const applyStartsAt = form.watch('applyStartsAt')
  const applyExpiresAt = form.watch('applyExpiresAt')
  const applyPausedUntil = form.watch('applyPausedUntil')
  const applyWeeklySchedule = form.watch('applyWeeklySchedule')
  const applyClientPolicy = form.watch('applyClientPolicy')
  const clientPolicyMode = form.watch('clientPolicyMode')
  const clientPolicyClients = form.watch('clientPolicyClients')
  const applyUpstreamModelUpdateCheckEnabled = form.watch(
    'applyUpstreamModelUpdateCheckEnabled'
  )
  const upstreamModelUpdateCheckEnabled = form.watch(
    'upstreamModelUpdateCheckEnabled'
  )
  const applyUpstreamModelUpdateAutoSyncEnabled = form.watch(
    'applyUpstreamModelUpdateAutoSyncEnabled'
  )
  const upstreamModelUpdateAutoSyncEnabled = form.watch(
    'upstreamModelUpdateAutoSyncEnabled'
  )
  const applyUpstreamModelUpdateIgnoredModels = form.watch(
    'applyUpstreamModelUpdateIgnoredModels'
  )
  const weeklyEnabled = form.watch('weeklyEnabled')
  const weeklyWindows = form.watch('weeklyWindows')
  const autoBan = form.watch('autoBan')
  const groupValues = form.watch('groupValues')
  const modelValues = form.watch('modelValues')

  const groupQuery = useQuery({
    queryKey: ['groups'],
    queryFn: getGroups,
    enabled: props.open && applyGroup,
  })
  const modelQuery = useQuery({
    queryKey: ['channel_models'],
    queryFn: getAllModels,
    enabled: props.open && applyModels,
  })
  const groupOptions = useMemo(
    () =>
      (groupQuery.data?.data ?? []).map((group) => ({
        value: group,
        label: group,
      })),
    [groupQuery.data]
  )
  const modelOptions = useMemo(
    () =>
      (modelQuery.data?.data ?? []).map((model) => ({
        value: model.id,
        label: model.id,
      })),
    [modelQuery.data]
  )
  const selectedGroupValues = useMemo(
    () => parseChannelBatchListValues(groupValues),
    [groupValues]
  )
  const selectedModelValues = useMemo(
    () => parseChannelBatchListValues(modelValues),
    [modelValues]
  )

  const previewMutation = useMutation({
    mutationFn: () => previewChannelBatch(props.filter),
  })
  const updateMutation = useMutation({
    mutationFn: (request: {
      target: ChannelBatchTarget
      updates: ReturnType<typeof buildChannelBatchUpdates>
    }) => batchUpdateChannels(request.target, request.updates),
  })

  const previewMutationRef = useRef(previewMutation)
  const translateRef = useRef(t)
  previewMutationRef.current = previewMutation
  translateRef.current = t

  const loadPreview = useCallback(async (): Promise<void> => {
    try {
      const response = await previewMutationRef.current.mutateAsync()
      if (!response.success || !response.data) {
        throw new Error(
          response.message || translateRef.current('Failed to preview channels')
        )
      }
      setPreview(response.data)
    } catch (error) {
      setPreview(null)
      toast.error(
        getApiErrorMessage(error) ||
          translateRef.current('Failed to preview channels')
      )
    }
  }, [])

  const initialTargetMode =
    props.selectedIds.length > 0 ? 'selected' : 'filtered'

  useEffect(() => {
    if (!props.open) return
    form.reset({
      ...CHANNEL_BATCH_EDIT_DEFAULT_VALUES,
      targetMode: initialTargetMode,
    })
    setPreview(null)
    if (initialTargetMode === 'filtered') void loadPreview()
  }, [form, initialTargetMode, loadPreview, props.open])

  const handleTargetModeChange = (values: string[]): void => {
    const next = values.find((value) => value !== targetMode)
    if (next !== 'selected' && next !== 'filtered') return
    form.setValue('targetMode', next, { shouldValidate: true })
    setPreview(null)
    if (next === 'filtered') void loadPreview()
  }

  const handleClose = (): void => {
    if (updateMutation.isPending) return
    props.onOpenChange(false)
  }

  const handleSubmit = async (
    values: ChannelBatchEditValues
  ): Promise<void> => {
    let target: ChannelBatchTarget
    if (values.targetMode === 'selected') {
      target = { mode: 'selected', ids: props.selectedIds }
    } else {
      if (!preview) {
        toast.error(t('Preview the filtered channels before saving'))
        return
      }
      target = {
        mode: 'filtered',
        filter: props.filter,
        fingerprint: preview.fingerprint,
      }
    }

    try {
      const response = await updateMutation.mutateAsync({
        target,
        updates: buildChannelBatchUpdates(values),
      })
      if (!response.success) {
        throw new Error(response.message || t('Failed to update channels'))
      }

      const updated = response.data?.updated ?? 0
      toast.success(
        t('{{count}} channels updated', {
          count: updated,
        })
      )
      await queryClient.invalidateQueries({ queryKey: channelsQueryKeys.all })
      props.onSuccess()
      props.onOpenChange(false)
    } catch (error) {
      if (
        getApiErrorStatus(error) === 409 &&
        values.targetMode === 'filtered'
      ) {
        setPreview(null)
        await loadPreview()
        toast.warning(t('Filtered channels changed. Review the new count.'))
        return
      }
      toast.error(getApiErrorMessage(error) || t('Failed to update channels'))
    }
  }

  const targetCount =
    targetMode === 'selected' ? props.selectedIds.length : preview?.count

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => !open && handleClose()}
      title={t('Batch Edit Channels')}
      description={t('Only checked fields will be changed.')}
      contentHeight='min(70vh, 720px)'
      contentClassName='sm:max-w-4xl'
      bodyClassName='pr-1'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={handleClose}
            disabled={updateMutation.isPending}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form='channel-batch-edit-form'
            disabled={
              updateMutation.isPending ||
              (targetMode === 'selected' && props.selectedIds.length === 0) ||
              (targetMode === 'filtered' && (!preview || preview.count === 0))
            }
          >
            {updateMutation.isPending ? (
              <Loader2
                data-icon='inline-start'
                className='animate-spin'
                aria-hidden='true'
              />
            ) : (
              <Pencil data-icon='inline-start' aria-hidden='true' />
            )}
            {t('Apply Changes')}
          </Button>
        </>
      }
    >
      <form
        id='channel-batch-edit-form'
        onSubmit={form.handleSubmit(handleSubmit, (errors) => {
          toast.error(t(getFirstFormError(errors) || 'Invalid form values'))
        })}
        className='flex flex-col gap-5'
      >
        <FieldSet>
          <FieldLegend>{t('Apply to')}</FieldLegend>
          <ToggleGroup
            value={[targetMode]}
            onValueChange={handleTargetModeChange}
            variant='outline'
            spacing={2}
            className='grid w-full grid-cols-2'
            aria-label={t('Batch edit target')}
          >
            <ToggleGroupItem
              value='selected'
              className='w-full'
              disabled={props.selectedIds.length === 0}
            >
              {t('Selected ({{count}})', { count: props.selectedIds.length })}
            </ToggleGroupItem>
            <ToggleGroupItem value='filtered' className='w-full'>
              {t('All filtered channels')}
            </ToggleGroupItem>
          </ToggleGroup>

          {targetMode === 'filtered' && (
            <Alert>
              {previewMutation.isPending ? (
                <Loader2 className='animate-spin' aria-hidden='true' />
              ) : (
                <RefreshCw aria-hidden='true' />
              )}
              <AlertTitle>{t('Filtered target preview')}</AlertTitle>
              <AlertDescription className='flex items-center justify-between gap-3'>
                <span>
                  {preview
                    ? t('{{count}} channels match the current filters', {
                        count: preview.count,
                      })
                    : t('Preview required')}
                </span>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() => void loadPreview()}
                  disabled={previewMutation.isPending}
                >
                  <RefreshCw data-icon='inline-start' aria-hidden='true' />
                  {t('Refresh Preview')}
                </Button>
              </AlertDescription>
            </Alert>
          )}

          {typeof targetCount === 'number' && targetMode === 'selected' && (
            <Badge variant='outline' className='w-fit'>
              {t('{{count}} target channels', { count: targetCount })}
            </Badge>
          )}
        </FieldSet>

        <Separator />

        <FieldSet>
          <FieldLegend>{t('Fields to update')}</FieldLegend>
          <FieldGroup className='gap-0'>
            <BatchField
              id='batch-apply-group'
              label={t('Group')}
              checked={applyGroup}
              onCheckedChange={(checked) =>
                form.setValue('applyGroup', checked, { shouldValidate: true })
              }
            >
              <div className='grid gap-2 sm:grid-cols-[auto_minmax(0,1fr)]'>
                <ListModeControl
                  value={form.watch('groupMode')}
                  onChange={(value) => form.setValue('groupMode', value)}
                />
                <div className='min-w-0'>
                  <FieldLabel htmlFor='batch-group-values' className='sr-only'>
                    {t('Groups')}
                  </FieldLabel>
                  {groupQuery.isLoading ? (
                    <Skeleton className='h-9 w-full' />
                  ) : (
                    <MultiSelect
                      id='batch-group-values'
                      options={groupOptions}
                      selected={selectedGroupValues}
                      onChange={(values) =>
                        form.setValue('groupValues', values.join(', '), {
                          shouldDirty: true,
                          shouldValidate: true,
                        })
                      }
                      placeholder={t('Select groups')}
                      maxVisibleChips={4}
                    />
                  )}
                </div>
              </div>
            </BatchField>
            <Separator />

            <div className='grid sm:grid-cols-2 sm:gap-6'>
              <BatchField
                id='batch-apply-priority'
                label={t('Priority')}
                checked={applyPriority}
                onCheckedChange={(checked) =>
                  form.setValue('applyPriority', checked, {
                    shouldValidate: true,
                  })
                }
              >
                <Input
                  type='number'
                  {...form.register('priority', { valueAsNumber: true })}
                />
              </BatchField>
              <BatchField
                id='batch-apply-weight'
                label={t('Weight')}
                checked={applyWeight}
                onCheckedChange={(checked) =>
                  form.setValue('applyWeight', checked, {
                    shouldValidate: true,
                  })
                }
              >
                <Input
                  type='number'
                  min={0}
                  {...form.register('weight', { valueAsNumber: true })}
                />
              </BatchField>
            </div>
            <Separator />

            <BatchField
              id='batch-apply-models'
              label={t('Models')}
              checked={applyModels}
              onCheckedChange={(checked) =>
                form.setValue('applyModels', checked, { shouldValidate: true })
              }
            >
              <div className='grid gap-2 sm:grid-cols-[auto_minmax(0,1fr)]'>
                <ListModeControl
                  value={form.watch('modelsMode')}
                  onChange={(value) => form.setValue('modelsMode', value)}
                />
                <div className='min-w-0'>
                  <FieldLabel htmlFor='batch-model-values' className='sr-only'>
                    {t('Models')}
                  </FieldLabel>
                  {modelQuery.isLoading ? (
                    <Skeleton className='h-9 w-full' />
                  ) : (
                    <MultiSelect
                      id='batch-model-values'
                      options={modelOptions}
                      selected={selectedModelValues}
                      onChange={(values) =>
                        form.setValue('modelValues', values.join(', '), {
                          shouldDirty: true,
                          shouldValidate: true,
                        })
                      }
                      placeholder={t('Select models or add custom ones')}
                      allowCreate
                      createLabel='Add custom model "{{value}}"'
                      maxVisibleChips={6}
                    />
                  )}
                </div>
              </div>
            </BatchField>
            <Separator />

            <BatchField
              id='batch-apply-tag'
              label={t('Tag')}
              checked={applyTag}
              onCheckedChange={(checked) =>
                form.setValue('applyTag', checked, { shouldValidate: true })
              }
            >
              <Input
                {...form.register('tag')}
                placeholder={t('Leave empty to clear')}
              />
            </BatchField>
            <Separator />

            <BatchField
              id='batch-apply-model-mapping'
              label={t('Model Mapping')}
              checked={applyModelMapping}
              onCheckedChange={(checked) =>
                form.setValue('applyModelMapping', checked, {
                  shouldValidate: true,
                })
              }
            >
              <Textarea
                {...form.register('modelMapping')}
                className='min-h-24 font-mono text-xs'
                placeholder={t('JSON object; leave empty to clear')}
              />
            </BatchField>
            <Separator />

            <BatchField
              id='batch-apply-auto-ban'
              label={t('Auto Disable')}
              checked={applyAutoBan}
              onCheckedChange={(checked) =>
                form.setValue('applyAutoBan', checked, {
                  shouldValidate: true,
                })
              }
            >
              <Field orientation='horizontal'>
                <FieldContent>
                  <FieldTitle>{t('Enable automatic disabling')}</FieldTitle>
                </FieldContent>
                <Switch
                  checked={autoBan === '1'}
                  onCheckedChange={(checked) =>
                    form.setValue('autoBan', checked ? '1' : '0')
                  }
                  aria-label={t('Enable automatic disabling')}
                />
              </Field>
            </BatchField>
            <Separator />

            <div className='grid sm:grid-cols-2 sm:gap-6'>
              <BatchField
                id='batch-apply-test-model'
                label={t('Test Model')}
                checked={applyTestModel}
                onCheckedChange={(checked) =>
                  form.setValue('applyTestModel', checked, {
                    shouldValidate: true,
                  })
                }
              >
                <Input
                  {...form.register('testModel')}
                  placeholder={t('Leave empty to clear')}
                />
              </BatchField>
              <BatchField
                id='batch-apply-remark'
                label={t('Remark')}
                checked={applyRemark}
                onCheckedChange={(checked) =>
                  form.setValue('applyRemark', checked, {
                    shouldValidate: true,
                  })
                }
              >
                <Input
                  {...form.register('remark')}
                  placeholder={t('Leave empty to clear')}
                />
              </BatchField>
            </div>
          </FieldGroup>
        </FieldSet>

        <Separator />

        <FieldSet>
          <FieldLegend>{t('Request Limits')}</FieldLegend>
          <FieldDescription>
            {t(
              'Channel limits are shared by all users, tokens, keys, and application nodes using the channel. Capacity-full channels are skipped automatically.'
            )}
          </FieldDescription>
          <FieldGroup className='gap-0'>
            <BatchRateLimitField
              id='batch-channel-rpm-limit'
              label={t('Requests per minute')}
              mode={rpmLimitMode}
              value={rpmLimitValue}
              onModeChange={(mode) =>
                form.setValue('rpmLimitMode', mode, {
                  shouldDirty: true,
                  shouldValidate: true,
                })
              }
              onValueChange={(value) =>
                form.setValue('rpmLimitValue', value, {
                  shouldDirty: true,
                  shouldValidate: true,
                })
              }
            />
            <Separator />
            <BatchRateLimitField
              id='batch-channel-concurrency-limit'
              label={t('Concurrent requests')}
              mode={concurrencyLimitMode}
              value={concurrencyLimitValue}
              onModeChange={(mode) =>
                form.setValue('concurrencyLimitMode', mode, {
                  shouldDirty: true,
                  shouldValidate: true,
                })
              }
              onValueChange={(value) =>
                form.setValue('concurrencyLimitValue', value, {
                  shouldDirty: true,
                  shouldValidate: true,
                })
              }
            />
          </FieldGroup>
        </FieldSet>

        <Separator />

        <FieldSet>
          <FieldLegend>{t('Client access policy')}</FieldLegend>
          <FieldDescription>
            {t(
              'Filter this channel by detected client. Unknown clients are rejected by allow lists and accepted by deny lists.'
            )}
          </FieldDescription>
          <FieldGroup className='gap-0'>
            <BatchField
              id='batch-apply-client-policy'
              label={t('Client access policy')}
              checked={applyClientPolicy}
              onCheckedChange={(checked) =>
                form.setValue('applyClientPolicy', checked, {
                  shouldValidate: true,
                })
              }
            >
              <div className='grid gap-3 sm:grid-cols-2'>
                <Field>
                  <FieldLabel>{t('Policy mode')}</FieldLabel>
                  <Select
                    value={clientPolicyMode}
                    onValueChange={(value) => {
                      if (
                        value === 'unrestricted' ||
                        value === 'allow' ||
                        value === 'deny'
                      ) {
                        form.setValue('clientPolicyMode', value, {
                          shouldValidate: true,
                        })
                      }
                    }}
                  >
                    <SelectTrigger>
                      <SelectValue>
                        {t(CLIENT_POLICY_MODE_LABEL_KEYS[clientPolicyMode])}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value='unrestricted'>
                          {t('Unrestricted')}
                        </SelectItem>
                        <SelectItem value='allow'>{t('Allow only')}</SelectItem>
                        <SelectItem value='deny'>{t('Deny')}</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
                <Field>
                  <FieldLabel>{t('Client names')}</FieldLabel>
                  <ClientMultiSelect
                    selected={parseChannelBatchClientValues(
                      clientPolicyClients
                    )}
                    onChange={(clients) =>
                      form.setValue('clientPolicyClients', clients.join(', '), {
                        shouldValidate: true,
                      })
                    }
                    disabled={clientPolicyMode === 'unrestricted'}
                  />
                </Field>
              </div>
            </BatchField>
          </FieldGroup>
        </FieldSet>

        <Separator />

        <FieldSet>
          <FieldLegend>{t('Upstream Model Detection Settings')}</FieldLegend>
          <FieldGroup className='gap-0'>
            <BatchField
              id='batch-apply-upstream-check'
              label={t('Upstream Model Update Check')}
              checked={applyUpstreamModelUpdateCheckEnabled}
              onCheckedChange={(checked) =>
                form.setValue('applyUpstreamModelUpdateCheckEnabled', checked, {
                  shouldValidate: true,
                })
              }
            >
              <Field orientation='horizontal'>
                <FieldContent>
                  <FieldTitle>
                    {t('Periodically check for upstream model changes')}
                  </FieldTitle>
                </FieldContent>
                <Switch
                  checked={upstreamModelUpdateCheckEnabled}
                  onCheckedChange={(checked) =>
                    form.setValue('upstreamModelUpdateCheckEnabled', checked)
                  }
                  aria-label={t('Upstream Model Update Check')}
                />
              </Field>
            </BatchField>
            <Separator />
            <BatchField
              id='batch-apply-upstream-auto-sync'
              label={t('Auto Sync Upstream Models')}
              checked={applyUpstreamModelUpdateAutoSyncEnabled}
              onCheckedChange={(checked) =>
                form.setValue(
                  'applyUpstreamModelUpdateAutoSyncEnabled',
                  checked,
                  { shouldValidate: true }
                )
              }
            >
              <Field orientation='horizontal'>
                <FieldContent>
                  <FieldTitle>
                    {t(
                      'Automatically sync model list when upstream changes are detected'
                    )}
                  </FieldTitle>
                </FieldContent>
                <Switch
                  checked={upstreamModelUpdateAutoSyncEnabled}
                  onCheckedChange={(checked) =>
                    form.setValue('upstreamModelUpdateAutoSyncEnabled', checked)
                  }
                  aria-label={t('Auto Sync Upstream Models')}
                />
              </Field>
            </BatchField>
            <Separator />
            <BatchField
              id='batch-apply-upstream-ignored-models'
              label={t('Ignored upstream models')}
              checked={applyUpstreamModelUpdateIgnoredModels}
              onCheckedChange={(checked) =>
                form.setValue(
                  'applyUpstreamModelUpdateIgnoredModels',
                  checked,
                  { shouldValidate: true }
                )
              }
            >
              <Input
                {...form.register('upstreamModelUpdateIgnoredModels')}
                placeholder={t(
                  'e.g., gpt-4.1-nano,regex:^claude-.*$,regex:^sora-.*$'
                )}
              />
            </BatchField>
          </FieldGroup>
        </FieldSet>

        <Separator />

        <FieldSet>
          <FieldLegend>{t('Scheduled access')}</FieldLegend>
          <FieldGroup className='gap-0'>
            <BatchField
              id='batch-apply-starts-at'
              label={t('Enable from')}
              checked={applyStartsAt}
              onCheckedChange={(checked) =>
                form.setValue('applyStartsAt', checked, {
                  shouldValidate: true,
                })
              }
            >
              <Input type='datetime-local' {...form.register('startsAt')} />
            </BatchField>
            <Separator />
            <BatchField
              id='batch-apply-expires-at'
              label={t('Disable after')}
              checked={applyExpiresAt}
              onCheckedChange={(checked) =>
                form.setValue('applyExpiresAt', checked, {
                  shouldValidate: true,
                })
              }
            >
              <Input type='datetime-local' {...form.register('expiresAt')} />
            </BatchField>
            <Separator />
            <BatchField
              id='batch-apply-paused-until'
              label={t('Pause until')}
              checked={applyPausedUntil}
              onCheckedChange={(checked) =>
                form.setValue('applyPausedUntil', checked, {
                  shouldValidate: true,
                })
              }
            >
              <Input type='datetime-local' {...form.register('pausedUntil')} />
            </BatchField>
            <Separator />
            <BatchField
              id='batch-apply-weekly-schedule'
              label={t('Weekly availability')}
              checked={applyWeeklySchedule}
              onCheckedChange={(checked) =>
                form.setValue('applyWeeklySchedule', checked, {
                  shouldValidate: true,
                })
              }
            >
              <WeeklyScheduleEditor
                enabled={weeklyEnabled}
                windows={weeklyWindows}
                onChange={(enabled, windows) => {
                  form.setValue('weeklyEnabled', enabled, {
                    shouldValidate: true,
                  })
                  form.setValue('weeklyWindows', windows, {
                    shouldValidate: true,
                  })
                }}
              />
            </BatchField>
          </FieldGroup>
        </FieldSet>
      </form>
    </Dialog>
  )
}
