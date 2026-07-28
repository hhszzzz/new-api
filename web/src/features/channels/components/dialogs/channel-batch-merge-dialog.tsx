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
import { CombineIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { toIntlLocale } from '@/i18n/languages'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import { getChannelAggregates, mergeChannelsIntoAggregate } from '../../api'
import {
  buildChannelAggregateMergeParams,
  channelBatchMergeDefaultValues,
  channelBatchMergeFormSchema,
  channelsQueryKeys,
  type ChannelBatchMergeFormValues,
} from '../../lib'
import type { Channel, ChannelAggregate } from '../../types'

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  selectedChannels: Array<Pick<Channel, 'id' | 'name'>>
  onSuccess: () => void
}

const EMPTY_AGGREGATES: ChannelAggregate[] = []
const FORM_ID = 'channel-batch-merge-form'

export function ChannelBatchMergeDialog(props: Props) {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const currentUser = useAuthStore((state) => state.auth.user)
  const canEditSensitive = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.SENSITIVE_WRITE
  )
  const form = useForm<ChannelBatchMergeFormValues>({
    resolver: zodResolver(channelBatchMergeFormSchema),
    defaultValues: channelBatchMergeDefaultValues,
  })
  const aggregateQuery = useQuery({
    queryKey: channelsQueryKeys.aggregates(),
    queryFn: async () => {
      const response = await getChannelAggregates()
      if (!response.success) {
        throw new Error(response.message || t('Failed to load'))
      }
      return response
    },
    enabled: props.open,
  })
  const aggregates = aggregateQuery.data?.data ?? EMPTY_AGGREGATES
  const aggregateItems = useMemo(
    () =>
      aggregates.map((aggregate) => ({
        value: String(aggregate.id),
        label: `${aggregate.name} (${t('{{count}} child channels', {
          count: aggregate.child_count,
        })})`,
      })),
    [aggregates, t]
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
  const selectedIds = useMemo(
    () => selectedChannels.map((channel) => channel.id),
    [selectedChannels]
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
  const targetMode = form.watch('target_mode')
  const selectedAggregateId = form.watch('aggregate_id')

  const mutation = useMutation({
    mutationFn: async (values: ChannelBatchMergeFormValues) => {
      if (!canEditSensitive) {
        throw new Error(
          t('You do not have permission to edit sensitive channel settings.')
        )
      }
      const response = await mergeChannelsIntoAggregate(
        buildChannelAggregateMergeParams(values, selectedIds)
      )
      if (!response.success || !response.data) {
        throw new Error(response.message || t('Operation failed'))
      }
      return response.data
    },
    onSuccess: (data) => {
      toast.success(
        t('Merged {{count}} channels into {{name}}', {
          count: data.updated,
          name: data.aggregate.name,
        })
      )
      queryClient.invalidateQueries({ queryKey: channelsQueryKeys.lists() })
      queryClient.invalidateQueries({
        queryKey: channelsQueryKeys.aggregates(),
      })
      form.reset(channelBatchMergeDefaultValues)
      props.onSuccess()
      props.onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Operation failed'))
    },
  })

  const handleOpenChange = (open: boolean) => {
    if (!open) form.reset(channelBatchMergeDefaultValues)
    props.onOpenChange(open)
  }

  const handleTargetModeChange = (value: string) => {
    if (value !== 'existing' && value !== 'new') return
    form.setValue('target_mode', value, { shouldValidate: true })
    if (
      value === 'existing' &&
      !form.getValues('aggregate_id') &&
      aggregates[0]
    ) {
      form.setValue('aggregate_id', aggregates[0].id, {
        shouldValidate: true,
      })
    }
  }

  const mergeDisabled =
    !canEditSensitive ||
    selectedIds.length === 0 ||
    mutation.isPending ||
    (targetMode === 'existing' && !selectedAggregateId)

  return (
    <Dialog
      open={props.open}
      onOpenChange={handleOpenChange}
      title={t('Merge selected channels')}
      description={t('Selected {{names}}, {{count}} channels total', {
        names: selectedNames,
        count: selectedIds.length,
      })}
      contentClassName='sm:max-w-xl'
      bodyClassName='flex flex-col gap-5'
      footer={
        <>
          <Button variant='outline' onClick={() => handleOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button type='submit' form={FORM_ID} disabled={mergeDisabled}>
            {mutation.isPending ? (
              <Spinner data-icon='inline-start' />
            ) : (
              <HugeiconsIcon
                icon={CombineIcon}
                strokeWidth={2}
                data-icon='inline-start'
              />
            )}
            {t('Merge channels')}
          </Button>
        </>
      }
    >
      <form
        id={FORM_ID}
        className='flex flex-col gap-5'
        onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
      >
        {!canEditSensitive ? (
          <Alert variant='destructive'>
            <AlertDescription>
              {t(
                'You do not have permission to edit sensitive channel settings.'
              )}
            </AlertDescription>
          </Alert>
        ) : null}

        <FieldGroup>
          <Controller
            control={form.control}
            name='target_mode'
            render={({ field }) => (
              <FieldSet>
                <FieldLegend variant='label'>{t('Merge into')}</FieldLegend>
                <RadioGroup
                  value={field.value}
                  onValueChange={handleTargetModeChange}
                  className='grid gap-3 sm:grid-cols-2'
                >
                  <FieldLabel
                    htmlFor='merge-target-new'
                    className='cursor-pointer'
                  >
                    <Field orientation='horizontal'>
                      <RadioGroupItem
                        id='merge-target-new'
                        value='new'
                        disabled={!canEditSensitive}
                      />
                      <FieldContent>
                        <FieldTitle>{t('New aggregate')}</FieldTitle>
                        <FieldDescription>
                          {t(
                            'Create the aggregate and merge in one operation.'
                          )}
                        </FieldDescription>
                      </FieldContent>
                    </Field>
                  </FieldLabel>
                  <FieldLabel
                    htmlFor='merge-target-existing'
                    className={cn(
                      'cursor-pointer',
                      (aggregateQuery.isLoading || aggregates.length === 0) &&
                        'cursor-not-allowed opacity-60'
                    )}
                  >
                    <Field orientation='horizontal'>
                      <RadioGroupItem
                        id='merge-target-existing'
                        value='existing'
                        disabled={
                          !canEditSensitive ||
                          aggregateQuery.isLoading ||
                          aggregates.length === 0
                        }
                      />
                      <FieldContent>
                        <FieldTitle>{t('Existing aggregate')}</FieldTitle>
                        <FieldDescription>
                          {t(
                            'Move the selected channels into an existing one.'
                          )}
                        </FieldDescription>
                      </FieldContent>
                    </Field>
                  </FieldLabel>
                </RadioGroup>
              </FieldSet>
            )}
          />

          {aggregateQuery.isError ? (
            <Alert variant='destructive'>
              <AlertDescription className='flex items-center justify-between gap-3'>
                <span>{t('Failed to load channel aggregates')}</span>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={aggregateQuery.isFetching}
                  onClick={() => void aggregateQuery.refetch()}
                >
                  {t('Retry')}
                </Button>
              </AlertDescription>
            </Alert>
          ) : null}

          {targetMode === 'existing' ? (
            <Controller
              control={form.control}
              name='aggregate_id'
              render={({ field, fieldState }) => (
                <Field data-invalid={Boolean(fieldState.error)}>
                  <FieldLabel htmlFor='merge-aggregate-id'>
                    {t('Channel aggregate')}
                  </FieldLabel>
                  <Select
                    items={aggregateItems}
                    value={field.value ? String(field.value) : null}
                    onValueChange={(value) =>
                      field.onChange(value ? Number(value) : null)
                    }
                    disabled={!canEditSensitive || aggregateQuery.isLoading}
                  >
                    <SelectTrigger
                      id='merge-aggregate-id'
                      className='w-full'
                      aria-invalid={Boolean(fieldState.error)}
                    >
                      <SelectValue
                        placeholder={t('Select a channel aggregate')}
                      />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        {aggregateItems.map((item) => (
                          <SelectItem key={item.value} value={item.value}>
                            {item.label}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldError>
                    {fieldState.error?.message
                      ? t(fieldState.error.message)
                      : null}
                  </FieldError>
                </Field>
              )}
            />
          ) : (
            <FieldGroup className='gap-4'>
              <Controller
                control={form.control}
                name='name'
                render={({ field, fieldState }) => (
                  <Field data-invalid={Boolean(fieldState.error)}>
                    <FieldLabel htmlFor='merge-aggregate-name'>
                      {t('Aggregate name')}
                    </FieldLabel>
                    <Input
                      {...field}
                      id='merge-aggregate-name'
                      maxLength={191}
                      autoComplete='off'
                      disabled={!canEditSensitive}
                      aria-invalid={Boolean(fieldState.error)}
                    />
                    <FieldError>
                      {fieldState.error?.message
                        ? t(fieldState.error.message)
                        : null}
                    </FieldError>
                  </Field>
                )}
              />
              <Controller
                control={form.control}
                name='base_url'
                render={({ field, fieldState }) => (
                  <Field data-invalid={Boolean(fieldState.error)}>
                    <FieldLabel htmlFor='merge-aggregate-base-url'>
                      {t('Shared base URL (optional)')}
                    </FieldLabel>
                    <Input
                      {...field}
                      id='merge-aggregate-base-url'
                      type='url'
                      placeholder='https://api.example.com/v1'
                      autoComplete='url'
                      disabled={!canEditSensitive}
                      aria-invalid={Boolean(fieldState.error)}
                    />
                    <FieldError>
                      {fieldState.error?.message
                        ? t(fieldState.error.message)
                        : null}
                    </FieldError>
                  </Field>
                )}
              />
              <Controller
                control={form.control}
                name='remark'
                render={({ field, fieldState }) => (
                  <Field data-invalid={Boolean(fieldState.error)}>
                    <FieldLabel htmlFor='merge-aggregate-remark'>
                      {t('Remark (optional)')}
                    </FieldLabel>
                    <Textarea
                      {...field}
                      id='merge-aggregate-remark'
                      maxLength={255}
                      disabled={!canEditSensitive}
                      aria-invalid={Boolean(fieldState.error)}
                    />
                    <FieldError>
                      {fieldState.error?.message
                        ? t(fieldState.error.message)
                        : null}
                    </FieldError>
                  </Field>
                )}
              />
            </FieldGroup>
          )}

          <Controller
            control={form.control}
            name='inherit_aggregate_base_url'
            render={({ field }) => (
              <Field orientation='horizontal'>
                <FieldContent>
                  <FieldLabel htmlFor='merge-inherit-base-url'>
                    {t('Inherit aggregate base URL')}
                  </FieldLabel>
                  <FieldDescription>
                    {t(
                      'Use the aggregate URL at runtime while keeping each channel URL as a fallback.'
                    )}
                  </FieldDescription>
                </FieldContent>
                <Switch
                  id='merge-inherit-base-url'
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  disabled={!canEditSensitive}
                />
              </Field>
            )}
          />
        </FieldGroup>
      </form>
    </Dialog>
  )
}
