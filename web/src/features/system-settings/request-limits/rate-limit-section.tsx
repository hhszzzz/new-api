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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Code2, Palette } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
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

import { updateGroupRateLimitOptions, updateSystemOption } from '../api'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import type {
  GroupRateLimitPolicy,
  UpdateGroupRateLimitOptionsRequest,
} from '../types'
import {
  isValidGroupPoliciesJSON,
  isValidRequestCountJSON,
} from './rate-limit-validation'
import { RateLimitVisualEditor } from './rate-limit-visual-editor'

const createRateLimitSchema = (t: (key: string) => string) =>
  z.object({
    ModelRequestRateLimitEnabled: z.boolean(),
    ModelRequestRateLimitDurationMinutes: z.number().min(0),
    ModelRequestRateLimitCount: z.number().min(0).max(100000000),
    ModelRequestRateLimitSuccessCount: z.number().min(1).max(100000000),
    ModelRequestRateLimitGroup: z
      .string()
      .optional()
      .refine(isValidRequestCountJSON, {
        message: t('Invalid JSON format or values out of allowed range'),
      }),
    group_rate_limit_setting: z.object({
      member_enabled: z.boolean(),
      shared_pool_enabled: z.boolean(),
      policies: z
        .string()
        .optional()
        .refine(isValidGroupPoliciesJSON, {
          message: t('Invalid group rate-limit policy JSON'),
        }),
    }),
  })

type RateLimitFormValues = z.infer<ReturnType<typeof createRateLimitSchema>>

type RateLimitOptionValues = {
  ModelRequestRateLimitEnabled: boolean
  ModelRequestRateLimitDurationMinutes: number
  ModelRequestRateLimitCount: number
  ModelRequestRateLimitSuccessCount: number
  ModelRequestRateLimitGroup?: string
  'group_rate_limit_setting.member_enabled': boolean
  'group_rate_limit_setting.shared_pool_enabled': boolean
  'group_rate_limit_setting.policies'?: string
}

type RateLimitSectionProps = {
  defaultValues: RateLimitOptionValues
}

const GLOBAL_RATE_LIMIT_FIELDS = [
  'ModelRequestRateLimitEnabled',
  'ModelRequestRateLimitDurationMinutes',
  'ModelRequestRateLimitCount',
  'ModelRequestRateLimitSuccessCount',
] as const

function parseRequestCounts(value: string | undefined) {
  return JSON.parse(value?.trim() || '{}') as Record<string, [number, number]>
}

function parseGroupPolicies(value: string | undefined) {
  return JSON.parse(value?.trim() || '{}') as Record<
    string,
    GroupRateLimitPolicy
  >
}

function buildFormDefaults(
  defaults: RateLimitOptionValues
): RateLimitFormValues {
  return {
    ModelRequestRateLimitEnabled: defaults.ModelRequestRateLimitEnabled,
    ModelRequestRateLimitDurationMinutes:
      defaults.ModelRequestRateLimitDurationMinutes,
    ModelRequestRateLimitCount: defaults.ModelRequestRateLimitCount,
    ModelRequestRateLimitSuccessCount:
      defaults.ModelRequestRateLimitSuccessCount,
    ModelRequestRateLimitGroup: defaults.ModelRequestRateLimitGroup,
    group_rate_limit_setting: {
      member_enabled: defaults['group_rate_limit_setting.member_enabled'],
      shared_pool_enabled:
        defaults['group_rate_limit_setting.shared_pool_enabled'],
      policies: defaults['group_rate_limit_setting.policies'],
    },
  }
}

function normalizeFormValues(
  values: RateLimitFormValues
): RateLimitOptionValues {
  return {
    ModelRequestRateLimitEnabled: values.ModelRequestRateLimitEnabled,
    ModelRequestRateLimitDurationMinutes:
      values.ModelRequestRateLimitDurationMinutes,
    ModelRequestRateLimitCount: values.ModelRequestRateLimitCount,
    ModelRequestRateLimitSuccessCount: values.ModelRequestRateLimitSuccessCount,
    ModelRequestRateLimitGroup: values.ModelRequestRateLimitGroup,
    'group_rate_limit_setting.member_enabled':
      values.group_rate_limit_setting.member_enabled,
    'group_rate_limit_setting.shared_pool_enabled':
      values.group_rate_limit_setting.shared_pool_enabled,
    'group_rate_limit_setting.policies':
      values.group_rate_limit_setting.policies,
  }
}

