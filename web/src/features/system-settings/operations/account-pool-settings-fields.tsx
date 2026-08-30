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
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { MultiSelect } from '@/components/multi-select'
import {
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
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import type { AccountPoolSettingsValues } from './account-pool-settings-schema'

type NumericFieldName = Exclude<
  keyof AccountPoolSettingsValues,
  'enabled' | 'allowed_groups'
>

const numericFields: Array<{
  name: NumericFieldName
  label: string
  description: string
  min: number
  max: number
}> = [
  {
    name: 'regular_refresh_seconds',
    label: 'Regular refresh interval',
    description: 'Used while no quota window is close to resetting.',
    min: 60,
    max: 3600,
  },
  {
    name: 'near_reset_threshold_seconds',
    label: 'Near-reset threshold',
    description: 'Switch to the faster interval inside this reset window.',
    min: 60,
    max: 3600,
  },
  {
    name: 'near_reset_refresh_seconds',
    label: 'Near-reset refresh interval',
    description: 'Refresh frequency while a quota reset is close.',
    min: 30,
    max: 600,
  },
  {
    name: 'post_reset_delay_seconds',
    label: 'Post-reset delay',
    description: 'Wait briefly after reset_at before fetching the new window.',
    min: 0,
    max: 120,
  },
  {
    name: 'manual_refresh_cooldown_seconds',
    label: 'Manual refresh cooldown',
    description: 'Global cooldown shared by every account-pool viewer.',
    min: 30,
    max: 600,
  },
]

type AccountPoolSettingsFieldsProps = {
  form: UseFormReturn<AccountPoolSettingsValues>
  groups: string[]
  disabled: boolean
}

export function AccountPoolSettingsFields(
  props: AccountPoolSettingsFieldsProps
) {
  const { t } = useTranslation()
  return (
    <>
      <FormField
        control={props.form.control}
        name='enabled'
        render={({ field }) => (
          <SettingsSwitchItem>
            <SettingsSwitchContent>
              <FormLabel>{t('Enable account pool')}</FormLabel>
              <FormDescription>
                {t(
                  'Show the read-only Codex account pool to administrators and allowed groups.'
                )}
              </FormDescription>
            </SettingsSwitchContent>
            <FormControl>
              <Switch
                checked={field.value}
                onCheckedChange={field.onChange}
                disabled={props.disabled}
              />
            </FormControl>
          </SettingsSwitchItem>
        )}
      />

      <FormField
        control={props.form.control}
        name='allowed_groups'
        render={({ field }) => (
          <FormItem className='lg:col-span-2'>
            <FormLabel>{t('Allowed user groups')}</FormLabel>
            <FormControl>
              <MultiSelect
                options={props.groups.map((group) => ({
                  label: group,
                  value: group,
                }))}
                selected={field.value ?? []}
                onChange={field.onChange}
                placeholder={t('Select groups')}
                disabled={props.disabled}
                maxVisibleChips={8}
              />
            </FormControl>
            <FormDescription>
              {t(
                'An empty list keeps the pool administrator-only. Any matching effective group grants access.'
              )}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      {numericFields.map((config) => (
        <FormField
          key={config.name}
          control={props.form.control}
          name={config.name}
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                {t(config.label)} ({t('seconds')})
              </FormLabel>
              <FormControl>
                <Input
                  type='number'
                  min={config.min}
                  max={config.max}
                  disabled={props.disabled}
                  {...field}
                />
              </FormControl>
              <FormDescription>{t(config.description)}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      ))}
    </>
  )
}
