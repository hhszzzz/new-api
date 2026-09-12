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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import {
  ALL_VENDORS,
  OTHER_VENDOR,
  RADAR_VENDORS,
  resolveRadarModel,
} from '@/features/model-radar/lib/model-radar'

import { getModelRadarManagement } from '../api'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type { ModelRadarManagement } from '../types'
import {
  modelRadarSchema,
  parseModelRadarSettings,
  serializeModelRadarSettings,
  type ModelRadarFormValues,
} from './model-radar-form'
import { ModelRadarModelTable } from './model-radar-model-table'
import { ModelRadarSyncCard } from './model-radar-sync-card'

export function ModelRadarSection(props: { initialSerialized: string }) {
  const { t } = useTranslation()
  const management = useQuery({
    queryKey: ['model-radar-management'],
    queryFn: getModelRadarManagement,
  })
  let content = <LoadingState />
  if (management.data) {
    content = (
      <>
        <ModelRadarSyncCard management={management.data} />
        <ModelRadarDisplayForm
          key={props.initialSerialized}
          initialSerialized={props.initialSerialized}
          management={management.data}
        />
      </>
    )
  } else if (management.isError) {
    content = <ErrorState onRetry={() => void management.refetch()} />
  }
  return <SettingsSection title={t('Model Radar')}>{content}</SettingsSection>
}

function ModelRadarDisplayForm(props: {
  initialSerialized: string
  management: ModelRadarManagement
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const updateOption = useUpdateOption()
  const form = useForm<ModelRadarFormValues>({
    resolver: zodResolver(modelRadarSchema),
    defaultValues: parseModelRadarSettings(props.initialSerialized),
  })
  const currentVendors = new Set(
    props.management.snapshot?.models.map(
      (row) => resolveRadarModel(row.model, props.management.settings).vendor
    )
  )
  const vendors = [
    ...RADAR_VENDORS,
    { key: OTHER_VENDOR, label: t('Other') },
  ].sort(
    (a, b) =>
      Number(currentVendors.has(b.key)) - Number(currentVendors.has(a.key))
  )
  const options = [
    { value: ALL_VENDORS, label: t('All') },
    ...vendors.map((vendor) => ({ value: vendor.key, label: vendor.label })),
  ]
  const defaultVendor = useWatch({
    control: form.control,
    name: 'defaultVendor',
  })
  if (!options.some((option) => option.value === defaultVendor)) {
    options.push({ value: defaultVendor, label: defaultVendor })
  }
  const onSubmit = async (values: ModelRadarFormValues) => {
    const serialized = serializeModelRadarSettings(values)
    if (
      serialized ===
      serializeModelRadarSettings(
        parseModelRadarSettings(props.initialSerialized)
      )
    ) {
      return
    }
    const result = await updateOption
      .mutateAsync({ key: 'ModelRadarSettings', value: serialized })
      .catch(() => null)
    if (!result?.success) return
    form.reset(values)
    void queryClient.invalidateQueries({ queryKey: ['model-radar'] })
    void queryClient.invalidateQueries({ queryKey: ['model-radar-management'] })
  }
  return (
    <Form {...form}>
      <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
        <SettingsPageFormActions
          onSave={form.handleSubmit(onSubmit)}
          isSaving={updateOption.isPending}
          saveLabel='Save radar settings'
        />
        <h3 className='text-sm font-semibold'>{t('Display settings')}</h3>
        <FormField
          control={form.control}
          name='defaultVendor'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Default vendor')}</FormLabel>
              <Select
                items={options}
                value={field.value}
                onValueChange={field.onChange}
                disabled={updateOption.isPending}
              >
                <FormControl>
                  <SelectTrigger className='w-full'>
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  {options.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FormDescription>
                {t('Vendor tab selected when the radar page opens.')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='showDegradationAlerts'
          render={({ field }) => (
            <SettingsSwitchItem>
              <SettingsSwitchContent>
                <FormLabel>{t('Show degradation alerts')}</FormLabel>
                <FormDescription>
                  {t(
                    'Hide the alert section even when the source reports declines.'
                  )}
                </FormDescription>
              </SettingsSwitchContent>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  disabled={updateOption.isPending}
                />
              </FormControl>
            </SettingsSwitchItem>
          )}
        />
        <FormField
          control={form.control}
          name='autoEffortEnabled'
          render={({ field }) => (
            <SettingsSwitchItem>
              <SettingsSwitchContent>
                <FormLabel>{t('Allow automatic reasoning tiers')}</FormLabel>
                <FormDescription>
                  {t(
                    'Let users switch each model on so requests that pick a reasoning tier run on the tier the radar scores best.'
                  )}
                </FormDescription>
              </SettingsSwitchContent>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  disabled={updateOption.isPending}
                />
              </FormControl>
            </SettingsSwitchItem>
          )}
        />
        <ModelRadarModelTable
          form={form}
          snapshot={props.management.snapshot}
          disabled={updateOption.isPending}
        />
      </SettingsForm>
    </Form>
  )
}