export function RateLimitSection({ defaultValues }: RateLimitSectionProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [useVisualEditor, setUseVisualEditor] = useState(true)
  const rateLimitSchema = createRateLimitSchema(t)

  const form = useForm<RateLimitFormValues>({
    resolver: zodResolver(rateLimitSchema),
    mode: 'onChange',
    defaultValues: buildFormDefaults(defaultValues),
  })

  useEffect(() => {
    form.reset(buildFormDefaults(defaultValues))
  }, [defaultValues, form])

  const saveRateLimits = useMutation({
    mutationFn: async (formValues: RateLimitFormValues) => {
      const values = normalizeFormValues(formValues)
      const requests: Array<Promise<{ success: boolean; message: string }>> = []
      const groupSettingsChanged =
        values.ModelRequestRateLimitGroup !==
          defaultValues.ModelRequestRateLimitGroup ||
        values['group_rate_limit_setting.member_enabled'] !==
          defaultValues['group_rate_limit_setting.member_enabled'] ||
        values['group_rate_limit_setting.shared_pool_enabled'] !==
          defaultValues['group_rate_limit_setting.shared_pool_enabled'] ||
        values['group_rate_limit_setting.policies'] !==
          defaultValues['group_rate_limit_setting.policies']

      if (groupSettingsChanged) {
        const request: UpdateGroupRateLimitOptionsRequest = {
          member_enabled: values['group_rate_limit_setting.member_enabled'],
          shared_pool_enabled:
            values['group_rate_limit_setting.shared_pool_enabled'],
          model_request_rate_limit_group: parseRequestCounts(
            values.ModelRequestRateLimitGroup
          ),
          policies: parseGroupPolicies(
            values['group_rate_limit_setting.policies']
          ),
        }
        requests.push(updateGroupRateLimitOptions(request))
      }

      for (const key of GLOBAL_RATE_LIMIT_FIELDS) {
        if (values[key] !== defaultValues[key]) {
          requests.push(updateSystemOption({ key, value: values[key] }))
        }
      }

      const responses = await Promise.all(requests)
      const failed = responses.find((response) => !response.success)
      if (failed) {
        throw new Error(failed.message || t('Failed to update setting'))
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['system-options'] })
      toast.success(t('Setting updated successfully'))
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to update setting'))
    },
  })

  const onSubmit = async (values: RateLimitFormValues) => {
    await saveRateLimits.mutateAsync(values)
  }

  const requestCounts = form.watch('ModelRequestRateLimitGroup') ?? '{}'
  const policies = form.watch('group_rate_limit_setting.policies') ?? '{}'

  return (
    <SettingsSection title={t('Rate Limiting')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={saveRateLimits.isPending}
            saveLabel='Save rate limits'
          />

          <FormField
            control={form.control}
            name='ModelRequestRateLimitEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable rate limiting')}</FormLabel>
                  <FormDescription>
                    {t(
                      'This controls model request rate limiting. Web/API route throttling is configured by environment variables and may still return 429.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <div className='grid gap-4 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='ModelRequestRateLimitDurationMinutes'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Limit period')}</FormLabel>
                  <FormControl>
                    <div className='flex items-center gap-2'>
                      <Input
                        type='number'
                        min={0}
                        step={1}
                        {...field}
                        onChange={(event) =>
                          field.onChange(Number(event.target.value) || 0)
                        }
                      />
                      <span className='text-muted-foreground text-sm'>
                        {t('minutes')}
                      </span>
                    </div>
                  </FormControl>
                  <FormDescription>
                    {t('Time window for rate limiting')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='ModelRequestRateLimitCount'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max requests per period')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={100000000}
                      step={1}
                      {...field}
                      onChange={(event) =>
                        field.onChange(Number(event.target.value) || 0)
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Including failed requests, 0 = unlimited')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='ModelRequestRateLimitSuccessCount'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max successful requests')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={100000000}
                      step={1}
                      {...field}
                      onChange={(event) =>
                        field.onChange(Number(event.target.value) || 1)
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Only successful requests')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <Card>
            <CardHeader>
              <CardTitle>{t('Group enforcement')}</CardTitle>
              <CardDescription>
                {t(
                  'Member limits are calculated per user. Shared pools are additional group-wide hard limits across all application nodes.'
                )}
              </CardDescription>
            </CardHeader>
            <CardContent className='space-y-4'>
              <FormField
                control={form.control}
                name='group_rate_limit_setting.member_enabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Enable group member limits')}</FormLabel>
                      <FormDescription>
                        {t(
                          'Each user receives an independent group limit; a user-specific limit can only make it stricter.'
                        )}
                      </FormDescription>
                    </SettingsSwitchContent>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </SettingsSwitchItem>
                )}
              />
              <FormField
                control={form.control}
                name='group_rate_limit_setting.shared_pool_enabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Enable group shared pools')}</FormLabel>
                      <FormDescription>
                        {t(
                          'Users, tokens, and nodes in the same group consume one shared RPM, concurrency, and streaming TPS pool.'
                        )}
                      </FormDescription>
                    </SettingsSwitchContent>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </SettingsSwitchItem>
                )}
              />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
                <div>
                  <CardTitle>{t('Group rate-limit policies')}</CardTitle>
                  <CardDescription>
                    {t(
                      'Closing either switch keeps its configured values so they are restored when re-enabled.'
                    )}
                  </CardDescription>
                </div>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() => setUseVisualEditor((current) => !current)}
                >
                  {useVisualEditor ? (
                    <>
                      <Code2 className='mr-2 h-4 w-4' />
                      {t('JSON Mode')}
                    </>
                  ) : (
                    <>
                      <Palette className='mr-2 h-4 w-4' />
                      {t('Visual Mode')}
                    </>
                  )}
                </Button>
              </div>
            </CardHeader>
            <CardContent>
              {useVisualEditor ? (
                <RateLimitVisualEditor
                  requestCounts={requestCounts}
                  policies={policies}
                  onRequestCountsChange={(value) =>
                    form.setValue('ModelRequestRateLimitGroup', value, {
                      shouldDirty: true,
                      shouldValidate: true,
                    })
                  }
                  onPoliciesChange={(value) =>
                    form.setValue('group_rate_limit_setting.policies', value, {
                      shouldDirty: true,
                      shouldValidate: true,
                    })
                  }
                />
              ) : (
                <div className='grid gap-5 lg:grid-cols-2'>
                  <FormField
                    control={form.control}
                    name='ModelRequestRateLimitGroup'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>
                          {t('Request-count overrides JSON')}
                        </FormLabel>
                        <FormControl>
                          <JsonCodeEditor
                            value={field.value || ''}
                            onChange={field.onChange}
                            name={field.name}
                            onBlur={field.onBlur}
                            textareaRef={field.ref}
                            placeholder={`{\n  "default": [200, 100]\n}`}
                            aria-invalid={Boolean(
                              form.formState.errors.ModelRequestRateLimitGroup
                            )}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Keeps the legacy [total requests, successful requests] format.'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name='group_rate_limit_setting.policies'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>
                          {t('Member and shared-pool JSON')}
                        </FormLabel>
                        <FormControl>
                          <JsonCodeEditor
                            value={field.value || ''}
                            onChange={field.onChange}
                            name={field.name}
                            onBlur={field.onBlur}
                            textareaRef={field.ref}
                            placeholder={`{\n  "default": {\n    "member_limits": { "rpm_limit": 60 },\n    "shared_pool": { "rpm_limit": 3000 }\n  }\n}`}
                            aria-invalid={Boolean(
                              form.formState.errors.group_rate_limit_setting
                                ?.policies
                            )}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Omit a field or set it to null to leave that layer unlimited.'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>
              )}
            </CardContent>
          </Card>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
