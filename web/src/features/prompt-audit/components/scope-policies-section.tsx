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
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

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
    <Card>
      <CardHeader>
        <CardTitle>{t('Inspection rules by source')}</CardTitle>
        <CardDescription>
          {t(
            'Choose wordlists and model review separately for each part of a request. Disabled wordlists keep their assignments.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-5'>
        <div className='flex items-center justify-between gap-4'>
          <Label htmlFor='word-filter-enabled'>
            {t('Enable wordlist filtering')}
          </Label>
          <Switch
            id='word-filter-enabled'
            checked={props.wordFilterEnabled}
            onCheckedChange={props.onWordFilterChange}
          />
        </div>
        {PROMPT_AUDIT_SCOPES.map((scope) => {
          const label = promptAuditScopeLabel(scope, t)
          const policy = props.policies[scope]
          return (
            <div
              key={scope}
              className='grid gap-3 border-t pt-4 md:grid-cols-[12rem_1fr_auto] md:items-center'
            >
              <Label htmlFor={`scope-libraries-${scope}`}>{label}</Label>
              <MultiSelect
                id={`scope-libraries-${scope}`}
                aria-label={label}
                options={options}
                selected={policy.library_ids}
                maxVisibleChips={4}
                placeholder={t('No wordlists selected')}
                onChange={(library_ids) =>
                  props.onChange({
                    ...props.policies,
                    [scope]: { ...policy, library_ids },
                  })
                }
              />
              <div className='flex items-center gap-2'>
                <Label htmlFor={`scope-model-${scope}`}>
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
        <p className='text-muted-foreground text-xs'>
          {t(
            'Tool definitions, metadata, binary content, and gateway-injected instructions are excluded.'
          )}
        </p>
      </CardContent>
    </Card>
  )
}
