/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { Reorder } from 'motion/react'
import { useMemo, type ComponentProps } from 'react'
import { useTranslation } from 'react-i18next'

import { AutoGroupOrderItem } from '@/components/auto-group-order-item'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { cn } from '@/lib/utils'

import {
  ApiKeyGroupCombobox,
  type ApiKeyGroupOption,
} from './api-key-group-combobox'

type AutoGroupOrderEditorProps = Omit<ComponentProps<'div'>, 'onChange'> & {
  value: string[]
  options: ApiKeyGroupOption[]
  onChange: (value: string[]) => void
  'data-slot'?: string
  'data-form-root'?: string
}

export function AutoGroupOrderEditor(props: AutoGroupOrderEditorProps) {
  const { t } = useTranslation()
  const candidates = useMemo(
    () =>
      props.options.filter(
        (option) =>
          option.value !== 'auto' && !props.value.includes(option.value)
      ),
    [props.options, props.value]
  )

  const handleAdd = (group: string) => {
    if (props.value.includes(group)) return
    props.onChange([...props.value, group])
  }

  const handleRemove = (group: string) => {
    props.onChange(props.value.filter((item) => item !== group))
  }

  const handleMove = (index: number, direction: 'up' | 'down') => {
    const targetIndex = direction === 'up' ? index - 1 : index + 1
    if (targetIndex < 0 || targetIndex >= props.value.length) return
    const next = [...props.value]
    ;[next[index], next[targetIndex]] = [next[targetIndex], next[index]]
    props.onChange(next)
  }

  return (
    <div
      id={props.id}
      data-slot={props['data-slot']}
      data-form-root={props['data-form-root']}
      role='group'
      tabIndex={-1}
      aria-label={props['aria-label'] || t('Auto group order')}
      aria-describedby={props['aria-describedby']}
      aria-invalid={props['aria-invalid']}
      className={cn('flex flex-col gap-3', props.className)}
    >
      <p className='text-muted-foreground text-xs' aria-live='polite'>
        {t('{{count}} groups selected', { count: props.value.length })}
      </p>

      <ApiKeyGroupCombobox
        options={candidates}
        value={undefined}
        onValueChange={handleAdd}
        placeholder={t('Add Auto group')}
        disabled={candidates.length === 0}
      />

      {props.value.length === 0 ? (
        <Empty className='min-h-24 border'>
          <EmptyHeader>
            <EmptyTitle>{t('Auto group order')}</EmptyTitle>
            <EmptyDescription>
              {t('Select at least one Auto group.')}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <Reorder.Group
          axis='y'
          values={props.value}
          onReorder={props.onChange}
          className='flex flex-col gap-2'
        >
          {props.value.map((group, index) => (
            <AutoGroupOrderItem
              key={group}
              group={group}
              index={index}
              count={props.value.length}
              onMove={handleMove}
              onRemove={handleRemove}
            />
          ))}
        </Reorder.Group>
      )}
    </div>
  )
}
