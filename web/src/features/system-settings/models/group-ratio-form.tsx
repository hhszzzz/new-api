/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { memo, useCallback, useState } from 'react'
import { useWatch, type UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormLabel,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'
import { getSystemOptions } from '@/features/system-settings/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageActionsPortal } from '../components/settings-page-context'
import {
  GroupRatioVisualEditor,
  type GroupSettingsSection,
} from './group-ratio-visual-editor'

type GroupFormValues = {
  GroupRatio: string
  TopupGroupRatio: string
  UserUsableGroups: string
  GroupGroupRatio: string
  AutoGroups: string
  MaxTokenAutoGroups: number
  DefaultUseAutoGroup: boolean
  GroupSpecialUsableGroup: string
}

type GroupRatioFormProps = {
  form: UseFormReturn<GroupFormValues>
  onSave: (values: GroupFormValues) => Promise<void>
  onRegistryChanged?: (values: GroupFormValues) => void
  isSaving: boolean
}

export const GroupRatioForm = memo(function GroupRatioForm({
  form,
  onSave,
  onRegistryChanged,
  isSaving,
}: GroupRatioFormProps) {
  const { t } = useTranslation()
  const values = useWatch({ control: form.control })
  const [section, setSection] = useState<GroupSettingsSection>('groups')

  const handleFieldChange = useCallback(
    (field: keyof GroupFormValues, value: string) => {
      form.setValue(field, value, {
        shouldValidate: true,
        shouldDirty: true,
      })
    },
    [form]
  )

  const refreshGroupSettings = useCallback(async () => {
    const response = requireServerSuccess(await getSystemOptions())
    const current = new Map(
      response.data.map((option) => [option.key, option.value])
    )
    const read = (key: string, fallback: string) =>
      String(current.get(key) ?? fallback)
    const synced: GroupFormValues = {
      GroupRatio: read('GroupRatio', '{}'),
      TopupGroupRatio: read('TopupGroupRatio', '{}'),
      UserUsableGroups: read('UserUsableGroups', '{}'),
      GroupGroupRatio: read('GroupGroupRatio', '{}'),
      AutoGroups: read('AutoGroups', '[]'),
      MaxTokenAutoGroups: Number(read('MaxTokenAutoGroups', '5')) || 5,
      DefaultUseAutoGroup: read('DefaultUseAutoGroup', 'false') === 'true',
      GroupSpecialUsableGroup: read(
        'group_ratio_setting.group_special_usable_group',
        '{}'
      ),
    }
    form.setValue('GroupRatio', synced.GroupRatio, { shouldDirty: false })
    form.setValue('TopupGroupRatio', synced.TopupGroupRatio, {
      shouldDirty: false,
    })
    form.setValue('UserUsableGroups', synced.UserUsableGroups, {
      shouldDirty: false,
    })
    form.setValue('GroupGroupRatio', synced.GroupGroupRatio, {
      shouldDirty: false,
    })
    form.setValue('AutoGroups', synced.AutoGroups, { shouldDirty: false })
    form.setValue('MaxTokenAutoGroups', synced.MaxTokenAutoGroups, {
      shouldDirty: false,
    })
    form.setValue('DefaultUseAutoGroup', synced.DefaultUseAutoGroup, {
      shouldDirty: false,
    })
    form.setValue('GroupSpecialUsableGroup', synced.GroupSpecialUsableGroup, {
      shouldDirty: false,
    })
    onRegistryChanged?.(synced)
  }, [form, onRegistryChanged])

  return (
    <div className='min-w-0 space-y-5'>
      <Form {...form}>
        <SettingsPageActionsPortal>
          <Button
            type='button'
            size='sm'
            onClick={form.handleSubmit(onSave)}
            disabled={isSaving}
          >
            {isSaving ? t('Saving...') : t('Save group settings')}
          </Button>
        </SettingsPageActionsPortal>
        <GroupRatioVisualEditor
          section={section}
          onSectionChange={setSection}
          defaultUseAutoGroupField={
            <FormField
              control={form.control}
              name='DefaultUseAutoGroup'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Default new tokens to Auto')}</FormLabel>
                    <FormDescription>
                      {t(
                        'When enabled, new tokens use Auto and start with all currently authorized groups in their own ordered list.'
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
          }
          groupRatio={values.GroupRatio ?? ''}
          groupGroupRatio={values.GroupGroupRatio ?? ''}
          groupSpecialUsableGroup={values.GroupSpecialUsableGroup ?? ''}
          onRegistryChanged={refreshGroupSettings}
          onChange={(field, value) =>
            handleFieldChange(field as keyof GroupFormValues, value)
          }
        />
      </Form>
    </div>
  )
})
