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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  CirclePlus,
  Cpu,
  FlaskConical,
  HardDrive,
  Save,
  ShieldAlert,
  Timer,
  Trash2,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { getGroups } from '@/features/users/api'
import { cn } from '@/lib/utils'

import {
  getPromptAuditCategories,
  getPromptAuditConfig,
  listPromptWordlists,
  testPromptAuditNode,
  updatePromptAuditConfig,
} from './api'
import { PromptAuditNavigation } from './components/prompt-audit-navigation'
import { ScopePoliciesSection } from './components/scope-policies-section'
import {
  promptAuditEndpointBaseURLUpdate,
  promptAuditEndpointDrafts,
  promptAuditEndpointUpdate,
  type PromptAuditEndpointDraft,
  validatePromptAuditConfig,
} from './lib'
import { defaultPromptScopePolicies } from './scopes'
import type {
  PromptAuditCategory,
  PromptAuditConfig,
  PromptAuditConfigUpdate,
} from './types'

type NumberFieldProps = {
  id: string
  label: string
  value: number
  min: number
  max: number
  description: string
  onChange: (value: number) => void
}

const MB = 1024 * 1024

function bytesToMB(bytes: number): number {
  return Math.max(1, Math.round(bytes / MB))
}

function mbToBytes(mb: number): number {
  return Math.max(MB, Math.round(mb * MB))
}

function NumberField({
  id,
  label,
  value,
  min,
  max,
  description,
  onChange,
}: NumberFieldProps) {
  return (
    <div className='bg-muted/20 hover:bg-muted/30 flex flex-col justify-between space-y-1.5 rounded-xl border p-3.5 transition-colors'>
      <div className='space-y-1'>
        <Label
          htmlFor={id}
          className='text-foreground cursor-pointer text-xs font-medium'
        >
          {label}
        </Label>
        <Input
          id={id}
          type='number'
          min={min}
          max={max}
          value={value}
          className='bg-background h-9 font-mono text-sm'
          onChange={(event) => onChange(Number(event.target.value || '0'))}
        />
      </div>
      <p className='text-muted-foreground text-[11px] leading-relaxed'>
        {description}
      </p>
    </div>
  )
}

type PromptAuditSettingsFormProps = {
  initialConfig: PromptAuditConfig
  categories: PromptAuditCategory[]
  availableGroups: string[]
}

function promptAuditConfigDraft(
  config: PromptAuditConfig
): PromptAuditConfigUpdate {
  return {
    scope_policies: config.scope_policies ?? defaultPromptScopePolicies(),
    word_filter_enabled: config.word_filter_enabled ?? true,
    mode: config.mode,
    output_mode: config.output_mode ?? 'off',
    blocking_latest_turn_only: config.blocking_latest_turn_only ?? true,
    manual_wordlist_action: config.manual_wordlist_action ?? 'block',
    enabled_categories: [...config.enabled_categories],
    controversial_block_categories: [
      ...(config.controversial_block_categories ?? []),
    ],
    review_enabled: config.review_enabled ?? false,
    review_prompt: config.review_prompt ?? '',
    all_groups: config.all_groups,
    groups: [...config.groups],
    total_timeout_ms: config.total_timeout_ms,
    chunk_overlap: config.chunk_overlap,
    chunk_concurrency: config.chunk_concurrency ?? 4,
    cache_ttl_seconds: config.cache_ttl_seconds,
    worker_count: config.worker_count,
    max_attempts: config.max_attempts,
    retention_days: config.retention_days,
    global_concurrency: config.global_concurrency,
    endpoint_concurrency: config.endpoint_concurrency,
    output_max_bytes: config.output_max_bytes ?? 8 * 1024 * 1024,
    output_memory_bytes: config.output_memory_bytes ?? 1024 * 1024,
    endpoints: [],
  }
}

