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
  ArrowDown,
  ArrowUp,
  ChevronDown,
  FlaskConical,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { MultiSelect } from '@/components/multi-select'
import { PasswordInput } from '@/components/password-input'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import {
  SettingsFormGrid,
  SettingsFormGridItem,
} from '@/features/system-settings/components/settings-form-layout'

import {
  promptAuditEndpointBaseURLUpdate,
  type PromptAuditEndpointDraft,
} from '../lib'
import type { PromptAuditDirection } from '../types'
import { NumberField } from './number-field'

type AuditModelCardProps = {
  endpoint: PromptAuditEndpointDraft
  index: number
  isFirst: boolean
  isLast: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
  onChange: (update: Partial<PromptAuditEndpointDraft>) => void
  onMoveUp: () => void
  onMoveDown: () => void
  onRemove: () => void
  canTest: boolean
  testLabel: string
  onTest: () => void
  tokenDescription: string
}

// Collapsed by default: the row carries the index, the enabled switch, the
// model name, purpose and direction badges, then move/remove/expand. Every
// control except the expand trigger sits outside the trigger so no button is
// nested inside another button. Expanded content unmounts when closed, so
// partially typed values are kept by the parent state, not the DOM.
export function AuditModelCard({
  endpoint,
  index,
  isFirst,
  isLast,
  open,
  onOpenChange,
  onChange,
  onMoveUp,
  onMoveDown,
  onRemove,
  canTest,
  testLabel,
  onTest,
  tokenDescription,
}: AuditModelCardProps) {
  const { t } = useTranslation()

  return (
    <Collapsible
      open={open}
      onOpenChange={onOpenChange}
      className='rounded-lg border'
    >
      <div className='flex items-center gap-2 px-3'>
        <Switch
          aria-label={`${t('Enabled')}: ${endpoint.model || t('New audit model')}`}
          checked={endpoint.enabled}
          onCheckedChange={(enabled) => onChange({ enabled })}
        />
        <CollapsibleTrigger
          render={
            <Button
              type='button'
              variant='ghost'
              className='group min-w-0 flex-1 justify-between gap-2 px-0 hover:bg-transparent aria-expanded:bg-transparent dark:hover:bg-transparent'
            />
          }
        >
          {/* The index, name, purpose and directions form one cluster so they
              stay adjacent: with five direct children, justify-between would
              spread the free space between each pair and pull the badges away
              from the model they describe. */}
          <span className='flex min-w-0 items-center gap-2'>
            <Badge
              variant='outline'
              className='shrink-0 font-mono text-xs'
            >{`#${index + 1}`}</Badge>
            <span className='truncate text-sm font-semibold'>
              {endpoint.model || t('New audit model')}
            </span>
            <Badge
              variant='outline'
              className='hidden max-w-40 shrink-0 truncate text-xs font-normal sm:inline'
              title={
                endpoint.purpose === 'classify'
                  ? t('Qwen3Guard classification')
                  : t('Gray-area review model')
              }
            >
              {endpoint.purpose === 'classify'
                ? t('Qwen3Guard classification')
                : t('Gray-area review model')}
            </Badge>
            <span className='text-muted-foreground hidden shrink-0 text-xs sm:inline'>
              {endpoint.directions
                .map((direction) =>
                  direction === 'input' ? t('Input') : t('Output')
                )
                .join(' · ')}
            </span>
          </span>
          <ChevronDown
            className='text-muted-foreground size-4 shrink-0 transition-transform duration-200 group-aria-expanded:rotate-180'
            aria-hidden='true'
          />
        </CollapsibleTrigger>
        <Button
          variant='ghost'
          size='icon-sm'
          aria-label={t('Move audit model up')}
          disabled={isFirst}
          onClick={onMoveUp}
        >
          <ArrowUp className='size-4' />
        </Button>
        <Button
          variant='ghost'
          size='icon-sm'
          aria-label={t('Move audit model down')}
          disabled={isLast}
          onClick={onMoveDown}
        >
          <ArrowDown className='size-4' />
        </Button>
        <Button
          variant='ghost'
          size='icon-sm'
          className='text-destructive hover:text-destructive hover:bg-destructive/10'
          aria-label={t('Remove audit model')}
          onClick={onRemove}
        >
          <Trash2 className='size-4' />
        </Button>
      </div>

      <CollapsibleContent className='border-t'>
        <SettingsFormGrid className='p-4'>
          <SettingsFormGridItem span='full'>
            <Field>
              <FieldLabel htmlFor={`prompt-audit-node-purpose-${index}`}>
                {t('Audit model purpose')}
              </FieldLabel>
              <NativeSelect
                id={`prompt-audit-node-purpose-${index}`}
                value={endpoint.purpose}
                onChange={(event) =>
                  onChange({
                    purpose: event.target
                      .value as PromptAuditEndpointDraft['purpose'],
                  })
                }
              >
                <NativeSelectOption value='classify'>
                  {t('Qwen3Guard classification')}
                </NativeSelectOption>
                <NativeSelectOption value='review'>
                  {t('Gray-area review model')}
                </NativeSelectOption>
              </NativeSelect>
            </Field>
          </SettingsFormGridItem>

          {endpoint.purpose === 'classify' ? (
            <SettingsFormGridItem span='full'>
              <Field>
                <FieldLabel htmlFor={`prompt-audit-node-directions-${index}`}>
                  {t('Audit directions')}
                </FieldLabel>
                {/* Same chips picker as the wordlist assignment further down
                    the page, so every multi-value choice looks alike. The
                    option labels stay t('...') literals the i18n key scraper
                    can see. */}
                <MultiSelect
                  id={`prompt-audit-node-directions-${index}`}
                  aria-label={t('Audit directions')}
                  options={[
                    { value: 'input', label: t('Request input') },
                    { value: 'output', label: t('Generated output') },
                  ]}
                  selected={endpoint.directions}
                  placeholder={t('No directions selected')}
                  onChange={(values) =>
                    onChange({
                      directions: values as PromptAuditDirection[],
                    })
                  }
                />
              </Field>
            </SettingsFormGridItem>
          ) : null}

          <SettingsFormGridItem>
            <Field>
              <FieldLabel htmlFor={`prompt-audit-node-url-${index}`}>
                {t('Base URL')}
              </FieldLabel>
              <Input
                id={`prompt-audit-node-url-${index}`}
                placeholder='https://guard.example.com/v1'
                value={endpoint.base_url}
                onChange={(event) =>
                  onChange(
                    promptAuditEndpointBaseURLUpdate(
                      endpoint,
                      event.target.value
                    )
                  )
                }
              />
              <FieldDescription>
                {t(
                  'Address of an OpenAI-compatible service; /chat/completions is appended automatically.'
                )}
              </FieldDescription>
            </Field>
          </SettingsFormGridItem>

          <SettingsFormGridItem>
            <Field>
              <FieldLabel htmlFor={`prompt-audit-node-model-${index}`}>
                {t('Model')}
              </FieldLabel>
              <Input
                id={`prompt-audit-node-model-${index}`}
                value={endpoint.model}
                onChange={(event) => onChange({ model: event.target.value })}
              />
            </Field>
          </SettingsFormGridItem>

          <SettingsFormGridItem span='full'>
            <div className='grid gap-x-5 gap-y-6 sm:grid-cols-3'>
              <NumberField
                id={`prompt-audit-node-timeout-${index}`}
                label={t('Timeout (ms)')}
                value={endpoint.timeout_ms}
                min={100}
                max={120000}
                description={t('Timeout for a single audit model attempt.')}
                onChange={(timeout_ms) => onChange({ timeout_ms })}
              />
              <NumberField
                id={`prompt-audit-node-limit-${index}`}
                label={t('Input limit (characters)')}
                value={endpoint.input_limit}
                min={256}
                max={1048576}
                description={t('Smallest enabled value controls chunk size.')}
                onChange={(input_limit) => onChange({ input_limit })}
              />
              <NumberField
                id={`prompt-audit-node-concurrency-${index}`}
                label={t('Audit model concurrency')}
                value={endpoint.concurrency}
                min={1}
                max={256}
                description={t(
                  'Maximum simultaneous calls to this audit model per process.'
                )}
                onChange={(concurrency) => onChange({ concurrency })}
              />
            </div>
          </SettingsFormGridItem>

          <SettingsFormGridItem span='full'>
            <Field>
              <FieldLabel htmlFor={`prompt-audit-node-token-${index}`}>
                {t('API token')}
              </FieldLabel>
              <PasswordInput
                id={`prompt-audit-node-token-${index}`}
                autoComplete='new-password'
                placeholder={
                  endpoint.has_token
                    ? t('Saved token (unchanged)')
                    : t('Optional token')
                }
                value={endpoint.token}
                onChange={(event) =>
                  onChange({ token: event.target.value, token_changed: true })
                }
              />
              <div className='flex flex-wrap items-center justify-between gap-3'>
                <FieldDescription>{tokenDescription}</FieldDescription>
                {endpoint.has_token && !endpoint.token_changed && (
                  <Button
                    variant='ghost'
                    size='sm'
                    className='h-7 px-2 text-xs'
                    onClick={() => onChange({ token: '', token_changed: true })}
                  >
                    {t('Clear saved token')}
                  </Button>
                )}
              </div>
            </Field>
          </SettingsFormGridItem>

          <SettingsFormGridItem span='full'>
            <div className='flex justify-end'>
              <Button
                variant='outline'
                size='sm'
                disabled={!canTest}
                onClick={onTest}
              >
                <FlaskConical className='size-3.5' />
                {testLabel}
              </Button>
            </div>
          </SettingsFormGridItem>
        </SettingsFormGrid>
      </CollapsibleContent>
    </Collapsible>
  )
}
