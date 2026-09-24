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
import { useTranslation } from 'react-i18next'

import { MultiSelect } from '@/components/multi-select'
import { Label } from '@/components/ui/label'
import { SettingsSwitchField } from '@/features/system-settings/components/settings-form-layout'
import { SettingsSection } from '@/features/system-settings/components/settings-section'

import { PROMPT_AUDIT_SCOPES, promptAuditScopeLabel } from '../scopes'
import type { PromptScopePolicies, PromptWordlist } from '../types'

/**
 * A scope is inspected by model review, by wordlists, or by both, and the one
 * control that picks that holds model review as an option of its own. It is not
 * a wordlist, so it never reaches the ids that are saved.
 */
const MODEL_AUDIT_OPTION = 'model_audit'

interface ScopePoliciesSectionProps {
  policies: PromptScopePolicies
  libraries: PromptWordlist[]
  wordFilterEnabled: boolean
  onChange: (policies: PromptScopePolicies) => void
  onWordFilterChange: (enabled: boolean) => void
}

export function ScopePoliciesSection(props: ScopePoliciesSectionProps) {
  const { t } = useTranslation()
  const options = [
    { value: MODEL_AUDIT_OPTION, label: t('Model audit') },
    ...props.libraries.map((library) => ({
      value: library.id,
      label:
        (library.id === 'manual' ? t('Custom wordlist') : library.name) +
        (library.enabled ? '' : ' (' + t('Disabled') + ')'),
    })),
  ]
  return (
    <SettingsSection title={t('Inspection rules by source')}>
      <p className='text-muted-foreground text-xs leading-relaxed'>
        {t(
          'Choose what inspects each part of a request: model review, wordlists, or both. Disabled wordlists keep their assignments.'
        )}
      </p>

      <SettingsSwitchField
        controlId='word-filter-enabled'
        checked={props.wordFilterEnabled}
        onCheckedChange={props.onWordFilterChange}
        label={t('Enable wordlist filtering')}
        description={t(
          'Sensitive words are now managed with prompt inspection rules and wordlists.'
        )}
      />

      <div className='divide-y'>
        {PROMPT_AUDIT_SCOPES.map((scope) => {
          const label = promptAuditScopeLabel(scope, t)
          const policy = props.policies[scope]
          return (
            <div
              key={scope}
              className='grid items-center gap-3 py-3 md:grid-cols-[13rem_1fr]'
            >
              <div className='space-y-0.5'>
                <Label
                  htmlFor={`scope-inspectors-${scope}`}
                  className='cursor-pointer text-sm font-medium'
                >
                  {label}
                </Label>
              </div>
              <div>
                <MultiSelect
                  id={`scope-inspectors-${scope}`}
                  aria-label={label}
                  options={options}
                  selected={[
                    ...(policy.model_audit ? [MODEL_AUDIT_OPTION] : []),
                    ...policy.library_ids,
                  ]}
                  maxVisibleChips={3}
                  placeholder={t('No inspection selected')}
                  onChange={(values) =>
                    props.onChange({
                      ...props.policies,
                      [scope]: {
                        library_ids: values.filter(
                          (value) => value !== MODEL_AUDIT_OPTION
                        ),
                        model_audit: values.includes(MODEL_AUDIT_OPTION),
                      },
                    })
                  }
                />
              </div>
            </div>
          )
        })}
      </div>
      <p className='text-muted-foreground text-xs leading-relaxed'>
        {t(
          'While the latest-turn switch is on, wordlists inspect only the latest turn and the newest tool round in every mode, and model audit does so only in blocking mode; async model audit reads the whole request. Other enabled sources (system, developer, task) are always inspected.'
        )}
      </p>
      <p className='text-muted-foreground text-xs leading-relaxed'>
        {t(
          'MCP tool definitions are inspected separately and cached. Other tool definitions, metadata, binary content, and gateway-injected instructions are excluded.'
        )}
      </p>
      <p className='text-muted-foreground text-xs leading-relaxed'>
        {t(
          'Clients can forge agent context and skill labels. Disabling inspection for these sources can allow disguised content to bypass your rules.'
        )}
      </p>
    </SettingsSection>
  )
}
