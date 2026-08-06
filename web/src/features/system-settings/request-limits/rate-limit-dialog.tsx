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
import { useEffect } from 'react'
import { type Control, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { ComboboxInput } from '@/components/ui/combobox-input'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

const MAX_RATE_LIMIT = 2_147_483_647

const createRateLimitDialogSchema = (t: (key: string) => string) => {
  const optionalLimit = z
    .number()
    .int(t('Must be an integer'))
    .min(1, t('Must be at least 1'))
    .max(MAX_RATE_LIMIT, t('Must be at most 2,147,483,647'))
    .optional()

  return z
    .object({
      groupName: z
        .string()
        .trim()
        .min(1, t('Group name is required'))
        .max(64, t('Group name must be at most 64 characters')),
      requestCountEnabled: z.boolean(),
      maxRequests: z
        .number()
        .int(t('Must be an integer'))
        .min(0, t('Must be at least 0'))
        .max(MAX_RATE_LIMIT, t('Must be at most 2,147,483,647')),
      maxSuccess: z
        .number()
        .int(t('Must be an integer'))
        .min(1, t('Must be at least 1'))
        .max(MAX_RATE_LIMIT, t('Must be at most 2,147,483,647')),
      memberRpmLimit: optionalLimit,
      memberConcurrencyLimit: optionalLimit,
      memberStreamTpsLimit: optionalLimit,
      memberFirstTokenDelayMs: optionalLimit,
      sharedRpmLimit: optionalLimit,
      sharedConcurrencyLimit: optionalLimit,
      sharedStreamTpsLimit: optionalLimit,
    })
    .superRefine((value, context) => {
      if (!value.requestCountEnabled) return
      if (!Number.isInteger(value.maxRequests)) {
        context.addIssue({
          code: 'custom',
          path: ['maxRequests'],
          message: t('Must be an integer'),
        })
      }
      if (!Number.isInteger(value.maxSuccess)) {
        context.addIssue({
          code: 'custom',
          path: ['maxSuccess'],
          message: t('Must be an integer'),
        })
      }
    })
}

export type RateLimitEntryData = {
  groupName: string
  requestCountEnabled: boolean
  maxRequests: number
  maxSuccess: number
  memberRpmLimit?: number
  memberConcurrencyLimit?: number
  memberStreamTpsLimit?: number
  memberFirstTokenDelayMs?: number
  sharedRpmLimit?: number
  sharedConcurrencyLimit?: number
  sharedStreamTpsLimit?: number
}

type RateLimitDialogFormValues = RateLimitEntryData
type LimitFieldName =
  | 'memberRpmLimit'
  | 'memberConcurrencyLimit'
  | 'memberStreamTpsLimit'
  | 'memberFirstTokenDelayMs'
  | 'sharedRpmLimit'
  | 'sharedConcurrencyLimit'
  | 'sharedStreamTpsLimit'

const RATE_LIMIT_FORM_ID = 'rate-limit-form'

type RateLimitDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (data: RateLimitEntryData) => void
  editData?: RateLimitEntryData | null
  groupOptions: Array<{ value: string; label: string }>
  groupListLoading: boolean
  groupListFailed: boolean
  editGroupMissing: boolean
}

type LimitFieldsProps = {
  control: Control<RateLimitDialogFormValues>
  title: string
  description: string
  fields: Array<{ name: LimitFieldName; label: string }>
}

