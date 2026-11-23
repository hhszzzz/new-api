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
import { useQuery } from '@tanstack/react-query'
import { type FormEvent, useEffect, useMemo, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DateTimePicker } from '@/components/datetime-picker'
import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { getAdminPlans } from '@/features/subscriptions/api'
import { getGroups } from '@/features/users/api'
import { useLatestAsyncTask } from '@/hooks/use-latest-async-task'
import {
  formatQuotaWithCurrency,
  getCurrencyDisplay,
  getCurrencyLabel,
} from '@/lib/currency'
import {
  formatQuota,
  getEditableQuotaStep,
  parseQuotaFromDollars,
} from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { addTimeToDate } from '@/lib/time'

import { createRedemption, getRedemption, updateRedemption } from '../api'
import {
  ERROR_MESSAGES,
  REDEMPTION_TYPES,
  SUCCESS_MESSAGES,
} from '../constants'
import {
  getRedemptionFormSchema,
  type RedemptionFormValues,
  REDEMPTION_FORM_DEFAULT_VALUES,
  transformFormDataToPayload,
  transformRedemptionToFormDefaults,
} from '../lib'
import type { Redemption, RedemptionFormData, RedemptionType } from '../types'
import {
  RedemptionsExportDialog,
  type RedemptionExportData,
} from './redemptions-export-dialog'
import { useRedemptions } from './redemptions-provider'

const DEFAULT_GROUP_VALUE = 'use-default-group'

function getRedemptionTypeDetails(
  data: RedemptionFormData,
  t: (key: string, options?: Record<string, unknown>) => string,
  planTitle?: string
) {
  switch (data.type) {
    case 'balance':
      return t('Balance quota {{quota}}', { quota: formatQuota(data.quota) })
    case 'subscription':
      return t('Subscription plan {{id}}', {
        id: planTitle || data.plan_id,
      })
    case 'rate_limit':
      return t('RPM +{{rpm}}, concurrency +{{concurrency}} for {{days}} days', {
        rpm: data.rpm_delta,
        concurrency: data.concurrency_delta,
        days: Math.round((data.validity_seconds ?? 0) / 86400),
      })
    case 'registration':
      return t('Registration group {{group}}', {
        group: data.target_group || t('Default'),
      })
  }
}

type RedemptionsMutateDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: Redemption
}

