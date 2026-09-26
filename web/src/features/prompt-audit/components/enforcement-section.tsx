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
import { ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { MultiSelect } from '@/components/multi-select'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import {
  SettingsControlChildren,
  SettingsFormGrid,
  SettingsFormGridItem,
  SettingsSwitchField,
} from '@/features/system-settings/components/settings-form-layout'
import { SettingsSection } from '@/features/system-settings/components/settings-section'

import { DEFAULT_PROBE_SEMANTIC_THRESHOLD } from '../lib'
import type { PromptAuditCategory, PromptAuditConfigUpdate } from '../types'
import { NumberField } from './number-field'

type EnforcementSectionProps = {
  config: PromptAuditConfigUpdate
  categories: PromptAuditCategory[]
  groups: string[]
  onChange: (patch: Partial<PromptAuditConfigUpdate>) => void
}

export function EnforcementSection({
  config,
  categories,
  groups,
  onChange,
}: EnforcementSectionProps) {
  const { t } = useTranslation()

  let modeDescription = t(
    'Synchronous interception: requests wait for the audit result before dispatch. Risk matches return 403; audit failures return 503.'
  )
  if (config.mode === 'off') {
    modeDescription = t('Requests are not sent to audit models.')
  } else if (config.mode === 'async_audit') {
    modeDescription = t(
      'Requests continue normally while durable workers record would_action.'
    )
  }

  let outputModeDescription = t('Generated output is not collected for audit.')
  if (config.output_mode === 'blocking') {
    outputModeDescription = t(
      'Synchronous full-buffer blocking: generation is buffered and delivered only after review passes, adding end-to-end latency. Blocked content returns 403 and generation usage is still billed.'
    )
  } else if (config.output_mode === 'async_audit') {
    outputModeDescription = t(
      'Streaming remains live; risky output is recorded as delivered and recommended for blocking.'
    )
  }

  return (
    <SettingsSection title={t('Enforcement policy')}>
      <p className='text-muted-foreground text-xs leading-relaxed'>
        {t(
          'Probe blocking and wordlist filtering run before model audit, billing, and upstream dispatch.'
        )}
      </p>

      <SettingsFormGrid>
        <SettingsFormGridItem>
          <Field>
            <FieldLabel htmlFor='prompt-audit-mode'>
              {t('Model audit mode')}
            </FieldLabel>
            <NativeSelect
              id='prompt-audit-mode'
              value={config.mode}
              onChange={(event) =>
                onChange({
                  mode: event.target.value as PromptAuditConfigUpdate['mode'],
                })
              }
            >
              <NativeSelectOption value='off'>{t('Off')}</NativeSelectOption>
              <NativeSelectOption value='async_audit'>
                {t('Async audit')}
              </NativeSelectOption>
              <NativeSelectOption value='blocking'>
                {t('Blocking')}
              </NativeSelectOption>
            </NativeSelect>
            <FieldDescription>{modeDescription}</FieldDescription>
          </Field>
        </SettingsFormGridItem>

        <SettingsFormGridItem>
          <Field>
            <FieldLabel htmlFor='prompt-audit-output-mode'>
              {t('Output audit mode')}
            </FieldLabel>
            <NativeSelect
              id='prompt-audit-output-mode'
              value={config.output_mode}
              onChange={(event) =>
                onChange({
                  output_mode: event.target
                    .value as PromptAuditConfigUpdate['output_mode'],
                })
              }
            >
              <NativeSelectOption value='off'>{t('Off')}</NativeSelectOption>
              <NativeSelectOption value='async_audit'>
                {t('Async observation')}
              </NativeSelectOption>
              <NativeSelectOption value='blocking'>
                {t('Full-buffer blocking')}
              </NativeSelectOption>
            </NativeSelect>
            <FieldDescription>{outputModeDescription}</FieldDescription>
          </Field>
        </SettingsFormGridItem>
      </SettingsFormGrid>

      <div className='divide-y'>
        <SettingsSwitchField
          controlId='prompt-audit-probe-block'
          checked={config.probe_block_enabled ?? false}
          onCheckedChange={(probe_block_enabled) =>
            onChange({ probe_block_enabled })
          }
          label={t('Block probe requests')}
          description={t(
            'Block standalone greetings and health checks in every audit mode. Token-count requests are exempt.'
          )}
        />
        {config.probe_block_enabled && (
          <SettingsControlChildren className='pb-3'>
            {/* Folded away by default: the phrase list is long, and leaving it
                open pushed every setting below it out of view for as long as
                probe blocking stayed on. */}
            <Collapsible className='flex flex-col gap-3'>
              <CollapsibleTrigger
                render={
                  <Button
                    type='button'
                    variant='ghost'
                    className='group justify-between px-0 hover:bg-transparent aria-expanded:bg-transparent dark:hover:bg-transparent'
                  />
                }
              >
                <span className='flex items-center gap-2'>
                  <span className='text-sm font-medium'>
                    {t('Probe phrases')}
                  </span>
                  <Badge variant='secondary' className='text-xs font-normal'>
                    {(config.probe_phrases ?? []).length}
                  </Badge>
                </span>
                <ChevronDown
                  className='text-muted-foreground size-4 shrink-0 transition-transform duration-200 group-aria-expanded:rotate-180'
                  aria-hidden='true'
                />
              </CollapsibleTrigger>
              <CollapsibleContent className='flex flex-col gap-3'>
                <Field>
                  <Textarea
                    id='prompt-audit-probe-phrases'
                    aria-label={t('Probe phrases')}
                    value={(config.probe_phrases ?? []).join('\n')}
                    onChange={(event) =>
                      onChange({
                        probe_phrases: event.target.value.split('\n'),
                      })
                    }
                    rows={6}
                  />
                  <FieldDescription>
                    {t(
                      'One phrase per line. Matches the whole message, ignoring case and punctuation at either end. Conversation history and media are excluded.'
                    )}
                  </FieldDescription>
                </Field>
              </CollapsibleContent>
            </Collapsible>
            {/* The switch governs the whole probe gate, phrase list included,
                so it sits beside it rather than under the semantic gate. */}
            <SettingsSwitchField
              controlId='prompt-audit-probe-admins'
              checked={config.probe_include_admins ?? false}
              onCheckedChange={(probe_include_admins) =>
                onChange({ probe_include_admins })
              }
              label={t('Include administrators in probe detection')}
              description={t(
                'Administrators are exempt so their own liveness checks stay ordinary traffic. Turn this on to refuse them under the same rules.'
              )}
            />
            <SettingsSwitchField
              controlId='prompt-audit-probe-semantic'
              checked={config.probe_semantic_enabled ?? false}
              onCheckedChange={(probe_semantic_enabled) =>
                onChange({ probe_semantic_enabled })
              }
              label={t('Semantic probe detection')}
              description={t(
                'Ask a TypeSafe node whether a short standalone first message is just a liveness check, for the greetings the phrase list misses. Costs one cached model call per distinct message; a node error lets the request through.'
              )}
            />
            {config.probe_semantic_enabled && (
              <SettingsControlChildren>
                <NumberField
                  id='prompt-audit-probe-semantic-threshold'
                  label={t('Semantic probe threshold')}
                  value={
                    config.probe_semantic_threshold ??
                    DEFAULT_PROBE_SEMANTIC_THRESHOLD
                  }
                  min={0.01}
                  max={1}
                  step={0.01}
                  description={t(
                    'Probability at or above which the message counts as a probe. Higher values refuse fewer real questions.'
                  )}
                  onChange={(probe_semantic_threshold) =>
                    onChange({ probe_semantic_threshold })
                  }
                />
              </SettingsControlChildren>
            )}
          </SettingsControlChildren>
        )}
        <SettingsSwitchField
          controlId='prompt-audit-expand-base64'
          checked={config.expand_base64 ?? true}
          onCheckedChange={(expand_base64) => onChange({ expand_base64 })}
          label={t('Decode base64 before auditing')}
          description={t(
            'Decode inline base64 runs before the wordlist and audit model read the text, so a request that encodes its real content is judged on the content. The stored full request still shows what the client sent.'
          )}
        />
        <SettingsSwitchField
          controlId='prompt-audit-blocking-latest-turn-only'
          checked={config.blocking_latest_turn_only ?? true}
          onCheckedChange={(blocking_latest_turn_only) =>
            onChange({ blocking_latest_turn_only })
          }
          label={t('Scan only the latest turn')}
          description={t(
            'Inspect the latest user turn, the preceding assistant turn, and the newest tool round. Wordlists follow this switch in every mode; model audit follows it only in blocking mode, and async model audit always reads the whole request. Other enabled sources (system, developer, task) are always inspected. Turn this off to include older conversation turns.'
          )}
        />
        <SettingsSwitchField
          controlId='prompt-audit-all-groups'
          checked={config.all_groups}
          onCheckedChange={(all_groups) => onChange({ all_groups })}
          label={t('All groups')}
          description={t(
            'Apply the policy to every request group, including newly added groups.'
          )}
        />
      </div>

      {!config.all_groups && (
        <SettingsControlChildren>
          <Field>
            <FieldLabel htmlFor='prompt-audit-audited-groups'>
              {t('Audited groups')}
            </FieldLabel>
            {groups.length === 0 ? (
              <FieldDescription>
                {t('No request groups are available.')}
              </FieldDescription>
            ) : (
              <MultiSelect
                id='prompt-audit-audited-groups'
                aria-label={t('Audited groups')}
                options={groups.map((group) => ({
                  value: group,
                  label: group,
                }))}
                selected={config.groups}
                maxVisibleChips={3}
                placeholder={t('No groups selected')}
                onChange={(selected) => onChange({ groups: selected })}
              />
            )}
          </Field>
        </SettingsControlChildren>
      )}

      {/*
        Gray-area review is intentionally hidden from the UI. The backend
        capability remains intact (review_enabled stays false unless it
        was already enabled in persisted config).
      */}
      <Collapsible className='flex flex-col gap-4'>
        <CollapsibleTrigger
          render={
            <Button
              type='button'
              variant='ghost'
              className='group justify-between px-0 hover:bg-transparent aria-expanded:bg-transparent dark:hover:bg-transparent'
            />
          }
        >
          <span className='flex items-center gap-2'>
            <span className='text-sm font-medium'>{t('Risk categories')}</span>
            <Badge variant='secondary' className='text-xs font-normal'>
              {config.enabled_categories.length} / {categories.length}
            </Badge>
          </span>
          <ChevronDown
            className='text-muted-foreground size-4 shrink-0 transition-transform duration-200 group-aria-expanded:rotate-180'
            aria-hidden='true'
          />
        </CollapsibleTrigger>
        <CollapsibleContent className='flex flex-col gap-4'>
          <Field>
            {/* No visible label: the trigger directly above already names this
                control and carries the n/N count. The accessible name comes
                from the picker's aria-label. */}
            {/* Chips picker, matching the wordlist assignment further down the
                page. The old checkbox grid printed each description under its
                label; that second line has no room here, and passing it as an
                option `hint` would add a marker icon to every chip and break
                the row's visual match with the wordlist picker. */}
            <MultiSelect
              id='prompt-audit-risk-categories'
              aria-label={t('Risk categories')}
              options={categories.map((category) => ({
                value: category.id,
                label: t(category.label),
              }))}
              selected={config.enabled_categories}
              maxVisibleChips={3}
              placeholder={t('No risk categories selected')}
              onChange={(enabled_categories) =>
                onChange({ enabled_categories })
              }
            />
            <FieldDescription>
              {t(
                'Qwen3Guard always returns its complete classification; these selections only control the blocking policy.'
              )}
            </FieldDescription>
          </Field>
        </CollapsibleContent>
      </Collapsible>
    </SettingsSection>
  )
}
