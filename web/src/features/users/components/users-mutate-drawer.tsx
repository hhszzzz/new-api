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
import { Pencil } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
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
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { getPricing } from '@/features/pricing/api'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  EMPTY_PERMISSION_CATALOG,
  hasPermission,
  normalizeAdminPermissions,
} from '@/lib/admin-permissions'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import { formatQuota, parseQuotaFromDollars } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  createUser,
  updateUser,
  getUser,
  getUserPolicy,
  getGroups,
  getPermissionCatalog,
} from '../api'
import { BINDING_FIELDS, ERROR_MESSAGES, SUCCESS_MESSAGES } from '../constants'
import {
  userFormSchema,
  type UserFormValues,
  USER_FORM_DEFAULT_VALUES,
  transformFormDataToPayload,
  transformUserToFormDefaults,
} from '../lib'
import type { User } from '../types'
import { UserQuotaDialog } from './user-quota-dialog'
import { useUsers } from './users-provider'

type UsersMutateDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: User
}

const EMPTY_STRING_LIST: string[] = []

export function UsersMutateDrawer({
  open,
  onOpenChange,
  currentRow,
}: UsersMutateDrawerProps) {
  const { t } = useTranslation()
  const isUpdate = !!currentRow
  const currentRowId = currentRow?.id
  const { triggerRefresh } = useUsers()
  const currentUser = useAuthStore((s) => s.auth.user)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [quotaDialogOpen, setQuotaDialogOpen] = useState(false)

  // Fetch groups
  const { data: groupsData } = useQuery({
    queryKey: ['groups'],
    queryFn: getGroups,
    staleTime: 5 * 60 * 1000,
  })

  const { data: pricingData } = useQuery({
    queryKey: ['pricing'],
    queryFn: getPricing,
    enabled: open,
    staleTime: 5 * 60 * 1000,
  })

  // Permission catalog is owned by the backend; fetched once and reused.
  const { data: permissionCatalog = EMPTY_PERMISSION_CATALOG } = useQuery({
    queryKey: ['admin-permission-catalog'],
    queryFn: getPermissionCatalog,
    staleTime: 5 * 60 * 1000,
  })

  const form = useForm<UserFormValues>({
    resolver: zodResolver(userFormSchema),
    defaultValues: USER_FORM_DEFAULT_VALUES,
  })

  // Load the user record and policy together so the drawer never renders a
  // partially populated policy form.
  useEffect(() => {
    let active = true
    if (!open) return () => undefined

    if (!isUpdate || !currentRowId) {
      form.reset(USER_FORM_DEFAULT_VALUES)
      return () => undefined
    }

    void Promise.all([getUser(currentRowId), getUserPolicy(currentRowId)])
      .then(([userResult, policyResult]) => {
        if (!active || !userResult.success || !userResult.data) return
        form.reset(
          transformUserToFormDefaults(
            userResult.data,
            policyResult.success ? policyResult.data : undefined
          )
        )
      })
      .catch(() => {
        if (active) toast.error(t('Failed to load'))
      })

    return () => {
      active = false
    }
  }, [open, isUpdate, currentRowId, form, t])

  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'

  const currentQuotaRaw = form.watch('quota_dollars') || 0
  const selectedRole = form.watch('role')
  const selectedGroups = form.watch('groups') ?? EMPTY_STRING_LIST
  const primaryGroup = form.watch('primary_group')
  const modelLimitsEnabled = form.watch('model_limits_enabled') ?? false
  const selectedModelLimits = form.watch('model_limits') ?? EMPTY_STRING_LIST
  const modelBlocklistEnabled = form.watch('model_blocklist_enabled') ?? false
  const selectedModelBlocklist =
    form.watch('model_blocklist') ?? EMPTY_STRING_LIST
  const checkinMode = form.watch('checkin_mode') ?? 'global'
  const groupOptions = useMemo(() => {
    const values = new Set([...(groupsData?.data || []), ...selectedGroups])
    return [...values].map((group) => ({ value: group, label: group }))
  }, [groupsData?.data, selectedGroups])
  const modelOptions = useMemo(() => {
    const values = new Set([
      ...(pricingData?.data || []).map((model) => model.model_name),
      ...selectedModelLimits,
      ...selectedModelBlocklist,
    ])
    return [...values]
      .sort((left, right) => left.localeCompare(right))
      .map((model) => ({ value: model, label: model }))
  }, [pricingData?.data, selectedModelLimits, selectedModelBlocklist])
  const canEditAdminPermissions = currentUser?.role === ROLE.SUPER_ADMIN
  const targetIsAdmin = (selectedRole ?? currentRow?.role ?? 0) >= ROLE.ADMIN

  const onSubmit = async (data: UserFormValues) => {
    if (!data.groups?.length) {
      form.setError('groups', {
        type: 'manual',
        message: t('Select at least one group'),
      })
      return
    }

    if (!data.primary_group || !data.groups.includes(data.primary_group)) {
      form.setError('primary_group', {
        type: 'manual',
        message: t('Select a default group'),
      })
      return
    }

    if (!isUpdate) {
      const passwordLength = data.password?.length || 0
      if (passwordLength < 8 || passwordLength > 20) {
        form.setError('password', {
          type: 'manual',
          message: t('Password must be between 8 and 20 characters'),
        })
        return
      }
    }

    setIsSubmitting(true)
    try {
      const payload = transformFormDataToPayload(
        data,
        currentRow?.id,
        permissionCatalog
      )

      if (isUpdate && currentRow) {
        const result = await updateUser(
          payload as typeof payload & { id: number }
        )
        if (!result.success) {
          toast.error(result.message || t(ERROR_MESSAGES.UPDATE_FAILED))
          return
        }
        toast.success(t(SUCCESS_MESSAGES.USER_UPDATED))
      } else {
        const result = await createUser(payload)
        if (!result.success) {
          toast.error(result.message || t(ERROR_MESSAGES.CREATE_FAILED))
          return
        }
        toast.success(t(SUCCESS_MESSAGES.USER_CREATED))
      }
      onOpenChange(false)
      triggerRefresh()
    } catch {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setIsSubmitting(false)
    }
  }

  const refreshUserData = async () => {
    if (!currentRow) return
    const [userResult, policyResult] = await Promise.all([
      getUser(currentRow.id),
      getUserPolicy(currentRow.id),
    ])
    if (userResult.success && userResult.data) {
      form.reset(
        transformUserToFormDefaults(
          userResult.data,
          policyResult.success ? policyResult.data : undefined
        )
      )
    }
    triggerRefresh()
  }

  return (
    <>
      <Sheet
        open={open}
        onOpenChange={(v) => {
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
              {isUpdate ? t('Update') : t('Create')} {t('User')}
            </SheetTitle>
            <SheetDescription>
              {isUpdate
                ? t('Update the user by providing necessary info.')
                : t('Add a new user by providing necessary info.')}
            </SheetDescription>
          </SheetHeader>
          <Form {...form}>
            <form
              id='user-form'
              onSubmit={form.handleSubmit(onSubmit)}
              className={sideDrawerFormClassName()}
            >
              {/* Basic Information */}
              <SideDrawerSection>
                <h3 className='text-sm font-medium'>
                  {t('Basic Information')}
                </h3>

                <FormField
                  control={form.control}
                  name='username'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Username')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          placeholder={t('Enter username')}
                          disabled={isUpdate}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                {!isUpdate && (
                  <FormField
                    control={form.control}
                    name='role'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Role')}</FormLabel>
                        <Select
                          items={[
                            { value: '1', label: t('Common User') },
                            { value: '10', label: t('Admin') },
                          ]}
                          onValueChange={(value) =>
                            value !== null &&
                            field.onChange(Number.parseInt(value))
                          }
                          value={String(field.value)}
                        >
                          <FormControl>
                            <SelectTrigger>
                              <SelectValue placeholder={t('Select a role')} />
                            </SelectTrigger>
                          </FormControl>
                          <SelectContent alignItemWithTrigger={false}>
                            <SelectGroup>
                              <SelectItem value='1'>
                                {t('Common User')}
                              </SelectItem>
                              <SelectItem value='10'>{t('Admin')}</SelectItem>
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                        <FormDescription>
                          {t("Set the user's role (cannot be Root)")}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                )}

                <FormField
                  control={form.control}
                  name='display_name'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Display Name')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          placeholder={t('Enter display name')}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Leave empty to use username')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='password'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Password')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='password'
                          placeholder={
                            isUpdate
                              ? t('Leave empty to keep unchanged')
                              : t('Enter password (8-20 characters)')
                          }
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </SideDrawerSection>

              {/* Group & Quota Settings */}
              <SideDrawerSection>
                <h3 className='text-sm font-medium'>{t('Group & Quota')}</h3>

                <FormField
                  control={form.control}
                  name='groups'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('User Groups')}</FormLabel>
                      <FormControl>
                        <MultiSelect
                          id='user-groups'
                          options={groupOptions}
                          selected={field.value || []}
                          onChange={(values) => {
                            field.onChange(values)
                            if (
                              !primaryGroup ||
                              !values.includes(primaryGroup)
                            ) {
                              form.setValue('primary_group', values[0] || '', {
                                shouldDirty: true,
                              })
                            }
                          }}
                          placeholder={t('Select user groups')}
                          maxVisibleChips={5}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='primary_group'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Default Group')}</FormLabel>
                      <Select
                        items={selectedGroups.map((group) => ({
                          value: group,
                          label: group,
                        }))}
                        onValueChange={(value) =>
                          value !== null && field.onChange(value)
                        }
                        value={field.value || null}
                        disabled={selectedGroups.length === 0}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue
                              placeholder={t('Select a default group')}
                            />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            {selectedGroups.map((group) => (
                              <SelectItem key={group} value={group}>
                                {group}
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormDescription>
                        {t('Used when a token does not select a group')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                {isUpdate && (
                  <>
                    <FormField
                      control={form.control}
                      name='quota_dollars'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>
                            {t('Remaining Quota ({{currency}})', {
                              currency: currencyLabel,
                            })}
                          </FormLabel>
                          <div className='flex gap-2'>
                            <FormControl>
                              <Input
                                value={
                                  tokensOnly
                                    ? String(field.value || 0)
                                    : (field.value || 0).toFixed(6)
                                }
                                readOnly
                                className='flex-1'
                              />
                            </FormControl>
                            <Button
                              type='button'
                              variant='outline'
                              onClick={() => setQuotaDialogOpen(true)}
                            >
                              <Pencil className='mr-1 h-4 w-4' />
                              {t('Adjust Quota')}
                            </Button>
                          </div>
                          <FormDescription>
                            {formatQuota(
                              parseQuotaFromDollars(field.value || 0)
                            )}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='remark'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Remark')}</FormLabel>
                          <FormControl>
                            <Textarea
                              {...field}
                              placeholder={t(
                                'Admin notes (only visible to admins)'
                              )}
                              rows={3}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </>
                )}
              </SideDrawerSection>

              <SideDrawerSection>
                <h3 className='text-sm font-medium'>{t('Request Limits')}</h3>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'These administrator-only user overrides apply to standard text generation requests and can only tighten group limits.'
                  )}
                </p>
                <div className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
                  <FormField
                    control={form.control}
                    name='rpm_limit'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Requests per minute')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={1}
                            max={2_147_483_647}
                            value={field.value ?? ''}
                            onChange={(event) =>
                              field.onChange(
                                event.target.value === ''
                                  ? undefined
                                  : Number(event.target.value)
                              )
                            }
                            placeholder={t('No user override')}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Leave empty to use group member and existing group/global request limits'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name='concurrency_limit'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Concurrent requests')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={1}
                            max={2_147_483_647}
                            value={field.value ?? ''}
                            onChange={(event) =>
                              field.onChange(
                                event.target.value === ''
                                  ? undefined
                                  : Number(event.target.value)
                              )
                            }
                            placeholder={t('No user override')}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Leave empty to use the group member limit; the shared pool still applies'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name='stream_tps_limit'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>
                          {t('Streaming tokens per second')}
                        </FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={1}
                            max={2_147_483_647}
                            value={field.value ?? ''}
                            onChange={(event) =>
                              field.onChange(
                                event.target.value === ''
                                  ? undefined
                                  : Number(event.target.value)
                              )
                            }
                            placeholder={t('No user override')}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Leave empty to use the group member streaming limit; the shared pool still applies'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>
              </SideDrawerSection>

              <SideDrawerSection>
                <h3 className='text-sm font-medium'>{t('Model Access')}</h3>

                <FormField
                  control={form.control}
                  name='model_limits_enabled'
                  render={({ field }) => (
                    <FormItem className='flex items-center justify-between gap-4'>
                      <div className='space-y-1'>
                        <FormLabel>{t('Limit available models')}</FormLabel>
                        <FormDescription>
                          {t('Only selected models are visible and usable')}
                        </FormDescription>
                      </div>
                      <FormControl>
                        <Switch
                          checked={field.value === true}
                          onCheckedChange={field.onChange}
                          aria-label={t('Limit available models')}
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />

                {modelLimitsEnabled && (
                  <FormField
                    control={form.control}
                    name='model_limits'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Allowed Models')}</FormLabel>
                        <FormControl>
                          <MultiSelect
                            id='user-model-limits'
                            options={modelOptions}
                            selected={field.value || []}
                            onChange={field.onChange}
                            placeholder={t('Select allowed models')}
                            maxVisibleChips={5}
                          />
                        </FormControl>
                        <FormDescription>
                          {t('No selection blocks access to every model')}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                )}

                <FormField
                  control={form.control}
                  name='model_blocklist_enabled'
                  render={({ field }) => (
                    <FormItem className='flex items-center justify-between gap-4'>
                      <div className='space-y-1'>
                        <FormLabel>{t('Block selected models')}</FormLabel>
                        <FormDescription>
                          {t(
                            'Selected models are hidden and cannot be used, even when they are allowed above'
                          )}
                        </FormDescription>
                      </div>
                      <FormControl>
                        <Switch
                          checked={field.value === true}
                          onCheckedChange={field.onChange}
                          aria-label={t('Block selected models')}
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />

                {modelBlocklistEnabled && (
                  <FormField
                    control={form.control}
                    name='model_blocklist'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Blocked Models')}</FormLabel>
                        <FormControl>
                          <MultiSelect
                            id='user-model-blocklist'
                            options={modelOptions}
                            selected={field.value || []}
                            onChange={field.onChange}
                            placeholder={t('Select blocked models')}
                            maxVisibleChips={5}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                )}
              </SideDrawerSection>

              <SideDrawerSection>
                <h3 className='text-sm font-medium'>
                  {t('Check-in & Quota Cap')}
                </h3>

                <FormField
                  control={form.control}
                  name='checkin_mode'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Check-in permission')}</FormLabel>
                      <Select
                        value={field.value || 'global'}
                        onValueChange={field.onChange}
                      >
                        <FormControl>
                          <SelectTrigger className='w-full'>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          <SelectItem value='global'>
                            {t('Follow global setting')}
                          </SelectItem>
                          <SelectItem value='allow'>
                            {t('Allow check-in')}
                          </SelectItem>
                          <SelectItem value='deny'>
                            {t('Deny check-in')}
                          </SelectItem>
                        </SelectContent>
                      </Select>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                {checkinMode !== 'deny' && (
                  <div className='grid grid-cols-2 gap-2'>
                    <FormField
                      control={form.control}
                      name='checkin_min_quota'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Min check-in reward')}</FormLabel>
                          <FormControl>
                            <Input
                              type='number'
                              min={0}
                              value={field.value ?? ''}
                              onChange={(e) =>
                                field.onChange(
                                  e.target.value === ''
                                    ? undefined
                                    : Number(e.target.value)
                                )
                              }
                              placeholder={t('Leave empty to follow global')}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name='checkin_max_quota'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Max check-in reward')}</FormLabel>
                          <FormControl>
                            <Input
                              type='number'
                              min={0}
                              value={field.value ?? ''}
                              onChange={(e) =>
                                field.onChange(
                                  e.target.value === ''
                                    ? undefined
                                    : Number(e.target.value)
                                )
                              }
                              placeholder={t('Leave empty to follow global')}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </div>
                )}
                {checkinMode !== 'deny' && (
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Check-in reward range in quota units; set both to the same value for a fixed reward. Leave empty to follow the global setting.'
                    )}
                  </p>
                )}

                <FormField
                  control={form.control}
                  name='quota_cap'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Balance cap (quota units)')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          value={field.value ?? ''}
                          onChange={(e) =>
                            field.onChange(
                              e.target.value === ''
                                ? undefined
                                : Number(e.target.value)
                            )
                          }
                          placeholder={t('Leave empty for no cap')}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Gift credits (check-in, redemption codes, invite transfers) cannot push the balance above this cap. Paid top-ups are unaffected.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </SideDrawerSection>

              {canEditAdminPermissions &&
                targetIsAdmin &&
                permissionCatalog.resources.length > 0 && (
                  <SideDrawerSection>
                    <h3 className='text-sm font-medium'>
                      {t('Admin Permissions')}
                    </h3>
                    <p className='text-muted-foreground text-xs'>
                      {t(
                        'Default administrator permissions can be overridden for this user.'
                      )}
                    </p>
                    <FormField
                      control={form.control}
                      name='admin_permissions'
                      render={({ field }) => {
                        const selected = normalizeAdminPermissions(
                          field.value,
                          permissionCatalog
                        )
                        return (
                          <FormItem>
                            <div className='space-y-3'>
                              {permissionCatalog.resources.map((resource) => (
                                <div
                                  key={resource.resource}
                                  className='space-y-2 rounded-md border p-3'
                                >
                                  <div className='text-sm font-medium'>
                                    {t(resource.label_key)}
                                  </div>
                                  <div className='space-y-2'>
                                    {resource.actions.map((option) => (
                                      <label
                                        key={option.action}
                                        className='flex items-start gap-3'
                                      >
                                        <Checkbox
                                          checked={
                                            selected[resource.resource]?.[
                                              option.action
                                            ] === true
                                          }
                                          onCheckedChange={(checked) => {
                                            field.onChange({
                                              ...selected,
                                              [resource.resource]: {
                                                ...selected[resource.resource],
                                                [option.action]:
                                                  checked === true,
                                              },
                                            })
                                          }}
                                        />
                                        <span className='flex flex-col gap-1'>
                                          <span className='text-sm font-medium'>
                                            {t(option.label_key)}
                                          </span>
                                          <span className='text-muted-foreground text-xs'>
                                            {t(option.description_key)}
                                          </span>
                                        </span>
                                      </label>
                                    ))}
                                  </div>
                                </div>
                              ))}
                            </div>
                            <FormMessage />
                          </FormItem>
                        )
                      }}
                    />
                    {currentUser && (
                      <p className='text-muted-foreground text-xs'>
                        {hasPermission(
                          currentUser,
                          ADMIN_PERMISSION_RESOURCES.CHANNEL,
                          ADMIN_PERMISSION_ACTIONS.SENSITIVE_WRITE
                        )
                          ? t(
                              'Your account can edit sensitive channel settings.'
                            )
                          : t(
                              'Your account cannot edit sensitive channel settings.'
                            )}
                      </p>
                    )}
                  </SideDrawerSection>
                )}

              {/* Binding Information (Read-only) */}
              {isUpdate && (
                <SideDrawerSection>
                  <h3 className='text-sm font-medium'>
                    {t('Binding Information')}
                  </h3>
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Third-party account bindings (read-only, managed by user in profile settings)'
                    )}
                  </p>

                  <div className='flex flex-col gap-3'>
                    {BINDING_FIELDS.map(({ key, label }) => (
                      <div key={key}>
                        <Label className='text-muted-foreground text-xs'>
                          {t(label)}
                        </Label>
                        <Input
                          value={
                            (currentRow?.[key as keyof User] as string) || '-'
                          }
                          disabled
                          className='mt-1'
                        />
                      </div>
                    ))}
                  </div>
                </SideDrawerSection>
              )}
            </form>
          </Form>
          <SheetFooter className={sideDrawerFooterClassName()}>
            <SheetClose render={<Button variant='outline' />}>
              {t('Close')}
            </SheetClose>
            <Button form='user-form' type='submit' disabled={isSubmitting}>
              {isSubmitting ? t('Saving...') : t('Save changes')}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      {/* Adjust Quota Dialog */}
      {currentRow && (
        <UserQuotaDialog
          open={quotaDialogOpen}
          onOpenChange={setQuotaDialogOpen}
          userId={currentRow.id}
          currentQuota={parseQuotaFromDollars(currentQuotaRaw || 0)}
          onSuccess={refreshUserData}
        />
      )}
    </>
  )
}
