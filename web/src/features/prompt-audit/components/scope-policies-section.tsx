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
import { Switch } from '@/components/ui/switch'
import { SettingsSwitchField } from '@/features/system-settings/components/settings-form-layout'
import { SettingsSection } from '@/features/system-settings/components/settings-section'

import { PROMPT_AUDIT_SCOPES, promptAuditScopeLabel } from '../scopes'
import type { PromptScopePolicies, PromptWordlist } from '../types'

interface ScopePoliciesSectionProps {
  policies: PromptScopePolicies
  libraries: PromptWordlist[]
  wordFilterEnabled: boolean
  onChange: (policies: PromptScopePolicies) => void
  onWordFilterChange: (enabled: boolean) => void
}

export function ScopePoliciesSection(props: ScopePoliciesSectionProps) {
  const { t } = useTranslation()
  const options = props.libraries.map((library) => ({
    value: library.id,
    label:
      (library.id === 'manual' ? t('Custom wordlist') : library.name) +
      (library.enabled ? '' : ' (' + t('Disabled') + ')'),
  }))
  return (
    <SettingsSection title={t('Inspection rules by source')}>
      <p className='text-muted-foreground text-xs leading-relaxed'>
        {t(
          'Choose wordlists and model review separately for each part of a request. Disabled wordlists keep their assignments.'
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
              className='grid items-center gap-3 py-3 md:grid-cols-[13rem_1fr_6rem]'
            >
              <div className='space-y-0.5'>
                <Label
                  htmlFor={`scope-libraries-${scope}`}
                  className='cursor-pointer text-sm font-medium'
                >
                  {label}
                </Label>
              </div>
              <div>
                <MultiSelect
                  id={`scope-libraries-${scope}`}
                  aria-label={label}
                  options={options}
                  selected={policy.library_ids}
                  maxVisibleChips={3}
                  placeholder={t('No wordlists selected')}
                  onChange={(library_ids) =>
                    props.onChange({
                      ...props.policies,
                      [scope]: { ...policy, library_ids },
                    })
                  }
                />
              </div>
              <div className='flex items-center justify-between gap-2 md:justify-end'>
                <Label
                  htmlFor={`scope-model-${scope}`}
                  className='text-muted-foreground text-xs md:sr-only'
                >
                  {t('Model audit')}
                  <span className='sr-only'>: {label}</span>
                </Label>
                <Switch
                  id={`scope-model-${scope}`}
                  checked={policy.model_audit}
                  onCheckedChange={(model_audit) =>
                    props.onChange({
                      ...props.policies,
                      [scope]: { ...policy, model_audit },
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
          'Wordlists inspect the latest user and preceding assistant turns, plus other assigned sources. Model audit uses its configured conversation scope.'
        )}
      </p>
      <p className='text-muted-foreground text-xs leading-relaxed'>
        {t(
          'Tool definitions, metadata, binary content, and gateway-injected instructions are excluded.'
        )}
      </p>
    </SettingsSection>
  )
}
