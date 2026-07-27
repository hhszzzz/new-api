/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ClientMultiSelect } from '@/components/client-multi-select'
import { MultiSelect, type Option } from '@/components/multi-select'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { getGroups } from '@/features/users/api'
import {
  CLIENT_MATCH_MODE_LABEL_KEYS,
  CLIENT_MATCH_SOURCE_LABEL_KEYS,
  CLIENT_POLICY_MODE_LABEL_KEYS,
  type ClientMatchMode,
  type ClientMatchSource,
  type ClientPolicyMode,
} from '@/lib/client-policy'

import { updateClientPolicyOptions } from '../api'
import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'

type MatchConfig = {
  source: ClientMatchSource
  header?: string
  mode: ClientMatchMode
  value: string
}

type Match = MatchConfig & { draftId: number }
type Rule = { draftId: number; name: string; matches: Match[] }
type StoredRule = { name: string; matches: MatchConfig[] }
type Policy = { mode: ClientPolicyMode; clients: string[] }
type Props = {
  defaultValues: {
    'client_policy_setting.rules': string
    'client_policy_setting.group_policies': string
  }
}

const SAFE_HEADER_OPTIONS = [
  { label: 'Anthropic-Beta', value: 'anthropic-beta' },
  { label: 'Anthropic-Version', value: 'anthropic-version' },
  { label: 'Originator', value: 'originator' },
  { label: 'X-App', value: 'x-app' },
  { label: 'X-Codex-Beta-Features', value: 'x-codex-beta-features' },
  { label: 'X-Client-Request-Id', value: 'x-client-request-id' },
  { label: 'X-Stainless-Lang', value: 'x-stainless-lang' },
  { label: 'X-Stainless-Runtime', value: 'x-stainless-runtime' },
]

type RuleTemplate = {
  id: string
  label: string
  name: string
  match: MatchConfig
}

let nextDraftId = 0

function createDraftId(): number {
  nextDraftId += 1
  return nextDraftId
}

const RULE_TEMPLATES: RuleTemplate[] = [
  {
    id: 'stainless-python',
    label: 'Python',
    name: 'python',
    match: {
      source: 'header',
      header: 'x-stainless-lang',
      mode: 'exact',
      value: 'python',
    },
  },
  {
    id: 'stainless-javascript',
    label: 'JavaScript',
    name: 'javascript',
    match: {
      source: 'header',
      header: 'x-stainless-lang',
      mode: 'exact',
      value: 'js',
    },
  },
  {
    id: 'stainless-go',
    label: 'Go',
    name: 'go',
    match: {
      source: 'header',
      header: 'x-stainless-lang',
      mode: 'exact',
      value: 'go',
    },
  },
  {
    id: 'stainless-java',
    label: 'Java',
    name: 'java',
    match: {
      source: 'header',
      header: 'x-stainless-lang',
      mode: 'exact',
      value: 'java',
    },
  },
  {
    id: 'stainless-ruby',
    label: 'Ruby',
    name: 'ruby',
    match: {
      source: 'header',
      header: 'x-stainless-lang',
      mode: 'exact',
      value: 'ruby',
    },
  },
  {
    id: 'stainless-php',
    label: 'PHP',
    name: 'php',
    match: {
      source: 'header',
      header: 'x-stainless-lang',
      mode: 'exact',
      value: 'php',
    },
  },
  {
    id: 'curl',
    label: 'curl',
    name: 'curl',
    match: {
      source: 'user_agent',
      mode: 'prefix',
      value: 'curl/',
    },
  },
  {
    id: 'postman',
    label: 'Postman',
    name: 'postman',
    match: {
      source: 'user_agent',
      mode: 'prefix',
      value: 'PostmanRuntime/',
    },
  },
  {
    id: 'httpie',
    label: 'HTTPie',
    name: 'httpie',
    match: {
      source: 'user_agent',
      mode: 'prefix',
      value: 'HTTPie/',
    },
  },
  {
    id: 'insomnia',
    label: 'Insomnia',
    name: 'insomnia',
    match: {
      source: 'user_agent',
      mode: 'prefix',
      value: 'insomnia/',
    },
  },
]

const RULE_VALUE_PREFIX = 'rule:'
const PRESET_VALUE_PREFIX = 'preset:'

