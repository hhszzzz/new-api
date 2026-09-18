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
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { JsonCodeEditor } from '@/components/json-code-editor'
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

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const schema = z.object({
  global: z.object({
    thinking_model_blacklist: z.string().refine((value) => {
      try {
        return Array.isArray(JSON.parse(value || '[]'))
      } catch {
        return false
      }
    }, 'Invalid JSON format'),
  }),
  general_setting: z.object({
    ping_interval_enabled: z.boolean(),
    ping_interval_seconds: z.coerce.number().min(1),
  }),
})
type FormValues = z.output<typeof schema>
type FormInput = z.input<typeof schema>
function flattenValues(values: FormValues) {
  return {
    'global.thinking_model_blacklist': JSON.stringify(
      JSON.parse(values.global.thinking_model_blacklist || '[]')
    ),
    'general_setting.ping_interval_enabled':
      values.general_setting.ping_interval_enabled,
    'general_setting.ping_interval_seconds':
      values.general_setting.ping_interval_seconds,
  }
}
export function GlobalSettingsCard(props: { defaultValues: FormValues }) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const form = useForm<FormInput, unknown, FormValues>({
    resolver: zodResolver(schema),
    defaultValues: props.defaultValues,
  })
  useEffect(() => {
    form.reset(props.defaultValues)
  }, [props.defaultValues, form])
  const pingEnabled = useWatch({
    control: form.control,
    name: 'general_setting.ping_interval_enabled',
  })
  async function onSubmit(values: FormValues) {
    const previous = flattenValues(props.defaultValues)
    const updates = Object.entries(flattenValues(values)).filter(
      ([key, value]) => value !== previous[key as keyof typeof previous]
    )
    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }
    for (const [key, value] of updates) {
      await updateOption.mutateAsync({ key, value })
    }
  }
  return (
    <SettingsSection title={t('Global Model Configuration')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />
          <FormField
            control={form.control}
            name='global.thinking_model_blacklist'
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t('Models that skip thinking suffix processing')}
                </FormLabel>
                <FormControl>
                  <JsonCodeEditor
                    value={field.value}
                    onChange={field.onChange}
                    name={field.name}
                    onBlur={field.onBlur}
                    textareaRef={field.ref}
                    ariaLabel={t('Models that skip thinking suffix processing')}
                    heightClassName='h-32 min-h-32 max-h-32'
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='general_setting.ping_interval_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable Ping')}</FormLabel>
                  <FormDescription>
                    {t('Send keep-alive messages during streaming requests.')}
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
          {pingEnabled && (
            <FormField
              control={form.control}
              name='general_setting.ping_interval_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Ping Interval (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      {...field}
                      value={String(field.value ?? '')}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          )}
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