function LimitFields({
  control,
  title,
  description,
  fields,
}: LimitFieldsProps) {
  const { t } = useTranslation()

  return (
    <div className='space-y-3 rounded-lg border p-4'>
      <div>
        <h4 className='text-sm font-medium'>{title}</h4>
        <p className='text-muted-foreground text-xs'>{description}</p>
      </div>
      <div className='grid gap-3 sm:grid-cols-2'>
        {fields.map(({ name, label }) => (
          <FormField
            key={name}
            control={control}
            name={name}
            render={({ field }) => (
              <FormItem>
                <FormLabel>{label}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={1}
                    max={MAX_RATE_LIMIT}
                    step={1}
                    value={field.value ?? ''}
                    placeholder={t('Unlimited')}
                    onChange={(event) =>
                      field.onChange(
                        event.target.value === ''
                          ? undefined
                          : Number(event.target.value)
                      )
                    }
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        ))}
      </div>
    </div>
  )
}

export function RateLimitDialog({
  open,
  onOpenChange,
  onSave,
  editData,
  groupOptions,
  groupListLoading,
  groupListFailed,
  editGroupMissing,
}: RateLimitDialogProps) {
  const { t } = useTranslation()
  const isEditMode = Boolean(editData)
  const schema = createRateLimitDialogSchema(t)

  const form = useForm<RateLimitDialogFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      groupName: '',
      requestCountEnabled: false,
      maxRequests: 0,
      maxSuccess: 1,
    },
  })

  useEffect(() => {
    form.reset(
      editData ?? {
        groupName: '',
        requestCountEnabled: false,
        maxRequests: 0,
        maxSuccess: 1,
      }
    )
  }, [editData, form, open])

  const requestCountEnabled = form.watch('requestCountEnabled')

  const handleSubmit = (values: RateLimitDialogFormValues) => {
    onSave({ ...values, groupName: values.groupName.trim() })
    onOpenChange(false)
  }

  let groupDescription = t('Select a group already configured in the system.')
  if (isEditMode) {
    groupDescription = editGroupMissing
      ? t(
          'This group no longer exists, but its saved policy can still be edited or deleted.'
        )
      : t('Group name cannot be changed when editing.')
  } else if (groupListFailed) {
    groupDescription = t(
      'Existing groups could not be loaded. Retry before adding.'
    )
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={
        isEditMode ? t('Edit group rate limit') : t('Add group rate limit')
      }
      description={t(
        'Configure request-count overrides, per-member limits, and the shared pool for this group.'
      )}
      contentClassName='sm:max-w-[760px]'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form={RATE_LIMIT_FORM_ID}
            disabled={
              !isEditMode &&
              (groupListLoading || groupListFailed || groupOptions.length === 0)
            }
          >
            {isEditMode ? t('Update') : t('Add')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={RATE_LIMIT_FORM_ID}
          onSubmit={form.handleSubmit(handleSubmit)}
          className='space-y-4'
        >
          <FormField
            control={form.control}
            name='groupName'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Group Name')}</FormLabel>
                <FormControl>
                  {isEditMode ? (
                    <Input {...field} disabled />
                  ) : (
                    <ComboboxInput
                      options={groupOptions}
                      value={field.value}
                      onValueChange={field.onChange}
                      placeholder={t('Select an existing group')}
                      emptyText={t('No available groups')}
                    />
                  )}
                </FormControl>
                <FormDescription>{groupDescription}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='space-y-3 rounded-lg border p-4'>
            <FormField
              control={form.control}
              name='requestCountEnabled'
              render={({ field }) => (
                <FormItem className='flex items-start justify-between gap-4'>
                  <div>
                    <FormLabel>{t('Request-count override')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Override the existing total and successful request counts for this group.'
                      )}
                    </FormDescription>
                  </div>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormItem>
              )}
            />
            {requestCountEnabled && (
              <div className='grid gap-3 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='maxRequests'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Max Requests (including failures)')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          max={MAX_RATE_LIMIT}
                          step={1}
                          {...field}
                          onChange={(event) =>
                            field.onChange(Number(event.target.value))
                          }
                        />
                      </FormControl>
                      <FormDescription>
                        {t('0 means unlimited total requests.')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='maxSuccess'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Max Successful Requests')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          max={MAX_RATE_LIMIT}
                          step={1}
                          {...field}
                          onChange={(event) =>
                            field.onChange(Number(event.target.value))
                          }
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Only successful requests count toward this limit.')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
            )}
          </div>

          <LimitFields
            control={form.control}
            title={t('Group member limits')}
            description={t(
              'Each user in the group receives an independent limit. Empty fields are unlimited at this layer.'
            )}
            fields={[
              { name: 'memberRpmLimit', label: t('RPM') },
              { name: 'memberConcurrencyLimit', label: t('Concurrency') },
              { name: 'memberStreamTpsLimit', label: t('Streaming TPS') },
              {
                name: 'memberFirstTokenDelayMs',
                label: t('First visible text delay (ms)'),
              },
            ]}
          />

          <LimitFields
            control={form.control}
            title={t('Group shared pool')}
            description={t(
              'All users, tokens, and application nodes in this group share these limits.'
            )}
            fields={[
              { name: 'sharedRpmLimit', label: t('RPM') },
              { name: 'sharedConcurrencyLimit', label: t('Concurrency') },
              { name: 'sharedStreamTpsLimit', label: t('Streaming TPS') },
            ]}
          />
        </form>
      </Form>
    </Dialog>
  )
}