function parseRules(raw: string): Rule[] {
  try {
    const value: unknown = JSON.parse(raw)
    if (!Array.isArray(value)) return []
    return value
      .filter((item): item is StoredRule => {
        if (!item || typeof item !== 'object') return false
        const candidate = item as { name?: unknown; matches?: unknown }
        return (
          typeof candidate.name === 'string' && Array.isArray(candidate.matches)
        )
      })
      .map((rule) => ({
        draftId: createDraftId(),
        name: rule.name.trim(),
        matches: rule.matches
          .filter((item): item is MatchConfig => {
            if (!item || typeof item !== 'object') return false
            const candidate = item as Partial<Match>
            return (
              (candidate.source === 'path' ||
                candidate.source === 'user_agent' ||
                candidate.source === 'header') &&
              (candidate.mode === 'exact' || candidate.mode === 'prefix') &&
              typeof candidate.value === 'string'
            )
          })
          .map((match) => ({
            ...match,
            draftId: createDraftId(),
            header: match.header?.trim().toLowerCase(),
          })),
      }))
  } catch {
    return []
  }
}

function parsePolicies(raw: string): Record<string, Policy> {
  try {
    const value: unknown = JSON.parse(raw)
    if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
    const result: Record<string, Policy> = {}
    for (const [group, policy] of Object.entries(value)) {
      if (!policy || typeof policy !== 'object') continue
      const candidate = policy as { mode?: unknown; clients?: unknown }
      const mode =
        candidate.mode === 'allow' || candidate.mode === 'deny'
          ? candidate.mode
          : 'unrestricted'
      const clients = Array.isArray(candidate.clients)
        ? candidate.clients
            .filter((client): client is string => typeof client === 'string')
            .map((client) => client.trim().toLowerCase())
            .filter(Boolean)
        : []
      result[group] = { mode, clients }
    }
    return result
  } catch {
    return {}
  }
}

function uniqueRuleName(base: string, rules: Rule[]): string {
  const normalizedBase = base.trim().toLowerCase() || 'custom_client'
  const names = new Set(rules.map((rule) => rule.name.trim().toLowerCase()))
  if (!names.has(normalizedBase)) return normalizedBase
  let suffix = 2
  while (names.has(`${normalizedBase}_${suffix}`)) suffix += 1
  return `${normalizedBase}_${suffix}`
}

