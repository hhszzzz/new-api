import { ChevronDown } from 'lucide-react'
import { useId, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { JsonCodeEditor } from '@/components/json-code-editor'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  SettingsControlGroup,
  SettingsSwitchField,
} from '@/features/system-settings/components/settings-form-layout'

import { useProtocolCatalog } from './api'
import {
  hasProtocolOverrides,
  inheritedProtocolPolicy,
  parseProtocolPolicy,
} from './policy'
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
  const inherited = {
    ...catalog.data?.defaults,
    ...(props.inherit ? catalog.data?.global_policy : {}),
  }
  const effective = { ...inherited, ...policy }
  const disabled = props.disabled || !policy || !catalog.data
  const conversionEnabled = Boolean(
    effective.conversion && effective.conversion !== 'native_only'
  )
  const previousConversion = useRef<'lossless' | 'safe'>('safe')
  const fields = [
    {
      key: 'conversion' as const,
      label: t('Conversion policy'),
      options: [
        { value: 'native_only', label: t('Conversion off') },
        { value: 'lossless', label: t('Lossless conversion') },
        { value: 'safe', label: t('Allow safe degradation') },
      ],
      description: t(
        'Safe degradation only omits display metadata. Required tools, history, and output constraints are preserved.'
      ),
    },
    {
      key: 'selection' as const,
      label: t('Upstream protocol selection'),
      options: [
        { value: 'declared', label: t('Declared capabilities') },
        { value: 'auto', label: t('Automatic discovery') },
      ],
      description: t(
        'Automatic discovery uses request failures and never sends background probes.'
      ),
    },
    {
      key: 'request_mode' as const,
      label: t('Request processing'),
      options: [
        { value: 'structured', label: t('Structured processing') },
        { value: 'passthrough', label: t('Pass through when eligible') },
      ],
      description: t(
        'Structured processing validates and converts requests. Eligible native requests can pass through unchanged.'
      ),
    },
    {
      key: 'state_scope' as const,
      label: t('Conversation storage'),
      options: [
        { value: 'disabled', label: t('Disabled') },
        { value: 'bridge', label: t('Bridged requests only') },
        { value: 'all', label: t('Include native requests') },
      ],
      description: t(
        'Stores conversation context for continuation. Bridged requests only is sufficient for most setups; store=false prevents storage.'
      ),
    },
  ]

  function updatePolicy(change: Partial<ProtocolPolicy>) {
    if (!policy) return
    props.onChange(JSON.stringify({ ...policy, ...change }, null, 2))
  }

  return (
    <div className='flex min-w-0 flex-col gap-5'>
      <SettingsControlGroup>
        <SettingsSwitchField
          controlId={`${id}-conversion-enabled`}
          label={t('Cross-protocol conversion')}
          description={t(
            'When off, this policy uses native protocols only. Model rules and channel overrides still apply.'
          )}
          checked={conversionEnabled}
          disabled={disabled}
          onCheckedChange={(checked) => {
            if (!checked) {
              previousConversion.current =
                effective.conversion === 'lossless' ? 'lossless' : 'safe'
            }
            updatePolicy({
              conversion: checked ? previousConversion.current : 'native_only',
            })
          }}
        />
      </SettingsControlGroup>
      <div className='grid min-w-0 gap-x-5 gap-y-5 sm:grid-cols-2'>
        {fields.map((field) => {
          const options = field.options.filter(
            (option) => option.value !== 'native_only' || !conversionEnabled
          )
          return (
            <div key={field.key} className='flex min-w-0 flex-col gap-2'>
              <Label htmlFor={`${id}-${field.key}`}>{field.label}</Label>
              <Select
                items={options}
                value={effective[field.key] ?? null}
                disabled={
                  disabled || (field.key === 'conversion' && !conversionEnabled)
                }
                onValueChange={(value) => {
                  if (!value) return
                  updatePolicy({
                    [field.key]: value,
                  })
                }}
              >
                <SelectTrigger
                  id={`${id}-${field.key}`}
                  aria-describedby={`${id}-${field.key}-description`}
                  className='w-full'
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {options.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <p
                id={`${id}-${field.key}-description`}
                className='text-muted-foreground text-xs leading-relaxed'
              >
                {field.description}
              </p>
            </div>
          )
        })}
      </div>
      <fieldset disabled={props.disabled || !policy} className='min-w-0'>
        <legend className='mb-2 text-sm font-medium'>
          {t('Upstream protocols')}
        </legend>
        <div className='flex flex-wrap gap-x-5 gap-y-3'>
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
                disabled={disabled}
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
        <p className='text-muted-foreground mt-3 text-xs leading-relaxed'>
          {t(
            'Leave empty to select protocols from the channel type and its configuration.'
          )}
        </p>
      </fieldset>
      <Collapsible defaultOpen={!policy} className='min-w-0 rounded-xl border'>
        <CollapsibleTrigger className='group focus-visible:ring-ring/50 flex w-full items-center justify-between gap-3 rounded-xl px-4 py-3 text-left text-sm font-medium outline-none focus-visible:ring-3'>
          {t('Model rules and storage limits')}
          <ChevronDown
            className='size-4 shrink-0 transition-transform group-aria-expanded:rotate-180'
            aria-hidden='true'
          />
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className='flex min-w-0 flex-col gap-3 px-4 pb-4'>
            <p className='text-muted-foreground text-xs leading-relaxed'>
              {t(
                'Model rules match the model name before channel model mapping (the left-hand name). The first matching rule applies.'
              )}
            </p>
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
              heightClassName='h-52 min-h-52 max-h-52'
            />
            {!policy && <p role='alert'>{t('Invalid protocol policy JSON')}</p>}
            {props.inherit && hasProtocolOverrides(props.value) && (
              <Button
                type='button'
                variant='outline'
                size='sm'
                className='self-start'
                disabled={props.disabled}
                onClick={() => props.onChange(inheritedProtocolPolicy)}
              >
                {t('Reset to global policy')}
              </Button>
            )}
          </div>
        </CollapsibleContent>
      </Collapsible>
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