export function RedemptionsMutateDrawer({
  open,
  onOpenChange,
  currentRow,
}: RedemptionsMutateDrawerProps) {
  const { t } = useTranslation()
  const isUpdate = !!currentRow
  const redemptionId = currentRow?.id
  const { triggerRefresh } = useRedemptions()
  const [isSubmitting, setIsSubmitting] = useState(false)
  const submitScope = open ? (currentRow?.id ?? 'create') : null
  const { begin: beginSubmitTask, invalidate: invalidateSubmitTask } =
    useLatestAsyncTask(submitScope)
  const [createdCodes, setCreatedCodes] = useState<RedemptionExportData | null>(
    null
  )
  const loadKey = useMemo(
    () =>
      Symbol(
        `${open ? 'open' : 'closed'}:${isUpdate ? 'update' : 'create'}:${redemptionId ?? ''}`
      ),
    [open, isUpdate, redemptionId]
  )
  const [redemptionLoad, setRedemptionLoad] = useState<{
    key: symbol | null
    state: 'idle' | 'loading' | 'ready' | 'error'
    redemption: Redemption | null
  }>({ key: null, state: 'idle', redemption: null })
  let fallbackRedemptionLoadState: 'idle' | 'loading' | 'ready' = 'idle'
  if (open && isUpdate) {
    fallbackRedemptionLoadState = 'loading'
  } else if (open) {
    fallbackRedemptionLoadState = 'ready'
  }
  const activeRedemptionLoad =
    redemptionLoad.key === loadKey
      ? redemptionLoad
      : {
          key: loadKey,
          state: fallbackRedemptionLoadState,
          redemption: null,
        }
  const redemptionLoadState = activeRedemptionLoad.state
  const loadedRedemption = activeRedemptionLoad.redemption

  const form = useForm<RedemptionFormValues>({
    resolver: zodResolver(getRedemptionFormSchema(t)),
    defaultValues: REDEMPTION_FORM_DEFAULT_VALUES,
  })
  const selectedType = useWatch({ control: form.control, name: 'type' })
  const { data: plansResult } = useQuery({
    queryKey: ['admin-subscription-plans'],
    queryFn: getAdminPlans,
    enabled: open && !isUpdate && selectedType === 'subscription',
  })
  const { data: groupsResult } = useQuery({
    queryKey: ['user-groups'],
    queryFn: getGroups,
    enabled: open && !isUpdate && selectedType === 'registration',
  })

  const submitKey = `${open ? 'open' : 'closed'}:${redemptionId ?? ''}`
  const [prevSubmitKey, setPrevSubmitKey] = useState(submitKey)
  if (prevSubmitKey !== submitKey) {
    setPrevSubmitKey(submitKey)
    setIsSubmitting(false)
  }

  useEffect(() => {
    invalidateSubmitTask()
  }, [redemptionId, invalidateSubmitTask, open])

  useEffect(() => {
    form.reset(REDEMPTION_FORM_DEFAULT_VALUES)
  }, [form, loadKey])

  // Load existing data when updating
  useEffect(() => {
    if (!open || !isUpdate || redemptionId === undefined) return

    let ignoreResult = false

    void getRedemption(redemptionId)
      .then((result) => {
        if (ignoreResult) return

        if (
          !result.success ||
          !result.data ||
          result.data.id !== redemptionId
        ) {
          setRedemptionLoad({ key: loadKey, state: 'error', redemption: null })
          handleServerError(result, t('Failed to load'))
          return
        }

        form.reset(transformRedemptionToFormDefaults(result.data))
        setRedemptionLoad({
          key: loadKey,
          state: 'ready',
          redemption: result.data,
        })
      })
      .catch((error: unknown) => {
        if (ignoreResult) return

        setRedemptionLoad({ key: loadKey, state: 'error', redemption: null })
        handleServerError(error)
      })

    return () => {
      ignoreResult = true
    }
  }, [open, isUpdate, redemptionId, form, loadKey, t])

  const isUpdateReady =
    !isUpdate ||
    (redemptionLoadState === 'ready' && loadedRedemption?.id === redemptionId)
  const isLoadingRedemption = redemptionLoadState === 'loading'

  const onSubmit = async (data: RedemptionFormValues) => {
    if (isUpdate && (!currentRow || !loadedRedemption || !isUpdateReady)) {
      return
    }
    const isCurrent = beginSubmitTask()
    setIsSubmitting(true)
    try {
      const basePayload = transformFormDataToPayload(data)

      if (isUpdate && currentRow && loadedRedemption) {
        const quota = form.getFieldState('quota_dollars').isDirty
          ? basePayload.quota
          : loadedRedemption.quota
        const result = await updateRedemption({
          ...basePayload,
          quota,
          id: currentRow.id,
        })
        if (!isCurrent()) return
        if (result.success) {
          toast.success(t(SUCCESS_MESSAGES.REDEMPTION_UPDATED))
          onOpenChange(false)
          triggerRefresh()
        } else {
          handleServerError(result, t(ERROR_MESSAGES.UPDATE_FAILED))
        }
      } else {
        // Create mode
        const result = await createRedemption(basePayload)
        if (!isCurrent()) return
        if (result.success) {
          const count = result.data?.length || 0
          toast.success(
            count > 1
              ? t('Successfully created {{count}} redemption codes', {
                  count,
                })
              : t(SUCCESS_MESSAGES.REDEMPTION_CREATED)
          )
          if (result.data?.length) {
            const selectedPlanTitle = plansResult?.data?.find(
              ({ plan }) => plan.id === basePayload.plan_id
            )?.plan.title
            setCreatedCodes({
              keys: result.data,
              name: basePayload.name,
              quota:
                basePayload.type === 'balance'
                  ? formatQuotaWithCurrency(basePayload.quota, {
                      abbreviate: false,
                    })
                  : undefined,
              details: getRedemptionTypeDetails(
                basePayload,
                t,
                selectedPlanTitle
              ),
              batchId: result.batch_id,
            })
          }
          onOpenChange(false)
          triggerRefresh()
        } else {
          handleServerError(result, t(ERROR_MESSAGES.CREATE_FAILED))
        }
      }
    } catch (error) {
      if (!isCurrent()) return
      handleServerError(error, t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      if (isCurrent()) setIsSubmitting(false)
    }
  }

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    if (!isUpdate) {
      const name = form.getValues('name')
      if (!name?.trim() && form.getValues('type') === 'balance') {
        const quota = parseQuotaFromDollars(form.getValues('quota_dollars'))
        form.setValue('name', formatQuota(quota), { shouldValidate: true })
      }
    }

    void form.handleSubmit(onSubmit)(event)
  }

  const handleSetExpiry = (months: number, days: number, hours: number) => {
    const newDate = addTimeToDate(months, days, hours)
    form.setValue('expired_time', newDate ?? null)
  }

  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'
  const quotaStep = getEditableQuotaStep()
  const quotaLabel = t('Quota ({{currency}})', { currency: currencyLabel })
  const quotaPlaceholder = tokensOnly
    ? t('Enter quota in tokens')
    : t('Enter quota in {{currency}}', { currency: currencyLabel })
  let submitButtonLabel = t('Save changes')
  if (isLoadingRedemption) {
    submitButtonLabel = t('Loading...')
  } else if (isSubmitting) {
    submitButtonLabel = t('Saving...')
  }

  return (
    <>
      <Sheet
        open={open}
        onOpenChange={(v) => {
          if (!v) invalidateSubmitTask()
          onOpenChange(v)
          if (!v) {
            form.reset()
          }
        }}
      >
        <SheetContent
          className={sideDrawerContentClassName('sm:max-w-[600px]')}
        >
          <SheetHeader className={sideDrawerHeaderClassName()}>
            <SheetTitle>
              {isUpdate
                ? t('Update Redemption Code')
                : t('Create Redemption Code')}
            </SheetTitle>
            <SheetDescription>
              {isUpdate
                ? t('Update the redemption code by providing necessary info.')
                : t(
                    'Add new redemption code(s) by providing necessary info.'
                  )}{' '}
              {t('Click save when you&apos;re done.')}
            </SheetDescription>
          </SheetHeader>
          <Form {...form}>
            <form
              id='redemption-form'
              onSubmit={handleSubmit}
              className={sideDrawerFormClassName()}
              aria-busy={isLoadingRedemption}
            >
              <fieldset
                disabled={!isUpdateReady || isSubmitting}
                className='contents'
              >
                <SideDrawerSection>
                  <FormField
                    control={form.control}
                    name='name'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Name')}</FormLabel>
                        <FormControl>
                          <Input {...field} placeholder={t('Enter a name')} />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Name for this redemption batch (1-64 characters)'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  {!isUpdate && (
                    <FormField
                      control={form.control}
                      name='type'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Code type')}</FormLabel>
                          <Select
                            value={field.value}
                            onValueChange={(value) =>
                              field.onChange(value as RedemptionType)
                            }
                          >
                            <FormControl>
                              <SelectTrigger>
                                <SelectValue
                                  placeholder={t('Select a code type')}
                                />
                              </SelectTrigger>
                            </FormControl>
                            <SelectContent>
                              {REDEMPTION_TYPES.map((option) => (
                                <SelectItem
                                  key={option.value}
                                  value={option.value}
                                >
                                  {t(option.labelKey)}
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  )}

                  {selectedType === 'balance' && (
                    <FormField
                      control={form.control}
                      name='quota_dollars'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{quotaLabel}</FormLabel>
                          <FormControl>
                            <Input
                              {...field}
                              type='number'
                              step={quotaStep}
                              placeholder={quotaPlaceholder}
                              onChange={(e) =>
                                field.onChange(
                                  Number.parseFloat(e.target.value) || 0
                                )
                              }
                            />
                          </FormControl>
                          <FormDescription>
                            {tokensOnly
                              ? t('Enter the quota amount in tokens')
                              : t('Enter the quota amount in {{currency}}', {
                                  currency: currencyLabel,
                                })}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  )}

                  {selectedType === 'subscription' && !isUpdate && (
                    <FormField
                      control={form.control}
                      name='plan_id'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Subscription plan')}</FormLabel>
                          <Select
                            value={field.value > 0 ? String(field.value) : ''}
                            onValueChange={(value) =>
                              field.onChange(Number(value))
                            }
                          >
                            <FormControl>
                              <SelectTrigger>
                                <SelectValue
                                  placeholder={t('Select a subscription plan')}
                                />
                              </SelectTrigger>
                            </FormControl>
                            <SelectContent>
                              {(plansResult?.data ?? []).map(({ plan }) => (
                                <SelectItem
                                  key={plan.id}
                                  value={String(plan.id)}
                                >
                                  {plan.title}
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                          <FormDescription>
                            {t(
                              'The issued code keeps a snapshot of this plan.'
                            )}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  )}

                  {selectedType === 'rate_limit' && !isUpdate && (
                    <>
                      <div className='grid gap-4 sm:grid-cols-2'>
                        <FormField
                          control={form.control}
                          name='rpm_delta'
                          render={({ field }) => (
                            <FormItem>
                              <FormLabel>{t('RPM increase')}</FormLabel>
                              <FormControl>
                                <Input
                                  type='number'
                                  min={0}
                                  step={1}
                                  {...field}
                                  onChange={(event) =>
                                    field.onChange(
                                      Number(event.target.value) || 0
                                    )
                                  }
                                />
                              </FormControl>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                        <FormField
                          control={form.control}
                          name='concurrency_delta'
                          render={({ field }) => (
                            <FormItem>
                              <FormLabel>{t('Concurrency increase')}</FormLabel>
                              <FormControl>
                                <Input
                                  type='number'
                                  min={0}
                                  step={1}
                                  {...field}
                                  onChange={(event) =>
                                    field.onChange(
                                      Number(event.target.value) || 0
                                    )
                                  }
                                />
                              </FormControl>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                      </div>
                      <FormField
                        control={form.control}
                        name='validity_days'
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel>{t('Bonus validity in days')}</FormLabel>
                            <FormControl>
                              <Input
                                type='number'
                                min={1}
                                max={3650}
                                step={1}
                                {...field}
                                onChange={(event) =>
                                  field.onChange(
                                    Number(event.target.value) || 0
                                  )
                                }
                              />
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                    </>
                  )}

                  {selectedType === 'registration' && !isUpdate && (
                    <>
                      <FormField
                        control={form.control}
                        name='target_group'
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel>{t('Initial primary group')}</FormLabel>
                            <Select
                              value={
                                field.value
                                  ? `group:${field.value}`
                                  : DEFAULT_GROUP_VALUE
                              }
                              onValueChange={(value) =>
                                field.onChange(
                                  value == null || value === DEFAULT_GROUP_VALUE
                                    ? ''
                                    : value.slice('group:'.length)
                                )
                              }
                            >
                              <FormControl>
                                <SelectTrigger>
                                  <SelectValue
                                    placeholder={t('Use the default group')}
                                  />
                                </SelectTrigger>
                              </FormControl>
                              <SelectContent>
                                <SelectItem value={DEFAULT_GROUP_VALUE}>
                                  {t('Use the default group')}
                                </SelectItem>
                                {(groupsResult?.data ?? []).map((group) => (
                                  <SelectItem
                                    key={group}
                                    value={`group:${group}`}
                                  >
                                    {group}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <div className='grid gap-4 sm:grid-cols-2'>
                        <FormField
                          control={form.control}
                          name='initial_rpm_limit'
                          render={({ field }) => (
                            <FormItem>
                              <FormLabel>{t('Initial RPM limit')}</FormLabel>
                              <FormControl>
                                <Input
                                  type='number'
                                  min={0}
                                  step={1}
                                  {...field}
                                  value={field.value ?? ''}
                                  onChange={(event) =>
                                    field.onChange(
                                      event.target.value === ''
                                        ? null
                                        : Number(event.target.value) || 0
                                    )
                                  }
                                />
                              </FormControl>
                              <FormDescription>
                                {t(
                                  'Leave blank to use the configured default; 0 means no personal limit.'
                                )}
                              </FormDescription>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                        <FormField
                          control={form.control}
                          name='initial_concurrency_limit'
                          render={({ field }) => (
                            <FormItem>
                              <FormLabel>
                                {t('Initial concurrency limit')}
                              </FormLabel>
                              <FormControl>
                                <Input
                                  type='number'
                                  min={0}
                                  step={1}
                                  {...field}
                                  value={field.value ?? ''}
                                  onChange={(event) =>
                                    field.onChange(
                                      event.target.value === ''
                                        ? null
                                        : Number(event.target.value) || 0
                                    )
                                  }
                                />
                              </FormControl>
                              <FormDescription>
                                {t(
                                  'Leave blank to use the configured default; 0 means no personal limit.'
                                )}
                              </FormDescription>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                      </div>
                    </>
                  )}

                  <FormField
                    control={form.control}
                    name='expired_time'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Expiration Time')}</FormLabel>
                        <div className='flex flex-col gap-2'>
                          <FormControl>
                            <DateTimePicker
                              value={field.value ?? undefined}
                              onChange={(date) => field.onChange(date ?? null)}
                              placeholder={t('Never expires')}
                            />
                          </FormControl>
                          <div className='grid grid-cols-4 gap-1.5 sm:flex sm:gap-2'>
                            <Button
                              type='button'
                              variant='outline'
                              size='sm'
                              onClick={() => handleSetExpiry(0, 0, 0)}
                            >
                              {t('Never')}
                            </Button>
                            <Button
                              type='button'
                              variant='outline'
                              size='sm'
                              onClick={() => handleSetExpiry(1, 0, 0)}
                            >
                              {t('1M')}
                            </Button>
                            <Button
                              type='button'
                              variant='outline'
                              size='sm'
                              onClick={() => handleSetExpiry(0, 7, 0)}
                            >
                              {t('1W')}
                            </Button>
                            <Button
                              type='button'
                              variant='outline'
                              size='sm'
                              onClick={() => handleSetExpiry(0, 1, 0)}
                            >
                              {t('1 Day')}
                            </Button>
                          </div>
                        </div>
                        <FormDescription>
                          {t('Leave empty for never expires')}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  {!isUpdate && (
                    <FormField
                      control={form.control}
                      name='count'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Quantity')}</FormLabel>
                          <FormControl>
                            <Input
                              {...field}
                              type='number'
                              min='1'
                              max='100'
                              placeholder={t('Number of codes to create')}
                              onChange={(e) =>
                                field.onChange(
                                  Number.parseInt(e.target.value, 10) || 1
                                )
                              }
                            />
                          </FormControl>
                          <FormDescription>
                            {t(
                              'Create multiple redemption codes at once (1-100)'
                            )}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  )}
                </SideDrawerSection>
              </fieldset>
            </form>
          </Form>
          <SheetFooter className={sideDrawerFooterClassName()}>
            <SheetClose render={<Button variant='outline' />}>
              {t('Close')}
            </SheetClose>
            <Button
              form='redemption-form'
              type='submit'
              disabled={isSubmitting || !isUpdateReady}
            >
              {submitButtonLabel}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
      {createdCodes && (
        <RedemptionsExportDialog
          data={createdCodes}
          onClose={() => setCreatedCodes(null)}
        />
      )}
    </>
  )
}