export function ClientPolicySettingsSection(props: Props) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const editRevisionRef = useRef(0)
  const synchronizedRevisionRef = useRef(0)
  const submittedRevisionRef = useRef(0)
  const clientPolicyMutation = useMutation({
    mutationFn: updateClientPolicyOptions,
    onSuccess: (response) => {
      if (!response.success) {
        toast.error(response.message || t('Failed to update setting'))
        return
      }
      if (editRevisionRef.current === submittedRevisionRef.current) {
        synchronizedRevisionRef.current = submittedRevisionRef.current
      }
      queryClient.invalidateQueries({ queryKey: ['system-options'] })
      toast.success(t('Setting updated successfully'))
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to update setting'))
    },
  })
  const rawRules = props.defaultValues['client_policy_setting.rules']
  const rawPolicies =
    props.defaultValues['client_policy_setting.group_policies']
  const parsedRules = useMemo(() => parseRules(rawRules), [rawRules])
  const parsedPolicies = useMemo(
    () => parsePolicies(rawPolicies),
    [rawPolicies]
  )
  const [rules, setRules] = useState<Rule[]>(parsedRules)
  const [policies, setPolicies] =
    useState<Record<string, Policy>>(parsedPolicies)
  const groupsQuery = useQuery({ queryKey: ['groups'], queryFn: getGroups })

  useEffect(() => {
    if (editRevisionRef.current !== synchronizedRevisionRef.current) return
    setRules(parsedRules)
    setPolicies(parsedPolicies)
  }, [parsedPolicies, parsedRules])

  const editRules: typeof setRules = (updater) => {
    editRevisionRef.current += 1
    setRules(updater)
  }
  const editPolicies: typeof setPolicies = (updater) => {
    editRevisionRef.current += 1
    setPolicies(updater)
  }

  const availableGroups = useMemo(() => {
    const configuredGroups = groupsQuery.data?.data ?? []
    return [...new Set([...configuredGroups, ...Object.keys(policies)])]
      .filter(Boolean)
      .sort()
  }, [groupsQuery.data?.data, policies])
  const availableClients = useMemo(
    () => rules.map((rule) => rule.name).filter(Boolean),
    [rules]
  )

  const updateRule = (ruleIndex: number, patch: Partial<Rule>) => {
    editRules((current) =>
      current.map((rule, index) =>
        index === ruleIndex ? { ...rule, ...patch } : rule
      )
    )
  }

  const updateMatch = (
    ruleIndex: number,
    matchIndex: number,
    patch: Partial<Match>
  ) => {
    editRules((current) =>
      current.map((rule, index) => {
        if (index !== ruleIndex) return rule
        return {
          ...rule,
          matches: rule.matches.map((match, itemIndex) =>
            itemIndex === matchIndex ? { ...match, ...patch } : match
          ),
        }
      })
    )
  }

  const selectedRuleValues = rules.map(
    (rule) => `${RULE_VALUE_PREFIX}${rule.draftId}`
  )
  const rulePickerOptions = useMemo<Option[]>(
    () => [
      ...RULE_TEMPLATES.map((template) => ({
        label: template.label,
        value: `${PRESET_VALUE_PREFIX}${template.id}`,
      })),
      ...rules.map((rule) => ({
        label: rule.name || t('Client name'),
        value: `${RULE_VALUE_PREFIX}${rule.draftId}`,
      })),
    ],
    [rules, t]
  )

  const updateSelectedRules = (values: string[]) => {
    editRules((current) => {
      const currentByValue = new Map(
        current.map((rule) => [`${RULE_VALUE_PREFIX}${rule.draftId}`, rule])
      )
      const selectedValues = new Set(values)
      const next = current.filter((rule) =>
        selectedValues.has(`${RULE_VALUE_PREFIX}${rule.draftId}`)
      )

      for (const value of values) {
        if (currentByValue.has(value)) continue

        const template = value.startsWith(PRESET_VALUE_PREFIX)
          ? RULE_TEMPLATES.find(
              (item) => item.id === value.slice(PRESET_VALUE_PREFIX.length)
            )
          : undefined
        if (template) {
          next.push({
            draftId: createDraftId(),
            name: uniqueRuleName(template.name, next),
            matches: [{ ...template.match, draftId: createDraftId() }],
          })
          continue
        }

        const name = uniqueRuleName(value, next)
        next.push({
          draftId: createDraftId(),
          name,
          matches: [
            {
              draftId: createDraftId(),
              source: 'user_agent',
              mode: 'prefix',
              value: name,
            },
          ],
        })
      }

      return next
    })
  }

  const onSubmit = () => {
    const hasIncompleteRule = rules.some(
      (rule) =>
        !rule.name.trim() ||
        rule.matches.length === 0 ||
        rule.matches.some(
          (match) =>
            !match.value.trim() ||
            (match.source === 'header' && !match.header?.trim())
        )
    )
    if (hasIncompleteRule) {
      toast.error(t('Complete every client rule match before saving.'))
      return
    }

    const cleanRules = rules.map((rule) => ({
      name: rule.name.trim(),
      matches: rule.matches.map((match) => ({
        source: match.source,
        header: match.header?.trim().toLowerCase(),
        mode: match.mode,
        value: match.value.trim(),
      })),
    }))
    const cleanPolicies: Record<string, Policy> = {}
    for (const [group, policy] of Object.entries(policies)) {
      const normalizedGroup = group.trim()
      if (!normalizedGroup || policy.mode === 'unrestricted') continue
      cleanPolicies[normalizedGroup] = {
        mode: policy.mode,
        clients: [
          ...new Set(
            policy.clients
              .map((client) => client.trim().toLowerCase())
              .filter(Boolean)
          ),
        ],
      }
    }
    if (clientPolicyMutation.isPending) return
    submittedRevisionRef.current = editRevisionRef.current
    clientPolicyMutation.mutate({
      rules: cleanRules,
      group_policies: cleanPolicies,
    })
  }

  return (
    <SettingsSection title={t('Client Policies')}>
      <Alert>
        <AlertDescription>
          {t(
            'Client detection is a spoofable policy signal, not authentication. With no policy configured, existing behavior is unchanged.'
          )}
        </AlertDescription>
      </Alert>
      <SettingsForm
        onSubmit={(event) => {
          event.preventDefault()
          onSubmit()
        }}
      >
        <SettingsPageFormActions
          onSave={onSubmit}
          isSaving={clientPolicyMutation.isPending}
        />
        <div className='space-y-4 lg:col-span-2'>
          <div className='space-y-2'>
            <div>
              <div className='text-sm font-medium'>
                {t('Custom client identification rules')}
              </div>
              <div className='text-muted-foreground text-xs'>
                {t(
                  'All matches in a rule must match. Use exact or prefix matching on a path, User-Agent, or safe header.'
                )}
              </div>
            </div>
            <MultiSelect
              options={rulePickerOptions}
              selected={selectedRuleValues}
              onChange={updateSelectedRules}
              placeholder={t('Select clients or add custom names')}
              allowCreate
              createLabel='Add custom client "{{value}}"'
              maxVisibleChips={8}
            />
          </div>
          {rules.map((rule, ruleIndex) => (
            <div key={rule.draftId} className='space-y-3 rounded-lg border p-3'>
              <div className='flex gap-2'>
                <Input
                  value={rule.name}
                  placeholder={t('Client name')}
                  onChange={(event) =>
                    updateRule(ruleIndex, { name: event.target.value })
                  }
                />
                <Button
                  type='button'
                  variant='ghost'
                  onClick={() =>
                    editRules((current) =>
                      current.filter((_, index) => index !== ruleIndex)
                    )
                  }
                >
                  {t('Remove')}
                </Button>
              </div>
              {rule.matches.map((match, matchIndex) => (
                <div
                  key={match.draftId}
                  className='grid gap-2 md:grid-cols-[10rem_8rem_minmax(0,1fr)_auto]'
                >
                  <Select
                    value={match.source}
                    onValueChange={(value) =>
                      updateMatch(ruleIndex, matchIndex, {
                        source: value as Match['source'],
                      })
                    }
                  >
                    <SelectTrigger>
                      <SelectValue>
                        {t(CLIENT_MATCH_SOURCE_LABEL_KEYS[match.source])}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value='path'>{t('Path')}</SelectItem>
                        <SelectItem value='user_agent'>
                          {t('User-Agent')}
                        </SelectItem>
                        <SelectItem value='header'>
                          {t('Safe header')}
                        </SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <Select
                    value={match.mode}
                    onValueChange={(value) =>
                      updateMatch(ruleIndex, matchIndex, {
                        mode: value as Match['mode'],
                      })
                    }
                  >
                    <SelectTrigger>
                      <SelectValue>
                        {t(CLIENT_MATCH_MODE_LABEL_KEYS[match.mode])}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value='prefix'>{t('Prefix')}</SelectItem>
                        <SelectItem value='exact'>{t('Exact')}</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  {match.source === 'header' ? (
                    <div className='flex gap-2'>
                      <Combobox
                        options={SAFE_HEADER_OPTIONS}
                        value={match.header ?? ''}
                        onValueChange={(value) =>
                          updateMatch(ruleIndex, matchIndex, {
                            header: value ?? '',
                          })
                        }
                        placeholder={t('Header name')}
                        searchPlaceholder={t('Header name')}
                        emptyText={t('No matching items')}
                        allowCustomValue
                      />
                      <Input
                        value={match.value}
                        placeholder={t('Value')}
                        onChange={(event) =>
                          updateMatch(ruleIndex, matchIndex, {
                            value: event.target.value,
                          })
                        }
                      />
                    </div>
                  ) : (
                    <Input
                      value={match.value}
                      placeholder={t('Match value')}
                      onChange={(event) =>
                        updateMatch(ruleIndex, matchIndex, {
                          value: event.target.value,
                        })
                      }
                    />
                  )}
                  <Button
                    type='button'
                    variant='ghost'
                    onClick={() =>
                      updateRule(ruleIndex, {
                        matches: rule.matches.filter(
                          (_, index) => index !== matchIndex
                        ),
                      })
                    }
                  >
                    {t('Remove')}
                  </Button>
                </div>
              ))}
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={() =>
                  updateRule(ruleIndex, {
                    matches: [
                      ...rule.matches,
                      {
                        draftId: createDraftId(),
                        source: 'user_agent',
                        mode: 'prefix',
                        value: '',
                      },
                    ],
                  })
                }
              >
                {t('Add match')}
              </Button>
            </div>
          ))}
        </div>
        <div className='space-y-4 lg:col-span-2'>
          <div>
            <div className='text-sm font-medium'>
              {t('Group client policies')}
            </div>
            <div className='text-muted-foreground text-xs'>
              {t(
                'Allow lists reject unknown clients; deny lists allow unknown clients. Policies apply to both requested and routed execution groups.'
              )}
            </div>
          </div>
          <div className='space-y-2'>
            {availableGroups.map((group) => {
              const policy = policies[group] ?? {
                mode: 'unrestricted' as const,
                clients: [],
              }
              return (
                <div
                  key={group}
                  className='grid gap-2 rounded-lg border p-3 md:grid-cols-[minmax(0,1fr)_12rem_minmax(0,2fr)] md:items-center'
                >
                  <div className='font-medium'>{group}</div>
                  <Select
                    value={policy.mode}
                    onValueChange={(value) =>
                      editPolicies((current) => ({
                        ...current,
                        [group]: { ...policy, mode: value as Policy['mode'] },
                      }))
                    }
                  >
                    <SelectTrigger>
                      <SelectValue>
                        {t(CLIENT_POLICY_MODE_LABEL_KEYS[policy.mode])}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value='unrestricted'>
                          {t('Unrestricted')}
                        </SelectItem>
                        <SelectItem value='allow'>{t('Allow only')}</SelectItem>
                        <SelectItem value='deny'>{t('Deny')}</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <ClientMultiSelect
                    id={`group-client-policy-${group}`}
                    selected={policy.clients}
                    availableClients={availableClients}
                    disabled={policy.mode === 'unrestricted'}
                    onChange={(clients) =>
                      editPolicies((current) => ({
                        ...current,
                        [group]: { ...policy, clients },
                      }))
                    }
                  />
                </div>
              )
            })}
          </div>
        </div>
      </SettingsForm>
    </SettingsSection>
  )
}
