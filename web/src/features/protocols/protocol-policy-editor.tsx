import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { JsonCodeEditor } from '@/components/json-code-editor'
import { LoadingState } from '@/components/loading-state'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { useProtocolCatalog } from './api'
import { inheritedProtocolPolicy, parseProtocolPolicy } from './policy'
import type { ProtocolPolicy } from './types'

type ProtocolPolicyEditorProps = {
  value: string
  onChange: (value: string) => void
  disabled?: boolean
  inherit?: boolean
}

export function ProtocolPolicyEditor(props: ProtocolPolicyEditorProps) {
  const { t } = useTranslation()
  const id = useId()
  const catalog = useProtocolCatalog()
  const policy = parseProtocolPolicy(props.value)
  const inherited = props.inherit
    ? catalog.data?.global_policy
    : catalog.data?.defaults
  const fields = [
    {
      key: 'conversion' as const,
      label: t('Conversion policy'),
      options: [
        { value: 'native_only', label: t('Native only') },
        { value: 'lossless', label: t('Lossless conversion') },
        { value: 'safe', label: t('Allow safe degradation') },
      ],
    },
    {
      key: 'selection' as const,
      label: t('Upstream protocol selection'),
      options: [
        { value: 'declared', label: t('Declared capabilities') },
        { value: 'auto', label: t('Automatic discovery') },
      ],
    },
    {
      key: 'request_mode' as const,
      label: t('Request processing'),
      options: [
        { value: 'structured', label: t('Structured processing') },
        { value: 'passthrough', label: t('Pass through when eligible') },
      ],
    },
    {
      key: 'state_scope' as const,
      label: t('Conversation storage'),
      options: [
        { value: 'disabled', label: t('Disabled') },
        { value: 'bridge', label: t('Bridged requests only') },
        { value: 'all', label: t('Include native requests') },
      ],
    },
  ]

  function updatePolicy(change: Partial<ProtocolPolicy>) {
    if (!policy) return
    props.onChange(JSON.stringify({ ...policy, ...change }, null, 2))
  }

  return (
    <div className='space-y-4'>
      <div className='grid gap-4 sm:grid-cols-2'>
        {fields.map((field) => {
          const options = [
            { value: 'inherit', label: t('Use default') },
            ...field.options,
          ]
          const effective = field.options.find(
            (option) => option.value === inherited?.[field.key]
          )
          return (
            <div key={field.key} className='space-y-2'>
              <Label htmlFor={`${id}-${field.key}`}>{field.label}</Label>
              <Select
                items={options}
                value={policy?.[field.key] || 'inherit'}
                disabled={props.disabled || !policy}
                onValueChange={(value) => {
                  if (!value) return
                  updatePolicy({
                    [field.key]: value === 'inherit' ? undefined : value,
                  })
                }}
              >
                <SelectTrigger id={`${id}-${field.key}`} className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  {options.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {effective && (
                <p className='text-muted-foreground text-xs'>
                  {t('Default: {{value}}', { value: effective.label })}
                </p>
              )}
            </div>
          )
        })}
      </div>
      <fieldset disabled={props.disabled || !policy} className='space-y-2'>
        <legend className='mb-2 text-sm font-medium'>
          {t('Upstream protocols')}
        </legend>
        <div className='flex flex-wrap gap-4'>
          {catalog.isPending && (
            <LoadingState inline message={t('Loading...')} />
          )}
          {catalog.isError && (
            <ErrorState
              className='min-h-0 py-4'
              onRetry={() => void catalog.refetch()}
            />
          )}
          {catalog.data?.catalog.protocols.map((protocol) => (
            <Label key={protocol.id} className='flex items-center gap-2'>
              <Checkbox
                disabled={props.disabled || !policy}
                checked={
                  policy?.upstream_protocols?.includes(protocol.id) || false
                }
                onCheckedChange={(checked) => {
                  const protocols = policy?.upstream_protocols || []
                  updatePolicy({
                    upstream_protocols: checked
                      ? [...protocols, protocol.id]
                      : protocols.filter((value) => value !== protocol.id),
                  })
                }}
              />
              {protocol.name}
            </Label>
          ))}
        </div>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Leave empty to use channel defaults. Automatic discovery uses request failures and never sends background probes.'
          )}
        </p>
      </fieldset>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Safe degradation preserves required tools, history, and output constraints. Conversation storage respects store=false.'
        )}
      </p>
      <div className='space-y-2'>
        <Label htmlFor={`${id}-json`}>
          {t('Protocol rules and limits (JSON)')}
        </Label>
        <JsonCodeEditor
          id={`${id}-json`}
          ariaLabel={t('Protocol rules and limits (JSON)')}
          value={props.value || inheritedProtocolPolicy}
          onChange={props.onChange}
          disabled={props.disabled}
          aria-invalid={!policy}
          heightClassName='h-44 min-h-44 max-h-44'
        />
        {!policy && <p role='alert'>{t('Invalid protocol policy JSON')}</p>}
      </div>
    </div>
  )
}
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
