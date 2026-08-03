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
import {
  Add01Icon,
  ArrowDown01Icon,
  ArrowUp01Icon,
  Delete02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { nanoid } from 'nanoid'
import { useFieldArray, useWatch, type UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ReactIconByName } from '@/components/react-icon-by-name'
import { Button } from '@/components/ui/button'
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
import { REACT_ICON_NAME_MAX_LENGTH } from '@/lib/react-icon-name'
import { TOP_NAV_ICONS } from '@/lib/top-nav-icons'

import { getCustomHeaderNavOrderKey } from './config'
import type { HeaderNavFormValues } from './header-navigation-form'

type HeaderNavigationCustomEditorProps = {
  form: UseFormReturn<HeaderNavFormValues>
}

export function HeaderNavigationCustomEditor(
  props: HeaderNavigationCustomEditorProps
) {
  const { t } = useTranslation()
  const customNavigationFields = useFieldArray({
    control: props.form.control,
    name: 'custom',
    keyName: 'fieldKey',
  })
  const customNavigation = useWatch({
    control: props.form.control,
    name: 'custom',
  })
  const navigationOrder = useWatch({
    control: props.form.control,
    name: 'order',
  })

  const addCustomNavigation = () => {
    const id = `nav-${nanoid(10)}`
    customNavigationFields.append({
      id,
      title: '',
      url: '',
      icon: '',
      enabled: true,
    })
    props.form.setValue(
      'order',
      [...props.form.getValues('order'), getCustomHeaderNavOrderKey(id)],
      { shouldDirty: true }
    )
  }

  const removeCustomNavigation = (index: number) => {
    const id = props.form.getValues(`custom.${index}.id`)
    props.form.setValue(
      'order',
      props.form
        .getValues('order')
        .filter((key) => key !== getCustomHeaderNavOrderKey(id)),
      { shouldDirty: true }
    )
    customNavigationFields.remove(index)
  }

  const moveNavigation = (key: string, direction: -1 | 1) => {
    const current = [...props.form.getValues('order')]
    const currentIndex = current.indexOf(key)
    const nextIndex = currentIndex + direction
    if (currentIndex < 0 || nextIndex < 0 || nextIndex >= current.length) return

    ;[current[currentIndex], current[nextIndex]] = [
      current[nextIndex],
      current[currentIndex],
    ]
    props.form.setValue('order', current, { shouldDirty: true })
  }

  const navigationTitles: Record<string, string> = {
    home: t('Home'),
    console: t('Console'),
    pricing: t('Model Square'),
    modelStatus: t('Model Status'),
    modelRadar: t('Model Radar'),
    rankings: t('Rankings'),
    docs: t('Docs'),
    about: t('About'),
  }

  for (const item of customNavigation) {
    navigationTitles[getCustomHeaderNavOrderKey(item.id)] =
      item.title.trim() || t('Custom navigation')
  }

  return (
    <>
      <div
        data-settings-form-span='full'
        className='flex min-w-0 flex-col gap-3'
      >
        <div className='flex items-center justify-between gap-3'>
          <h4 className='min-w-0 text-sm font-medium'>
            {t('Custom navigation')}
          </h4>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={addCustomNavigation}
            disabled={customNavigationFields.fields.length >= 20}
          >
            <HugeiconsIcon
              icon={Add01Icon}
              data-icon='inline-start'
              strokeWidth={2}
              aria-hidden='true'
            />
            {t('Add navigation')}
          </Button>
        </div>

        {customNavigationFields.fields.length > 0 && (
          <div className='grid gap-3'>
            {customNavigationFields.fields.map((item, index) => (
              <div
                key={item.fieldKey}
                className='grid min-w-0 gap-3 rounded-lg border p-3'
              >
                <div className='grid min-w-0 gap-3 md:grid-cols-2'>
                  <FormField
                    control={props.form.control}
                    name={`custom.${index}.title`}
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Navigation name')}</FormLabel>
                        <FormControl>
                          <Input
                            {...field}
                            maxLength={40}
                            placeholder={t('Navigation name')}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={props.form.control}
                    name={`custom.${index}.icon`}
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Icon name')}</FormLabel>
                        <div className='flex min-w-0 items-center gap-2'>
                          <div
                            className='border-input bg-background text-muted-foreground flex size-8 shrink-0 items-center justify-center rounded-lg border'
                            aria-hidden='true'
                          >
                            <ReactIconByName
                              name={field.value}
                              fallback={
                                <HugeiconsIcon
                                  icon={TOP_NAV_ICONS.custom}
                                  className='size-4'
                                  strokeWidth={2}
                                />
                              }
                              className='size-4'
                            />
                          </div>
                          <FormControl>
                            <Input
                              {...field}
                              maxLength={REACT_ICON_NAME_MAX_LENGTH}
                              placeholder='LuRadar'
                              autoCapitalize='none'
                              autoCorrect='off'
                              spellCheck={false}
                            />
                          </FormControl>
                        </div>
                        <FormDescription className='text-xs'>
                          {t(
                            'Use a React Icons name such as LuRadar or FaGithub. Leave empty for the default link icon.'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={props.form.control}
                    name={`custom.${index}.url`}
                    render={({ field }) => (
                      <FormItem className='md:col-span-2'>
                        <FormLabel>{t('Embedded URL')}</FormLabel>
                        <FormControl>
                          <Input
                            {...field}
                            inputMode='url'
                            maxLength={2048}
                            placeholder='https://example.com'
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>

                <div className='flex items-center justify-between gap-3'>
                  <FormField
                    control={props.form.control}
                    name={`custom.${index}.enabled`}
                    render={({ field }) => (
                      <FormItem className='flex items-center gap-2'>
                        <FormControl>
                          <Switch
                            checked={field.value}
                            onCheckedChange={field.onChange}
                          />
                        </FormControl>
                        <FormLabel>{t('Enabled')}</FormLabel>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    aria-label={t('Remove navigation')}
                    title={t('Remove navigation')}
                    onClick={() => removeCustomNavigation(index)}
                  >
                    <HugeiconsIcon
                      icon={Delete02Icon}
                      strokeWidth={2}
                      aria-hidden='true'
                    />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <div
        data-settings-form-span='full'
        className='flex min-w-0 flex-col gap-3'
      >
        <h4 className='text-sm font-medium'>{t('Navigation order')}</h4>
        <div className='overflow-hidden rounded-lg border'>
          {navigationOrder.map((key, index) => (
            <div
              key={key}
              className='flex min-h-11 items-center justify-between gap-3 border-b px-3 last:border-b-0'
            >
              <span className='min-w-0 truncate text-sm'>
                {navigationTitles[key] ?? key}
              </span>
              <div className='flex shrink-0 items-center gap-1'>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-sm'
                  aria-label={t('Move navigation earlier')}
                  title={t('Move navigation earlier')}
                  disabled={index === 0}
                  onClick={() => moveNavigation(key, -1)}
                >
                  <HugeiconsIcon
                    icon={ArrowUp01Icon}
                    strokeWidth={2}
                    aria-hidden='true'
                  />
                </Button>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-sm'
                  aria-label={t('Move navigation later')}
                  title={t('Move navigation later')}
                  disabled={index === navigationOrder.length - 1}
                  onClick={() => moveNavigation(key, 1)}
                >
                  <HugeiconsIcon
                    icon={ArrowDown01Icon}
                    strokeWidth={2}
                    aria-hidden='true'
                  />
                </Button>
              </div>
            </div>
          ))}
        </div>
      </div>
    </>
  )
}