function PromptAuditSettingsForm({
  initialConfig,
  categories,
  availableGroups,
}: PromptAuditSettingsFormProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const librariesQuery = useQuery({
    queryKey: ['prompt-audit', 'wordlists'],
    queryFn: listPromptWordlists,
  })
  const [config, setConfig] = useState<PromptAuditConfigUpdate>(() =>
    promptAuditConfigDraft(initialConfig)
  )
  const [endpoints, setEndpoints] = useState<PromptAuditEndpointDraft[]>(() =>
    promptAuditEndpointDrafts(initialConfig.endpoints)
  )
  const persistedEndpointIDs = new Set(
    initialConfig.endpoints.map((model) => model.id)
  )

  const saveMutation = useMutation({
    mutationFn: async () => {
      const payload = {
        ...config,
        groups: [
          ...new Set(
            config.groups.map((group) => group.trim()).filter(Boolean)
          ),
        ],
        endpoints: endpoints.map(promptAuditEndpointUpdate),
      }
      const validationError = validatePromptAuditConfig(payload)
      if (validationError) throw new Error(t(validationError))
      const result = await updatePromptAuditConfig(payload)
      if (!result.success || !result.data) {
        throw new Error(
          result.message || t('Failed to save prompt audit settings')
        )
      }
      return result
    },
    onSuccess: (result) => {
      queryClient.setQueryData(['prompt-audit', 'config'], result)
      void queryClient.invalidateQueries({ queryKey: ['prompt-audit'] })
      if (result.data) {
        setConfig(promptAuditConfigDraft(result.data))
        setEndpoints(promptAuditEndpointDrafts(result.data.endpoints))
      }
      toast.success(t('Prompt audit settings saved'))
    },
    onError: (error) => toast.error(error.message),
  })
  const testMutation = useMutation({
    mutationFn: async (id: string) => {
      const result = await testPromptAuditNode(id)
      if (!result.success || !result.data) {
        if (result.data?.error_code === 'output_capability_unverified') {
          throw new Error(
            t(
              'Output audit test failed. Check that this model evaluates assistant replies and handles refusals correctly.'
            )
          )
        }
        if (result.data?.error_code === 'input_capability_unverified') {
          throw new Error(t('Input audit test failed on a harmless sample.'))
        }
        throw new Error(result.message || t('Audit model test failed'))
      }
      return result.data
    },
    onSuccess: (result) => {
      const directionLabels = {
        input: t('Request input'),
        output: t('Generated output'),
        review: t('Gray-area review'),
      }
      toast.success(
        result.tested_directions?.length
          ? t('Audit model passed {{directions}} tests in {{latency}} ms', {
              latency: result.latency_ms,
              directions: result.tested_directions
                .map((direction) => directionLabels[direction])
                .join(', '),
            })
          : t('Audit model responded in {{latency}} ms with {{safety}}', {
              latency: result.latency_ms,
              safety: result.safety || result.decision,
            })
      )
    },
    onError: (error) => toast.error(error.message),
  })

  const updateEndpoint = (
    index: number,
    update: Partial<PromptAuditEndpointDraft>
  ) => {
    setEndpoints((current) =>
      current.map((endpoint, endpointIndex) =>
        endpointIndex === index ? { ...endpoint, ...update } : endpoint
      )
    )
  }

  const groups = [...new Set([...availableGroups, ...config.groups])].sort()

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
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Prompt audit')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          disabled={
            saveMutation.isPending ||
            librariesQuery.isPending ||
            librariesQuery.isError
          }
          onClick={() => saveMutation.mutate()}
        >
          <Save className='size-4' />
          {saveMutation.isPending ? t('Saving...') : t('Save settings')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='w-full space-y-6'>
          <PromptAuditNavigation />

          <div className='grid w-full grid-cols-1 items-start gap-6 xl:grid-cols-12'>
            <div className='space-y-6 xl:col-span-7'>
              {librariesQuery.isPending && <LoadingState />}
              {librariesQuery.isError && (
                <ErrorState
                  description={librariesQuery.error.message}
                  onRetry={() => void librariesQuery.refetch()}
                />
              )}
              {librariesQuery.isSuccess && (
                <ScopePoliciesSection
                  policies={
                    config.scope_policies ?? defaultPromptScopePolicies()
                  }
                  libraries={librariesQuery.data}
                  wordFilterEnabled={config.word_filter_enabled ?? true}
                  onChange={(scope_policies) =>
                    setConfig((current) => ({ ...current, scope_policies }))
                  }
                  onWordFilterChange={(word_filter_enabled) =>
                    setConfig((current) => ({
                      ...current,
                      word_filter_enabled,
                    }))
                  }
                />
              )}

              <Card>
                <CardHeader>
                  <CardTitle>{t('Enforcement policy')}</CardTitle>
                  <CardDescription>
                    {t(
                      'Sensitive-word filtering runs first. Blocking mode fails closed before channel selection, billing, and upstream dispatch.'
                    )}
                  </CardDescription>
                </CardHeader>
                <CardContent className='space-y-5'>
                  <div className='grid gap-4 sm:grid-cols-2'>
                    <div className='bg-muted/20 flex flex-col justify-between space-y-3 rounded-xl border p-4'>
                      <div className='space-y-1.5'>
                        <Label
                          htmlFor='prompt-audit-mode'
                          className='cursor-pointer text-sm font-medium'
                        >
                          {t('Model audit mode')}
                        </Label>
                        <NativeSelect
                          id='prompt-audit-mode'
                          className='w-full'
                          value={config.mode}
                          onChange={(event) =>
                            setConfig((current) => ({
                              ...current,
                              mode: event.target
                                .value as PromptAuditConfigUpdate['mode'],
                            }))
                          }
                        >
                          <NativeSelectOption value='off'>
                            {t('Off')}
                          </NativeSelectOption>
                          <NativeSelectOption value='async_audit'>
                            {t('Async audit')}
                          </NativeSelectOption>
                          <NativeSelectOption value='blocking'>
                            {t('Blocking')}
                          </NativeSelectOption>
                        </NativeSelect>
                      </div>
                      <p className='text-muted-foreground text-xs leading-relaxed'>
                        {modeDescription}
                      </p>
                    </div>

                    <div className='bg-muted/20 flex flex-col justify-between space-y-3 rounded-xl border p-4'>
                      <div className='space-y-1.5'>
                        <Label
                          htmlFor='prompt-audit-output-mode'
                          className='cursor-pointer text-sm font-medium'
                        >
                          {t('Output audit mode')}
                        </Label>
                        <NativeSelect
                          id='prompt-audit-output-mode'
                          className='w-full'
                          value={config.output_mode}
                          onChange={(event) =>
                            setConfig((current) => ({
                              ...current,
                              output_mode: event.target
                                .value as PromptAuditConfigUpdate['output_mode'],
                            }))
                          }
                        >
                          <NativeSelectOption value='off'>
                            {t('Off')}
                          </NativeSelectOption>
                          <NativeSelectOption value='async_audit'>
                            {t('Async observation')}
                          </NativeSelectOption>
                          <NativeSelectOption value='blocking'>
                            {t('Full-buffer blocking')}
                          </NativeSelectOption>
                        </NativeSelect>
                      </div>
                      <p className='text-muted-foreground text-xs leading-relaxed'>
                        {outputModeDescription}
                      </p>
                    </div>

                    <div className='bg-muted/30 flex items-center justify-between gap-4 rounded-xl border p-4'>
                      <div className='space-y-0.5'>
                        <Label
                          htmlFor='prompt-audit-blocking-latest-turn-only'
                          className='cursor-pointer text-sm font-medium'
                        >
                          {t('Blocking scans the latest turn only')}
                        </Label>
                        <p className='text-muted-foreground text-xs'>
                          {t(
                            'In blocking mode, inspect the latest user turn, preceding assistant turn, and current tool activity. Other enabled sources are still inspected. Turn this off to include older conversation turns.'
                          )}
                        </p>
                      </div>
                      <Switch
                        id='prompt-audit-blocking-latest-turn-only'
                        checked={config.blocking_latest_turn_only}
                        onCheckedChange={(latestTurnOnly) =>
                          setConfig((current) => ({
                            ...current,
                            blocking_latest_turn_only: latestTurnOnly,
                          }))
                        }
                      />
                    </div>
                  </div>

                  <div className='bg-muted/30 flex items-center justify-between gap-4 rounded-xl border p-4'>
                    <div className='space-y-0.5'>
                      <Label
                        htmlFor='prompt-audit-all-groups'
                        className='cursor-pointer text-sm font-medium'
                      >
                        {t('All groups')}
                      </Label>
                      <p className='text-muted-foreground text-xs'>
                        {t(
                          'Apply the policy to every request group, including newly added groups.'
                        )}
                      </p>
                    </div>
                    <Switch
                      id='prompt-audit-all-groups'
                      checked={config.all_groups}
                      onCheckedChange={(allGroups) =>
                        setConfig((current) => ({
                          ...current,
                          all_groups: allGroups,
                        }))
                      }
                    />
                  </div>

                  {!config.all_groups && (
                    <div className='bg-background space-y-2.5 rounded-xl border p-4'>
                      <Label className='text-sm font-medium'>
                        {t('Audited groups')}
                      </Label>
                      <div className='grid gap-2 sm:grid-cols-2 lg:grid-cols-3'>
                        {groups.map((group) => {
                          const selected = config.groups.includes(group)
                          return (
                            <label
                              key={group}
                              className={cn(
                                'flex items-center gap-2.5 rounded-lg border p-3 text-sm cursor-pointer transition-colors',
                                selected
                                  ? 'border-primary/50 bg-primary/5 font-medium'
                                  : 'hover:bg-muted/40'
                              )}
                            >
                              <Checkbox
                                checked={selected}
                                onCheckedChange={(checked) =>
                                  setConfig((current) => ({
                                    ...current,
                                    groups: checked
                                      ? [...current.groups, group]
                                      : current.groups.filter(
                                          (value) => value !== group
                                        ),
                                  }))
                                }
                              />
                              <span className='break-all'>{group}</span>
                            </label>
                          )
                        })}
                        {groups.length === 0 && (
                          <p className='text-muted-foreground col-span-full text-sm'>
                            {t('No request groups are available.')}
                          </p>
                        )}
                      </div>
                    </div>
                  )}
                </CardContent>
              </Card>

              {/*
                Gray-area review is intentionally hidden from the UI. The backend
                capability remains intact (review_enabled stays false unless it
                was already enabled in persisted config).
              */}
              <Collapsible className='bg-card text-card-foreground rounded-xl border shadow-2xs'>
                <CollapsibleTrigger className='group focus-visible:ring-ring/50 hover:bg-muted/30 flex w-full items-center justify-between gap-3 rounded-xl px-5 py-4 text-left transition-colors outline-none focus-visible:ring-3'>
                  <div className='flex items-center gap-3'>
                    <div className='bg-primary/10 text-primary flex size-8 shrink-0 items-center justify-center rounded-lg'>
                      <ShieldAlert className='size-4' />
                    </div>
                    <div className='flex items-center gap-2'>
                      <span className='text-foreground text-sm font-semibold'>
                        {t('Advanced: risk categories')}
                      </span>
                      <Badge
                        variant='secondary'
                        className='text-xs font-normal'
                      >
                        {config.enabled_categories.length} / {categories.length}
                      </Badge>
                    </div>
                  </div>
                  <ChevronDown
                    className='text-muted-foreground size-4 shrink-0 transition-transform duration-200 group-aria-expanded:rotate-180'
                    aria-hidden='true'
                  />
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <div className='space-y-4 border-t px-5 pt-4 pb-5'>
                    <p className='text-muted-foreground text-xs leading-relaxed'>
                      {t(
                        'Qwen3Guard always returns its complete classification; these selections only control the blocking policy.'
                      )}
                    </p>
                    <div className='grid gap-3 sm:grid-cols-2'>
                      {categories.map((category) => {
                        const isChecked = config.enabled_categories.includes(
                          category.id
                        )
                        return (
                          <label
                            key={category.id}
                            className={cn(
                              'flex items-start gap-3 rounded-lg border p-3 cursor-pointer transition-colors',
                              isChecked
                                ? 'border-primary/40 bg-primary/5 shadow-2xs'
                                : 'hover:bg-muted/40'
                            )}
                          >
                            <Checkbox
                              className='mt-0.5'
                              checked={isChecked}
                              onCheckedChange={(checked) =>
                                setConfig((current) => ({
                                  ...current,
                                  enabled_categories: checked
                                    ? [
                                        ...current.enabled_categories,
                                        category.id,
                                      ]
                                    : current.enabled_categories.filter(
                                        (value) => value !== category.id
                                      ),
                                }))
                              }
                            />
                            <span>
                              <span className='block text-sm font-medium'>
                                {t(category.label)}
                              </span>
                              <span className='text-muted-foreground mt-1 block text-xs leading-normal'>
                                {t(category.description)}
                              </span>
                            </span>
                          </label>
                        )
                      })}
                    </div>
                  </div>
                </CollapsibleContent>
              </Collapsible>
            </div>

            <div className='space-y-6 xl:col-span-5'>
              <Card>
                <CardHeader>
                  <CardTitle id='audit-models' className='scroll-mt-24'>
                    {t('Audit models')}
                  </CardTitle>
                  <CardDescription>
                    {t(
                      'Add an OpenAI-compatible chat model used to review content, for example a Qwen3Guard deployment. Models are tried in order; tokens are write-only, so leave the field untouched to preserve the saved token. Save first, then test.'
                    )}
                  </CardDescription>
                </CardHeader>
                <CardContent className='space-y-4'>
                  {endpoints.map((endpoint, index) => {
                    let tokenDescription = t(
                      'No token is stored for this audit model.'
                    )
                    if (endpoint.token_changed) {
                      tokenDescription = endpoint.token
                        ? t('A replacement token will be saved.')
                        : t('The saved token will be cleared.')
                    } else if (endpoint.has_token) {
                      tokenDescription = t(
                        'A token is stored and will not be returned by the API.'
                      )
                    }

                    let testLabel = t('Save before testing')
                    if (
                      testMutation.isPending &&
                      testMutation.variables === endpoint.id
                    ) {
                      testLabel = t('Testing...')
                    } else if (persistedEndpointIDs.has(endpoint.id)) {
                      testLabel = t('Test saved audit model')
                    }

                    return (
                      <div
                        key={endpoint.client_key}
                        className='bg-card text-card-foreground overflow-hidden rounded-xl border shadow-2xs'
                      >
                        <div className='bg-muted/40 flex items-center justify-between gap-3 border-b px-4 py-2.5'>
                          <div className='flex min-w-0 items-center gap-2.5'>
                            <Badge
                              variant='outline'
                              className='shrink-0 font-mono text-xs'
                            >
                              #{index + 1}
                            </Badge>
                            <Switch
                              aria-label={`${t('Enabled')}: ${endpoint.model || t('New audit model')}`}
                              checked={endpoint.enabled}
                              onCheckedChange={(enabled) =>
                                updateEndpoint(index, { enabled })
                              }
                            />
                            <span className='truncate text-sm font-semibold'>
                              {endpoint.model || t('New audit model')}
                            </span>
                            <Badge
                              variant={
                                endpoint.enabled ? 'default' : 'secondary'
                              }
                              className='shrink-0 text-xs font-normal'
                            >
                              {endpoint.enabled ? t('Enabled') : t('Disabled')}
                            </Badge>
                          </div>
                          <div className='flex shrink-0 items-center gap-0.5'>
                            <Button
                              variant='ghost'
                              size='icon-sm'
                              aria-label={t('Move audit model up')}
                              disabled={index === 0}
                              onClick={() =>
                                setEndpoints((current) => {
                                  const next = [...current]
                                  ;[next[index - 1], next[index]] = [
                                    next[index],
                                    next[index - 1],
                                  ]
                                  return next
                                })
                              }
                            >
                              <ArrowUp className='size-4' />
                            </Button>
                            <Button
                              variant='ghost'
                              size='icon-sm'
                              aria-label={t('Move audit model down')}
                              disabled={index === endpoints.length - 1}
                              onClick={() =>
                                setEndpoints((current) => {
                                  const next = [...current]
                                  ;[next[index], next[index + 1]] = [
                                    next[index + 1],
                                    next[index],
                                  ]
                                  return next
                                })
                              }
                            >
                              <ArrowDown className='size-4' />
                            </Button>
                            <Button
                              variant='ghost'
                              size='icon-sm'
                              className='text-destructive hover:text-destructive hover:bg-destructive/10'
                              aria-label={t('Remove audit model')}
                              onClick={() =>
                                setEndpoints((current) =>
                                  current.filter(
                                    (_, endpointIndex) =>
                                      endpointIndex !== index
                                  )
                                )
                              }
                            >
                              <Trash2 className='size-4' />
                            </Button>
                          </div>
                        </div>

                        <div className='space-y-4 p-4'>
                          <div className='grid gap-3 sm:grid-cols-2'>
                            <div className='space-y-1.5'>
                              <Label
                                htmlFor={`prompt-audit-node-purpose-${index}`}
                              >
                                {t('Audit model purpose')}
                              </Label>
                              <NativeSelect
                                id={`prompt-audit-node-purpose-${index}`}
                                value={endpoint.purpose}
                                onChange={(event) =>
                                  updateEndpoint(index, {
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
                            </div>

                            {endpoint.purpose === 'classify' ? (
                              <div className='space-y-1.5'>
                                <Label>{t('Audit directions')}</Label>
                                <div className='bg-muted/20 flex items-center gap-3 rounded-lg border px-3 py-2'>
                                  {(
                                    [
                                      ['input', t('Request input')],
                                      ['output', t('Generated output')],
                                    ] as const
                                  ).map(([direction, label]) => (
                                    <label
                                      key={direction}
                                      className='flex cursor-pointer items-center gap-1.5 text-xs font-medium'
                                    >
                                      <Checkbox
                                        checked={endpoint.directions.includes(
                                          direction
                                        )}
                                        onCheckedChange={(checked) =>
                                          updateEndpoint(index, {
                                            directions: checked
                                              ? [
                                                  ...endpoint.directions,
                                                  direction,
                                                ]
                                              : endpoint.directions.filter(
                                                  (value) => value !== direction
                                                ),
                                          })
                                        }
                                      />
                                      {label}
                                    </label>
                                  ))}
                                </div>
                              </div>
                            ) : (
                              <div />
                            )}
                          </div>

                          <div className='space-y-1.5'>
                            <Label htmlFor={`prompt-audit-node-url-${index}`}>
                              {t('Base URL')}
                            </Label>
                            <Input
                              id={`prompt-audit-node-url-${index}`}
                              placeholder='https://guard.example.com/v1'
                              value={endpoint.base_url}
                              onChange={(event) =>
                                updateEndpoint(
                                  index,
                                  promptAuditEndpointBaseURLUpdate(
                                    endpoint,
                                    event.target.value
                                  )
                                )
                              }
                            />
                            <p className='text-muted-foreground text-xs'>
                              {t(
                                'Address of an OpenAI-compatible service; /chat/completions is appended automatically.'
                              )}
                            </p>
                          </div>

                          <div className='space-y-1.5'>
                            <Label htmlFor={`prompt-audit-node-model-${index}`}>
                              {t('Model')}
                            </Label>
                            <Input
                              id={`prompt-audit-node-model-${index}`}
                              value={endpoint.model}
                              onChange={(event) =>
                                updateEndpoint(index, {
                                  model: event.target.value,
                                })
                              }
                            />
                          </div>

                          <div className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
                            <NumberField
                              id={`prompt-audit-node-timeout-${index}`}
                              label={t('Timeout (ms)')}
                              value={endpoint.timeout_ms}
                              min={100}
                              max={120000}
                              description={t(
                                'Timeout for a single audit model attempt.'
                              )}
                              onChange={(timeout_ms) =>
                                updateEndpoint(index, { timeout_ms })
                              }
                            />
                            <NumberField
                              id={`prompt-audit-node-limit-${index}`}
                              label={t('Input limit (characters)')}
                              value={endpoint.input_limit}
                              min={256}
                              max={1048576}
                              description={t(
                                'Smallest enabled value controls chunk size.'
                              )}
                              onChange={(input_limit) =>
                                updateEndpoint(index, { input_limit })
                              }
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
                              onChange={(concurrency) =>
                                updateEndpoint(index, { concurrency })
                              }
                            />
                          </div>

                          <div className='space-y-2 border-t pt-3'>
                            <div className='space-y-1.5'>
                              <Label
                                htmlFor={`prompt-audit-node-token-${index}`}
                              >
                                {t('API token')}
                              </Label>
                              <Input
                                id={`prompt-audit-node-token-${index}`}
                                type='password'
                                autoComplete='new-password'
                                placeholder={
                                  endpoint.has_token
                                    ? t('Saved token (unchanged)')
                                    : t('Optional token')
                                }
                                value={endpoint.token}
                                onChange={(event) =>
                                  updateEndpoint(index, {
                                    token: event.target.value,
                                    token_changed: true,
                                  })
                                }
                              />
                              <div className='flex items-center justify-between gap-3'>
                                <p className='text-muted-foreground text-xs'>
                                  {tokenDescription}
                                </p>
                                {endpoint.has_token &&
                                  !endpoint.token_changed && (
                                    <Button
                                      variant='ghost'
                                      size='sm'
                                      className='h-7 px-2 text-xs'
                                      onClick={() =>
                                        updateEndpoint(index, {
                                          token: '',
                                          token_changed: true,
                                        })
                                      }
                                    >
                                      {t('Clear saved token')}
                                    </Button>
                                  )}
                              </div>
                            </div>

                            <div className='flex justify-end pt-1'>
                              <Button
                                variant='outline'
                                size='sm'
                                disabled={
                                  !persistedEndpointIDs.has(endpoint.id) ||
                                  (testMutation.isPending &&
                                    testMutation.variables === endpoint.id)
                                }
                                onClick={() => testMutation.mutate(endpoint.id)}
                              >
                                <FlaskConical className='size-3.5' />
                                {testLabel}
                              </Button>
                            </div>
                          </div>
                        </div>
                      </div>
                    )
                  })}

                  {endpoints.length === 0 && (
                    <p className='text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm'>
                      {t('No audit models configured')}
                    </p>
                  )}

                  <Button
                    variant='outline'
                    className='hover:border-primary/50 hover:bg-primary/5 w-full border-dashed py-4 transition-colors'
                    onClick={() =>
                      setEndpoints((current) => [
                        ...current,
                        {
                          client_key: crypto.randomUUID(),
                          original_id: '',
                          original_base_url: '',
                          id: '',
                          name: '',
                          base_url: '',
                          model: 'sileader/qwen3guard:0.6b',
                          timeout_ms: 3000,
                          input_limit: 4000,
                          concurrency: config.endpoint_concurrency,
                          enabled: true,
                          purpose: 'classify',
                          directions: ['input', 'output'],
                          has_token: false,
                          token: '',
                          token_changed: true,
                        },
                      ])
                    }
                  >
                    <CirclePlus className='size-4' />
                    {t('Add audit model')}
                  </Button>
                </CardContent>
              </Card>

              <Card>
                <CardHeader>
                  <CardTitle>{t('Timeouts, cache, and workers')}</CardTitle>
                  <CardDescription>
                    {t(
                      'Limits are measured in Unicode characters, milliseconds, seconds, or concurrent requests as labeled.'
                    )}
                  </CardDescription>
                </CardHeader>
                <CardContent className='space-y-6'>
                  <div className='space-y-3'>
                    <div className='text-muted-foreground flex items-center gap-2 border-b pb-2 text-xs font-semibold tracking-wider uppercase'>
                      <Timer className='text-primary size-3.5' />
                      <span>{t('Request timeouts and cache')}</span>
                    </div>
                    <div className='grid gap-3 sm:grid-cols-2'>
                      <NumberField
                        id='prompt-audit-total-timeout'
                        label={t('Total timeout (ms)')}
                        value={config.total_timeout_ms}
                        min={100}
                        max={120000}
                        description={t(
                          'Deadline for reviewing the complete request.'
                        )}
                        onChange={(value) =>
                          setConfig((current) => ({
                            ...current,
                            total_timeout_ms: value,
                          }))
                        }
                      />
                      <NumberField
                        id='prompt-audit-overlap'
                        label={t('Chunk overlap (characters)')}
                        value={config.chunk_overlap}
                        min={0}
                        max={512}
                        description={t(
                          'Overlap between adjacent chunks to resist boundary bypasses.'
                        )}
                        onChange={(value) =>
                          setConfig((current) => ({
                            ...current,
                            chunk_overlap: value,
                          }))
                        }
                      />
                      <NumberField
                        id='prompt-audit-chunk-concurrency'
                        label={t('Chunk concurrency')}
                        value={config.chunk_concurrency}
                        min={1}
                        max={16}
                        description={t(
                          'Maximum parallel checks per prompt, within the global and node limits. A blocking result stops later batches.'
                        )}
                        onChange={(value) =>
                          setConfig((current) => ({
                            ...current,
                            chunk_concurrency: value,
                          }))
                        }
                      />
                      <div className='sm:col-span-2'>
                        <NumberField
                          id='prompt-audit-cache-ttl'
                          label={t('Cache TTL (seconds)')}
                          value={config.cache_ttl_seconds}
                          min={0}
                          max={86400}
                          description={t('Use 0 to disable result caching.')}
                          onChange={(value) =>
                            setConfig((current) => ({
                              ...current,
                              cache_ttl_seconds: value,
                            }))
                          }
                        />
                      </div>
                    </div>
                  </div>

                  <div className='space-y-3'>
                    <div className='text-muted-foreground flex items-center gap-2 border-b pb-2 text-xs font-semibold tracking-wider uppercase'>
                      <HardDrive className='text-primary size-3.5' />
                      <span>{t('Output buffering and retention')}</span>
                    </div>
                    <div className='grid gap-3 sm:grid-cols-2'>
                      <NumberField
                        id='prompt-audit-output-memory'
                        label={t('Output memory threshold (MB)')}
                        value={bytesToMB(config.output_memory_bytes)}
                        min={1}
                        max={Math.max(1, bytesToMB(config.output_max_bytes))}
                        description={t(
                          'Larger buffered outputs spill to temporary storage.'
                        )}
                        onChange={(mb) =>
                          setConfig((current) => ({
                            ...current,
                            output_memory_bytes: mbToBytes(mb),
                          }))
                        }
                      />
                      <NumberField
                        id='prompt-audit-output-limit'
                        label={t('Maximum output capture (MB)')}
                        value={bytesToMB(config.output_max_bytes)}
                        min={1}
                        max={64}
                        description={t(
                          'Blocking stops delivery when this limit is exceeded.'
                        )}
                        onChange={(mb) =>
                          setConfig((current) => ({
                            ...current,
                            output_max_bytes: mbToBytes(mb),
                          }))
                        }
                      />
                      <div className='sm:col-span-2'>
                        <NumberField
                          id='prompt-audit-retention'
                          label={t('Retention (days)')}
                          value={config.retention_days}
                          min={0}
                          max={3650}
                          description={t(
                            'Use 0 to retain full prompt text permanently.'
                          )}
                          onChange={(value) =>
                            setConfig((current) => ({
                              ...current,
                              retention_days: value,
                            }))
                          }
                        />
                      </div>
                    </div>
                  </div>

                  <div className='space-y-3'>
                    <div className='text-muted-foreground flex items-center gap-2 border-b pb-2 text-xs font-semibold tracking-wider uppercase'>
                      <Cpu className='text-primary size-3.5' />
                      <span>{t('Queue workers and concurrency')}</span>
                    </div>
                    <div className='grid gap-3 sm:grid-cols-2'>
                      <NumberField
                        id='prompt-audit-workers'
                        label={t('Async workers')}
                        value={config.worker_count}
                        min={1}
                        max={64}
                        description={t(
                          'Durable queue worker count on the elected master.'
                        )}
                        onChange={(value) =>
                          setConfig((current) => ({
                            ...current,
                            worker_count: value,
                          }))
                        }
                      />
                      <NumberField
                        id='prompt-audit-attempts'
                        label={t('Maximum attempts')}
                        value={config.max_attempts}
                        min={1}
                        max={4}
                        description={t(
                          'Includes the initial asynchronous audit attempt.'
                        )}
                        onChange={(value) =>
                          setConfig((current) => ({
                            ...current,
                            max_attempts: value,
                          }))
                        }
                      />
                      <NumberField
                        id='prompt-audit-global-concurrency'
                        label={t('Global concurrency')}
                        value={config.global_concurrency}
                        min={1}
                        max={1024}
                        description={t(
                          'Maximum simultaneous audit calls in this process.'
                        )}
                        onChange={(value) =>
                          setConfig((current) => ({
                            ...current,
                            global_concurrency: value,
                          }))
                        }
                      />
                      <NumberField
                        id='prompt-audit-node-concurrency'
                        label={t('Default audit model concurrency')}
                        value={config.endpoint_concurrency}
                        min={1}
                        max={256}
                        description={t(
                          'Fallback concurrency used by newly configured audit models.'
                        )}
                        onChange={(value) =>
                          setConfig((current) => ({
                            ...current,
                            endpoint_concurrency: value,
                          }))
                        }
                      />
                    </div>
                  </div>
                </CardContent>
              </Card>
            </div>
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

export function PromptAuditSettings() {
  const { t } = useTranslation()
  const configQuery = useQuery({
    queryKey: ['prompt-audit', 'config'],
    queryFn: async () => {
      const result = await getPromptAuditConfig()
      if (!result.success || !result.data) {
        throw new Error(
          result.message || t('Failed to load prompt audit settings')
        )
      }
      return result
    },
  })
  const categoriesQuery = useQuery({
    queryKey: ['prompt-audit', 'categories'],
    queryFn: async () => {
      const result = await getPromptAuditCategories()
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to load risk categories'))
      }
      return result.data
    },
    staleTime: 5 * 60 * 1000,
  })
  const groupsQuery = useQuery({
    queryKey: ['groups', 'prompt-audit-settings'],
    queryFn: async () => {
      const result = await getGroups()
      return result.success ? (result.data ?? []) : []
    },
    staleTime: 5 * 60 * 1000,
  })

  if (
    configQuery.isLoading ||
    categoriesQuery.isLoading ||
    groupsQuery.isLoading
  ) {
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Prompt audit')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <p className='text-muted-foreground py-12 text-center'>
            {t('Loading prompt audit settings...')}
          </p>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  const error = configQuery.error || categoriesQuery.error || groupsQuery.error
  if (error || !configQuery.data?.data) {
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Prompt audit')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <p className='text-destructive py-12 text-center'>
            {error?.message || t('Prompt audit settings are unavailable')}
          </p>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  const config = configQuery.data.data
  return (
    <PromptAuditSettingsForm
      key={config.config_version}
      initialConfig={config}
      categories={categoriesQuery.data ?? []}
      availableGroups={groupsQuery.data ?? []}
    />
  )
}
