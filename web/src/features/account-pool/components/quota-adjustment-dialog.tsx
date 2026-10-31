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
import { Trash2 } from 'lucide-react'
import { useForm, useWatch, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { DateTimePicker } from '@/components/datetime-picker'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
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
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

import {
  ACCOUNT_POOL_QUOTA_RELEASE_OPTIONS,
  accountPoolQuotaAdjustmentInput,
  accountPoolQuotaAdjustmentSchema,
  accountPoolQuotaAdjustmentValues,
  type AccountPoolQuotaAdjustmentValues,
} from '../lib/quota-adjustment-schema'
import type {
  AccountPoolAccount,
  AccountPoolQuotaOverrideInput,
} from '../types'

type QuotaAdjustmentDialogProps = {
  account: AccountPoolAccount
  pending: boolean
  onClose: () => void
  onSave: (input: AccountPoolQuotaOverrideInput) => Promise<boolean>
  onClear: () => Promise<boolean>
}

const FORM_ID = 'account-pool-quota-adjustment-form'

export function QuotaAdjustmentDialog(props: QuotaAdjustmentDialogProps) {
  const { t } = useTranslation()
  const form = useForm<AccountPoolQuotaAdjustmentValues>({
    resolver: zodResolver(
      accountPoolQuotaAdjustmentSchema
    ) as unknown as Resolver<AccountPoolQuotaAdjustmentValues>,
    defaultValues: accountPoolQuotaAdjustmentValues(
      props.account.quota_override
    ),
  })
  const pinned = useWatch({ control: form.control, name: 'pinned' })
  const releaseMode = useWatch({
    control: form.control,
    name: 'release_mode',
  })
  const hasAdjustment =
    (props.account.quota_override?.offset_percent ?? 0) !== 0 ||
    props.account.quota_override?.pinned === true

  const close = () => {
    if (!props.pending) props.onClose()
  }

  const submit = async (values: AccountPoolQuotaAdjustmentValues) => {
    const saved = await props.onSave(accountPoolQuotaAdjustmentInput(values))
    if (saved) props.onClose()
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close()
      }}
      title={t('Adjust quota')}
      description={t(
        'Adjust the displayed quota for this account. The upstream quota is not changed.'
      )}
      contentClassName='sm:max-w-xl'
      footer={
        <>
          {hasAdjustment ? (
            <Button
              type='button'
              variant='outline'
              className='text-destructive sm:mr-auto'
              disabled={props.pending}
              onClick={() => {
                void props.onClear().then((cleared) => {
                  if (cleared) props.onClose()
                })
              }}
            >
              <Trash2 className='size-4' aria-hidden='true' />
              {t('Clear adjustment')}
            </Button>
          ) : null}
          <Button
            type='button'
            variant='outline'
            disabled={props.pending}
            onClick={close}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' form={FORM_ID} disabled={props.pending}>
            {t('Save')}
          </Button>
        </>
      }
    >
      <div className='text-muted-foreground text-xs'>
        {props.account.display_name}
        {props.account.email ? ` · ${props.account.email}` : null}
      </div>
      <Form {...form}>
        <form
          id={FORM_ID}
          className='space-y-5 py-2'
          onSubmit={form.handleSubmit(submit)}
        >
          <FormField
            control={form.control}
            name='offset_percent'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Remaining percentage offset')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={-100}
                    max={100}
                    step={1}
                    disabled={props.pending}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Amount added to the displayed remaining percentage. Negative values lower it.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='pinned'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between gap-4'>
                <div className='space-y-1'>
                  <FormLabel>{t('Pin remaining at 100%')}</FormLabel>
                  <FormDescription>
                    {t('Offset is ignored while the pin is active.')}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={props.pending}
                    aria-label={t('Pin remaining at 100%')}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          {pinned ? (
            <FormField
              control={form.control}
              name='release_mode'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Release')}</FormLabel>
                  <FormControl>
                    <ToggleGroup
                      value={[field.value]}
                      onValueChange={(values) => {
                        const next = values.find(
                          (value) => value !== field.value
                        )
                        if (next) field.onChange(next)
                      }}
                      aria-label={t('Release')}
                      variant='outline'
                      spacing={2}
                      className='grid w-full grid-cols-1 gap-2 sm:grid-cols-2'
                      disabled={props.pending}
                    >
                      {ACCOUNT_POOL_QUOTA_RELEASE_OPTIONS.map((option) => (
                        <ToggleGroupItem
                          key={option.value}
                          value={option.value}
                          className='w-full'
                        >
                          {t(option.labelKey)}
                        </ToggleGroupItem>
                      ))}
                    </ToggleGroup>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          ) : null}

          {pinned && releaseMode === 'at_time' ? (
            <FormField
              control={form.control}
              name='release_at'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Release time')}</FormLabel>
                  <FormControl>
                    <DateTimePicker
                      value={field.value}
                      onChange={field.onChange}
                      placeholder={t('Select date')}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          ) : null}
        </form>
      </Form>
    </Dialog>
  )
}
