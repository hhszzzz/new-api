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
import { KeyRound, RefreshCw } from 'lucide-react'
import { useEffect, useMemo } from 'react'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Form } from '@/components/ui/form'
import { Skeleton } from '@/components/ui/skeleton'
import {
  getAccountPoolSettings,
  updateAccountPoolSettings,
} from '@/features/account-pool/api'
import type { AccountPoolSettingsResponseData } from '@/features/account-pool/types'
import { getGroups } from '@/features/users/api'
import { cn } from '@/lib/utils'

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { AccountPoolSettingsFields } from './account-pool-settings-fields'
import {
  accountPoolSettingsSchema,
  type AccountPoolSettingsValues,
} from './account-pool-settings-schema'

const SETTINGS_QUERY_KEY = ['account-pool-settings'] as const

const defaultValues: AccountPoolSettingsValues = {
  enabled: false,
  hide_email_from_non_admins: true,
  provider_groups: {},
  regular_refresh_seconds: 300,
  near_reset_threshold_seconds: 600,
  near_reset_refresh_seconds: 60,
  post_reset_delay_seconds: 10,
  manual_refresh_cooldown_seconds: 60,
}

function toFormValues(
  data: AccountPoolSettingsResponseData
): AccountPoolSettingsValues {
  return {
    enabled: data.enabled,
    hide_email_from_non_admins: data.hide_email_from_non_admins ?? true,
    provider_groups: data.provider_groups ?? {},
    regular_refresh_seconds: data.regular_refresh_seconds,
    near_reset_threshold_seconds: data.near_reset_threshold_seconds,
    near_reset_refresh_seconds: data.near_reset_refresh_seconds,
    post_reset_delay_seconds: data.post_reset_delay_seconds,
    manual_refresh_cooldown_seconds: data.manual_refresh_cooldown_seconds,
  }
}

function syncStatusLabel(
  status: AccountPoolSettingsResponseData['last_sync_status']
) {
  if (status === 'success') return 'Last sync succeeded'
  if (status === 'partial') return 'Last sync was partial'
  if (status === 'failed') return 'Last sync failed'
  return 'Never synced'
}

function syncStatusClass(
  status: AccountPoolSettingsResponseData['last_sync_status']
) {
  if (status === 'success') {
    return 'border-success/30 bg-success/10 text-success'
  }
  if (status === 'partial') {
    return 'border-warning/30 bg-warning/10 text-warning'
  }
  if (status === 'failed') {
    return 'border-destructive/30 bg-destructive/10 text-destructive'
  }
  return 'text-muted-foreground'
}

export function AccountPoolSettingsSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const settingsQuery = useQuery({
    queryKey: SETTINGS_QUERY_KEY,
    queryFn: getAccountPoolSettings,
    retry: false,
  })
  const groupsQuery = useQuery({ queryKey: ['groups'], queryFn: getGroups })
  const form = useForm<AccountPoolSettingsValues>({
    resolver: zodResolver(
      accountPoolSettingsSchema
    ) as unknown as Resolver<AccountPoolSettingsValues>,
    defaultValues,
  })
  const settings = settingsQuery.data?.data
  const availableGroups = useMemo(() => {
    const configured = Object.values(settings?.provider_groups ?? {}).flat()
    const current = groupsQuery.data?.data ?? []
    return [...new Set([...current, ...configured])].filter(Boolean).sort()
  }, [groupsQuery.data?.data, settings?.provider_groups])

  useEffect(() => {
    if (settings) form.reset(toFormValues(settings))
  }, [form, settings])

  const updateMutation = useMutation({
    mutationFn: updateAccountPoolSettings,
    onSuccess: (response) => {
      queryClient.setQueryData(SETTINGS_QUERY_KEY, response)
      form.reset(toFormValues(response.data))
      toast.success(t('Account pool settings saved'))
    },
    onError: () => toast.error(t('Failed to save account pool settings')),
  })

  const onSubmit = (values: AccountPoolSettingsValues) => {
    if (updateMutation.isPending) return
    updateMutation.mutate(values)
  }

  if (settingsQuery.isLoading) {
    return (
      <SettingsSection title={t('Account Pool')}>
        <div className='grid gap-4 md:grid-cols-2'>
          <Skeleton className='h-24 w-full rounded-xl' />
          <Skeleton className='h-24 w-full rounded-xl' />
          <Skeleton className='h-72 w-full rounded-xl md:col-span-2' />
        </div>
      </SettingsSection>
    )
  }

  if (!settings) {
    return (
      <SettingsSection title={t('Account Pool')}>
        <div className='border-destructive/30 bg-destructive/5 text-destructive rounded-xl border p-4 text-sm'>
          {t('Failed to load account pool settings')}
        </div>
      </SettingsSection>
    )
  }

  return (
    <SettingsSection title={t('Account Pool')}>
      <div className='grid gap-3 md:grid-cols-2'>
        <div className='bg-muted/20 flex items-start gap-3 rounded-xl border p-3'>
          <KeyRound
            className='text-muted-foreground mt-0.5 size-4'
            aria-hidden='true'
          />
          <div className='min-w-0 space-y-1'>
            <div className='text-sm font-medium'>{t('Management key')}</div>
            <Badge
              variant='outline'
              className={cn(
                settings.management_key_configured
                  ? 'border-success/30 bg-success/10 text-success'
                  : 'border-destructive/30 bg-destructive/10 text-destructive'
              )}
            >
              {settings.management_key_configured
                ? t('Configured')
                : t('Not configured')}
            </Badge>
            <p className='text-muted-foreground text-xs'>
              {t(
                'The key is supplied through the deployment environment and is never returned by this API.'
              )}
            </p>
          </div>
        </div>

        <div className='bg-muted/20 flex items-start gap-3 rounded-xl border p-3'>
          <RefreshCw
            className='text-muted-foreground mt-0.5 size-4'
            aria-hidden='true'
          />
          <div className='min-w-0 space-y-1'>
            <div className='text-sm font-medium'>
              {t('Last synchronization')}
            </div>
            <Badge
              variant='outline'
              className={syncStatusClass(settings.last_sync_status)}
            >
              {t(syncStatusLabel(settings.last_sync_status))}
            </Badge>
            <p className='text-muted-foreground text-xs tabular-nums'>
              {settings.last_sync_at
                ? new Date(settings.last_sync_at).toLocaleString()
                : t('No synchronization has run yet.')}
            </p>
          </div>
        </div>
      </div>

      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateMutation.isPending}
            isSaveDisabled={!form.formState.isDirty}
            saveLabel='Save account pool settings'
          />
          <AccountPoolSettingsFields
            form={form}
            groups={availableGroups}
            disabled={updateMutation.isPending}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
