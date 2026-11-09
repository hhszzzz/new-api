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
import type { Resolver } from 'react-hook-form'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
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
import { Textarea } from '@/components/ui/textarea'
import {
  formatTimestampForInput,
  parseTimestampFromInput,
} from '@/lib/format'

import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'

type ClosureSwitchKey = 'web_closed' | 'api_paused'

const PRESETS = [
  {
    titleKey: 'Scheduled maintenance',
    messageKey:
      'We are performing scheduled maintenance. Service will resume as soon as possible.',
  },
  {
    titleKey: 'Emergency maintenance',
    messageKey:
      'We are addressing an unexpected issue. Please try again later.',
  },
  {
    titleKey: 'Service upgrade',
    messageKey:
      'We are upgrading the service. It will be available again shortly.',
  },
]

function closureSwitchField(
  key: ClosureSwitchKey
): 'site_closure.web_closed' | 'site_closure.api_paused' {
  return key === 'web_closed'
    ? 'site_closure.web_closed'
    : 'site_closure.api_paused'
}

export type SiteClosureFormValues = {
  site_closure: {
    web_closed: boolean
    api_paused: boolean
    title: string
    message: string
    expected_recovery_at: number
  }
}

type SiteClosureSectionProps = {
  defaultValues: SiteClosureFormValues
}

export function SiteClosureSection({
  defaultValues,
}: SiteClosureSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [pendingSwitch, setPendingSwitch] = useState<ClosureSwitchKey | null>(
    null
  )

  const schema = z.object({
    site_closure: z.object({
      web_closed: z.boolean(),
      api_paused: z.boolean(),
      title: z.string().max(200, {
        error: () =>
          t('Maintenance title must be at most 200 characters'),
      }),
      message: z.string().max(8192, {
        error: () =>
          t('Maintenance message must be at most 8192 characters'),
      }),
      expected_recovery_at: z.number().int().min(0, {
        error: () => t('Expected recovery time must not be negative'),
      }),
    }),
  })

  const { form, handleSubmit, handleReset, isDirty, isSubmitting } =
    useSettingsForm<SiteClosureFormValues>({
      resolver: zodResolver(schema) as Resolver<
        SiteClosureFormValues,
        unknown,
        SiteClosureFormValues
      >,
      defaultValues,
      onSubmit: async (_data, changedFields) => {
        const entries = Object.entries(changedFields)
        const switches = entries.filter(
          ([key]) =>
            key === 'site_closure.web_closed' ||
            key === 'site_closure.api_paused'
        )
        const content = entries.filter(
          ([key]) =>
            key !== 'site_closure.web_closed' &&
            key !== 'site_closure.api_paused'
        )
        // Write the copy first so the site cannot close with an empty message.
        for (const [key, value] of [...content, ...switches]) {
          const normalized =
            key === 'site_closure.expected_recovery_at'
              ? Math.max(0, Number(value) || 0)
              : value
          await updateOption.mutateAsync({
            key,
            value: normalized as string | boolean | number,
          })
        }
      },
    })

  const applySwitch = (key: ClosureSwitchKey, checked: boolean) => {
    form.setValue(closureSwitchField(key), checked, { shouldDirty: true })
  }

  const requestSwitch = (key: ClosureSwitchKey, checked: boolean) => {
    if (!checked) {
      applySwitch(key, false)
      return
    }
    setPendingSwitch(key)
  }

  const confirmPendingSwitch = () => {
    if (pendingSwitch) {
      applySwitch(pendingSwitch, true)
    }
    setPendingSwitch(null)
  }

  const applyPreset = (preset: (typeof PRESETS)[number]) => {
    form.setValue('site_closure.title', t(preset.titleKey), {
      shouldDirty: true,
    })
    form.setValue('site_closure.message', t(preset.messageKey), {
      shouldDirty: true,
    })
  }

  const pendingIsWeb = pendingSwitch === 'web_closed'

  return (
    <>
      <FormNavigationGuard when={isDirty} />

      <SettingsSection title={t('Site Closure')}>
        <Form {...form}>
          <SettingsForm onSubmit={handleSubmit}>
            <SettingsPageFormActions
              onSave={handleSubmit}
              onReset={handleReset}
              isSaving={isSubmitting || updateOption.isPending}
              isResetDisabled={!isDirty}
            />
            <FormDirtyIndicator isDirty={isDirty} />

            <FormField
              control={form.control}
              name='site_closure.web_closed'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Close the website')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Non-admin visitors only see the maintenance page. Model API traffic is unaffected; administrators keep full access.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={(checked) =>
                        requestSwitch('web_closed', checked)
                      }
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />

            <FormField
              control={form.control}
              name='site_closure.api_paused'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Pause the API service')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Non-admin model requests return 503. The web console stays available and administrator keys keep working.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={(checked) =>
                        requestSwitch('api_paused', checked)
                      }
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />

            <div className='flex flex-wrap items-center gap-2'>
              <span className='text-muted-foreground text-xs font-medium'>
                {t('Preset text')}
              </span>
              {PRESETS.map((preset) => (
                <Button
                  key={preset.titleKey}
                  type='button'
                  size='sm'
                  variant='outline'
                  onClick={() => applyPreset(preset)}
                >
                  {t(preset.titleKey)}
                </Button>
              ))}
            </div>

            <FormField
              control={form.control}
              name='site_closure.title'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Maintenance title')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('Website is under maintenance!')}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Leave empty to use the built-in localized title.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='site_closure.expected_recovery_at'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Expected recovery time')}</FormLabel>
                  <FormControl>
                    <Input
                      type='datetime-local'
                      value={
                        field.value > 0
                          ? formatTimestampForInput(field.value)
                          : ''
                      }
                      onChange={(event) =>
                        field.onChange(
                          event.target.value
                            ? parseTimestampFromInput(event.target.value)
                            : 0
                        )
                      }
                      onBlur={field.onBlur}
                      name={field.name}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Optional. Shown on the maintenance page.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='site_closure.message'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Maintenance message')}</FormLabel>
                  <FormControl>
                    <Textarea
                      rows={8}
                      placeholder={t(
                        'We are performing scheduled maintenance. Service will resume as soon as possible.'
                      )}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Supports Markdown. Leave empty to use the built-in localized message.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <p className='text-muted-foreground text-xs'>
              {t(
                'Changes take effect immediately on this node and within about one minute on other nodes.'
              )}
            </p>
          </SettingsForm>
        </Form>
      </SettingsSection>

      <ConfirmDialog
        open={pendingSwitch !== null}
        onOpenChange={(open) => {
          if (!open) setPendingSwitch(null)
        }}
        title={
          pendingIsWeb
            ? t('Close the website?')
            : t('Pause the API service?')
        }
        desc={
          pendingIsWeb
            ? t(
                'Non-admin visitors will only see the maintenance page. Administrators keep full access. The change takes effect after you save.'
              )
            : t(
                'Non-admin model requests will return 503 and running client calls will fail. Administrator keys are unaffected. The change takes effect after you save.'
              )
        }
        confirmText={t('Enable')}
        destructive
        handleConfirm={confirmPendingSwitch}
      />
    </>
  )
}
