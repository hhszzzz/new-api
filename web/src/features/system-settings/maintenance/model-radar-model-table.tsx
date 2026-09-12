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
import { useEffect } from 'react'
import { useFieldArray, type UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormField,
  FormItem,
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
import { Switch } from '@/components/ui/switch'
import {
  getVendorMeta,
  OTHER_VENDOR,
  RADAR_VENDORS,
  resolveRadarModel,
} from '@/features/model-radar/lib/model-radar'

import type { ModelRadarManagement } from '../types'
import type { ModelRadarFormValues } from './model-radar-form'

export function ModelRadarModelTable(props: {
  form: UseFormReturn<ModelRadarFormValues>
  snapshot: ModelRadarManagement['snapshot']
  disabled: boolean
}) {
  const { t } = useTranslation()
  const { fields, append, remove } = useFieldArray({
    control: props.form.control,
    name: 'models',
  })
  useEffect(() => {
    const existing = new Set(
      props.form.getValues('models').map((row) => row.model)
    )
    const additions =
      props.snapshot?.models
        .filter((row) => !existing.has(row.model))
        .map((row) => ({
          model: row.model,
          displayName: '',
          vendor: '',
          hidden: false,
          autoEffort: false,
          aliases: '',
        })) ?? []
    if (additions.length) append(additions, { shouldFocus: false })
  }, [props.snapshot, props.form, append])

  const counts = new Map(
    props.snapshot?.models.map((row) => [row.model, row.configuration_count])
  )
  return (
    <div className='min-w-0 space-y-3'>
      <h3 className='text-sm font-semibold'>{t('Model mapping')}</h3>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Models missing from the latest snapshot stay listed until you reset them.'
        )}
      </p>
      {props.form.formState.errors.models ? (
        <p role='alert' className='text-destructive text-sm'>
          {t('You can configure up to 256 model overrides.')}
        </p>
      ) : null}
      <StaticDataTable
        tableProps={{ 'aria-label': t('Model mapping') }}
        tableClassName='min-w-[1080px]'
        data={fields}
        getRowKey={(row) => row.id}
        emptyContent={t('No snapshot yet')}
        columns={[
          {
            id: 'model',
            header: t('Source model'),
            className: 'w-[30%]',
            cell: (row) => (
              <div className='flex min-w-0 flex-wrap items-center gap-2'>
                <span className='max-w-64 break-all whitespace-normal'>
                  {row.model}
                </span>
                {counts.has(row.model) ? (
                  <Badge variant='secondary'>
                    {t('{{count}} tiers', { count: counts.get(row.model) })}
                  </Badge>
                ) : null}
              </div>
            ),
          },
          {
            id: 'displayName',
            header: t('Display name'),
            cell: (row, index) => (
              <FormField
                control={props.form.control}
                name={`models.${index}.displayName`}
                render={({ field }) => (
                  <FormItem>
                    <FormControl>
                      <Input
                        {...field}
                        disabled={props.disabled}
                        maxLength={128}
                        aria-label={`${t('Display name')}: ${row.model}`}
                        placeholder={resolveRadarModel(row.model).displayName}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            ),
          },
          {
            id: 'vendor',
            header: t('Vendor'),
            cell: (row, index) => (
              <FormField
                control={props.form.control}
                name={`models.${index}.vendor`}
                render={({ field }) => {
                  const auto = getVendorMeta(
                    resolveRadarModel(row.model).vendor
                  )
                  const options = [
                    {
                      value: '',
                      label: `${t('Auto')} · ${auto.key === OTHER_VENDOR ? t('Other') : auto.label}`,
                    },
                    ...RADAR_VENDORS.map((vendor) => ({
                      value: vendor.key,
                      label: vendor.label,
                    })),
                    { value: OTHER_VENDOR, label: t('Other') },
                  ]
                  if (
                    field.value &&
                    !options.some((option) => option.value === field.value)
                  ) {
                    options.push({ value: field.value, label: field.value })
                  }
                  return (
                    <FormItem>
                      <Select
                        items={options}
                        value={field.value}
                        onValueChange={(value) => field.onChange(value ?? '')}
                        disabled={props.disabled}
                      >
                        <FormControl>
                          <SelectTrigger
                            aria-label={`${t('Vendor')}: ${row.model}`}
                            className='w-full min-w-40'
                          >
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
                      <FormMessage />
                    </FormItem>
                  )
                }}
              />
            ),
          },
          {
            id: 'aliases',
            header: t('Aliases'),
            className: 'w-[24%]',
            cell: (row, index) => (
              <FormField
                control={props.form.control}
                name={`models.${index}.aliases`}
                render={({ field }) => (
                  <FormItem>
                    <FormControl>
                      <Input
                        {...field}
                        disabled={props.disabled}
                        aria-label={`${t('Aliases')}: ${row.model}`}
                        placeholder={t('Comma-separated gateway names')}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            ),
          },
          {
            id: 'hidden',
            header: t('Hidden'),
            cell: (row, index) => (
              <FormField
                control={props.form.control}
                name={`models.${index}.hidden`}
                render={({ field }) => (
                  <FormItem>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                        disabled={props.disabled}
                        aria-label={`${t('Hidden')}: ${row.model}`}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            ),
          },
          {
            id: 'autoEffort',
            header: t('Allow tier adjustment'),
            cell: (row, index) => (
              <FormField
                control={props.form.control}
                name={`models.${index}.autoEffort`}
                render={({ field }) => (
                  <FormItem>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                        disabled={props.disabled}
                        aria-label={`${t('Allow tier adjustment')}: ${row.model}`}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            ),
          },
          {
            id: 'reset',
            header: <span className='sr-only'>{t('Reset row')}</span>,
            cell: (row, index) => (
              <Button
                type='button'
                variant='ghost'
                size='sm'
                disabled={props.disabled}
                aria-label={`${t('Reset row')}: ${row.model}`}
                onClick={() => {
                  if (!counts.has(row.model)) {
                    remove(index)
                    return
                  }
                  props.form.setValue(`models.${index}.displayName`, '', {
                    shouldDirty: true,
                    shouldValidate: true,
                  })
                  props.form.setValue(`models.${index}.vendor`, '', {
                    shouldDirty: true,
                    shouldValidate: true,
                  })
                  props.form.setValue(`models.${index}.hidden`, false, {
                    shouldDirty: true,
                  })
                  props.form.setValue(`models.${index}.autoEffort`, false, {
                    shouldDirty: true,
                  })
                  props.form.setValue(`models.${index}.aliases`, '', {
                    shouldDirty: true,
                    shouldValidate: true,
                  })
                }}
              >
                {t('Reset row')}
              </Button>
            ),
          },
        ]}
      />
    </div>
  )
}
