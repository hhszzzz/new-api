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
import {
  useForm,
  useWatch,
  type Resolver,
  type UseFormReturn,
} from 'react-hook-form'
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

type QuotaLimitSectionProps = {
  form: UseFormReturn<AccountPoolQuotaAdjustmentValues>
  field: 'primary' | 'secondary'
  title: string
  pending: boolean
}

const FORM_ID = 'account-pool-quota-adjustment-form'

function QuotaLimitSection(props: QuotaLimitSectionProps) {
  const { t } = useTranslation()
  const names =
    props.field === 'primary'
      ? ({
          offset: 'primary.offset_percent',
          pinned: 'primary.pinned',
          release: 'primary.release_mode',
          releaseAt: 'primary.release_at',
        } as const)
      : ({
          offset: 'secondary.offset_percent',
          pinned: 'secondary.pinned',
          release: 'secondary.release_mode',
          releaseAt: 'secondary.release_at',
        } as const)
  const pinned = useWatch({ control: props.form.control, name: names.pinned })
  const releaseMode = useWatch({
    control: props.form.control,
    name: names.release,
  })

  return (
    <section className='bg-muted/20 space-y-4 rounded-xl border p-3'>
      <h3 className='text-sm font-semibold'>{props.title}</h3>
      <FormField
        control={props.form.control}
        name={names.offset}
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
                aria-label={`${props.title} ${t('Remaining percentage offset')}`}
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
        control={props.form.control}
        name={names.pinned}
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
                aria-label={`${props.title} ${t('Pin remaining at 100%')}`}
              />
            </FormControl>
          </FormItem>
        )}
      />

      {pinned ? (
        <FormField
          control={props.form.control}
          name={names.release}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Release')}</FormLabel>
              <FormControl>
                <ToggleGroup
                  value={[field.value]}
                  onValueChange={(values) => {
                    const next = values.find((value) => value !== field.value)
                    if (next) field.onChange(next)
                  }}
                  aria-label={`${props.title} ${t('Release')}`}
                  variant='outline'
                  spacing={2}
                  className='grid w-full grid-cols-1 gap-2'
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
          control={props.form.control}
          name={names.releaseAt}
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
    </section>
  )
}

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
  const hasAdjustment = [
    props.account.quota_override?.primary,
    props.account.quota_override?.secondary,
  ].some(
    (override) =>
      override !== undefined &&
      (override.offset_percent !== 0 || override.pinned)
  )

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
      contentClassName='sm:max-w-3xl'
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
          <div className='grid gap-4 md:grid-cols-2'>
            <QuotaLimitSection
              form={form}
              field='primary'
              title={t('5-hour quota')}
              pending={props.pending}
            />
            <QuotaLimitSection
              form={form}
              field='secondary'
              title={t('Weekly / monthly quota')}
              pending={props.pending}
            />
          </div>
        </form>
      </Form>
    </Dialog>
  )
}
