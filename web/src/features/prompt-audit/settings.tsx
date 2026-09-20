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
  CirclePlus,
  FlaskConical,
  Save,
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
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { getGroups } from '@/features/users/api'

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
    <div className='space-y-1.5'>
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        type='number'
        min={min}
        max={max}
        value={value}
        onChange={(event) => onChange(Number(event.target.value || '0'))}
      />
      <p className='text-muted-foreground text-xs'>{description}</p>
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
    initialConfig.endpoints.map((node) => node.id)
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
        throw new Error(result.message || t('Audit node test failed'))
      }
      return result.data
    },
    onSuccess: (result) =>
      toast.success(
        t('Audit node responded in {{latency}} ms with {{safety}}', {
          latency: result.latency_ms,
          safety: result.safety || result.decision,
        })
      ),
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
    'Risk matches return 403 and audit failures return 503.'
  )
  if (config.mode === 'off') {
    modeDescription = t('Requests are not sent to audit nodes.')
  } else if (config.mode === 'async_audit') {
    modeDescription = t(
      'Requests continue normally while durable workers record would_action.'
    )
  }

  let outputModeDescription = t('Generated output is not collected for audit.')
  if (config.output_mode === 'blocking') {
    outputModeDescription = t(
      'Generated text is withheld until review passes. Generation usage is still billed when content is blocked.'
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
          <Save />
          {saveMutation.isPending ? t('Saving...') : t('Save settings')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='w-full space-y-4'>
          <PromptAuditNavigation />
          {librariesQuery.isPending && <LoadingState />}
          {librariesQuery.isError && (
            <ErrorState
              description={librariesQuery.error.message}
              onRetry={() => void librariesQuery.refetch()}
            />
          )}
          {librariesQuery.isSuccess && (
            <ScopePoliciesSection
              policies={config.scope_policies ?? defaultPromptScopePolicies()}
              libraries={librariesQuery.data}
              wordFilterEnabled={config.word_filter_enabled ?? true}
              onChange={(scope_policies) =>
                setConfig((current) => ({ ...current, scope_policies }))
              }
              onWordFilterChange={(word_filter_enabled) =>
                setConfig((current) => ({ ...current, word_filter_enabled }))
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
              <div className='space-y-1.5'>
                <Label htmlFor='prompt-audit-mode'>
                  {t('Model audit mode')}
                </Label>
                <NativeSelect
                  id='prompt-audit-mode'
                  className='w-full sm:w-72'
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
                <p className='text-muted-foreground text-xs'>
                  {modeDescription}
                </p>
              </div>

              <div className='space-y-1.5'>
                <Label htmlFor='prompt-audit-output-mode'>
                  {t('Output audit mode')}
                </Label>
                <NativeSelect
                  id='prompt-audit-output-mode'
                  className='w-full sm:w-72'
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
                <p className='text-muted-foreground text-xs'>
                  {outputModeDescription}
                </p>
              </div>

              <div className='flex items-start justify-between gap-4 rounded-lg border p-3'>
                <div>
                  <Label htmlFor='prompt-audit-all-groups'>
                    {t('All groups')}
                  </Label>
                  <p className='text-muted-foreground mt-1 text-xs'>
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
                <div>
                  <Label>{t('Audited groups')}</Label>
                  <div className='mt-2 grid gap-2 sm:grid-cols-2 lg:grid-cols-3'>
                    {groups.map((group) => (
                      <label
                        key={group}
                        className='flex items-center gap-2 rounded-lg border p-3 text-sm'
                      >
                        <Checkbox
                          checked={config.groups.includes(group)}
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
                    ))}
                    {groups.length === 0 && (
                      <p className='text-muted-foreground text-sm'>
                        {t('No request groups are available.')}
                      </p>
                    )}
                  </div>
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>{t('Gray-area review')}</CardTitle>
              <CardDescription>
                {t(
                  'An independent general model may review only Qwen3Guard Controversial results. It cannot override direct wordlist blocks or baseline blocks.'
                )}
              </CardDescription>
            </CardHeader>
            <CardContent className='space-y-4'>
              <div className='flex items-start justify-between gap-4 rounded-lg border p-3'>
                <div>
                  <Label htmlFor='prompt-audit-review-enabled'>
                    {t('Enable gray-area review')}
                  </Label>
                  <p className='text-muted-foreground mt-1 text-xs'>
                    {t(
                      'Requires at least one enabled node with the gray-area reviewer purpose.'
                    )}
                  </p>
                </div>
                <Switch
                  id='prompt-audit-review-enabled'
                  checked={config.review_enabled}
                  onCheckedChange={(review_enabled) =>
                    setConfig((current) => ({ ...current, review_enabled }))
                  }
                />
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='prompt-audit-review-prompt'>
                  {t('Reviewer system prompt')}
                </Label>
                <Textarea
                  id='prompt-audit-review-prompt'
                  value={config.review_prompt}
                  maxLength={20000}
                  rows={8}
                  placeholder={t(
                    'Leave empty to use the built-in reviewer policy.'
                  )}
                  onChange={(event) =>
                    setConfig((current) => ({
                      ...current,
                      review_prompt: event.target.value,
                    }))
                  }
                />
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'The server fixes the JSON output protocol; this prompt only defines review policy.'
                  )}
                </p>
              </div>
              <div>
                <Label>
                  {t('Controversial categories blocked before review')}
                </Label>
                <div className='mt-2 grid gap-2 sm:grid-cols-2 lg:grid-cols-3'>
                  {categories.map((category) => (
                    <label
                      key={category.id}
                      className='flex items-center gap-2 rounded-lg border p-3 text-sm'
                    >
                      <Checkbox
                        checked={config.controversial_block_categories.includes(
                          category.id
                        )}
                        onCheckedChange={(checked) =>
                          setConfig((current) => ({
                            ...current,
                            controversial_block_categories: checked
                              ? [
                                  ...current.controversial_block_categories,
                                  category.id,
                                ]
                              : current.controversial_block_categories.filter(
                                  (value) => value !== category.id
                                ),
                          }))
                        }
                      />
                      {t(category.label)}
                    </label>
                  ))}
                </div>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>{t('Risk categories')}</CardTitle>
              <CardDescription>
                {t(
                  'Qwen3Guard always returns its complete classification; these selections only control the blocking policy.'
                )}
              </CardDescription>
            </CardHeader>
            <CardContent className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
              {categories.map((category) => (
                <label
                  key={category.id}
                  className='flex items-start gap-3 rounded-lg border p-3'
                >
                  <Checkbox
                    className='mt-0.5'
                    checked={config.enabled_categories.includes(category.id)}
                    onCheckedChange={(checked) =>
                      setConfig((current) => ({
                        ...current,
                        enabled_categories: checked
                          ? [...current.enabled_categories, category.id]
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
                    <span className='text-muted-foreground mt-1 block text-xs'>
                      {t(category.description)}
                    </span>
                  </span>
                </label>
              ))}
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
            <CardContent className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
              <NumberField
                id='prompt-audit-total-timeout'
                label={t('Total timeout (ms)')}
                value={config.total_timeout_ms}
                min={100}
                max={120000}
                description={t('Deadline for reviewing the complete request.')}
                onChange={(value) =>
                  setConfig((current) => ({
                    ...current,
                    total_timeout_ms: value,
                  }))
                }
              />
              <NumberField
                id='prompt-audit-output-memory'
                label={t('Output memory threshold (bytes)')}
                value={config.output_memory_bytes}
                min={1024}
                max={config.output_max_bytes}
                description={t(
                  'Larger buffered outputs spill to temporary storage.'
                )}
                onChange={(value) =>
                  setConfig((current) => ({
                    ...current,
                    output_memory_bytes: value,
                  }))
                }
              />
              <NumberField
                id='prompt-audit-output-limit'
                label={t('Maximum output capture (bytes)')}
                value={config.output_max_bytes}
                min={1024}
                max={64 * 1024 * 1024}
                description={t(
                  'Blocking stops delivery when this limit is exceeded.'
                )}
                onChange={(value) =>
                  setConfig((current) => ({
                    ...current,
                    output_max_bytes: value,
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
                  setConfig((current) => ({ ...current, chunk_overlap: value }))
                }
              />
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
              <NumberField
                id='prompt-audit-retention'
                label={t('Retention (days)')}
                value={config.retention_days}
                min={0}
                max={3650}
                description={t('Use 0 to retain full prompt text permanently.')}
                onChange={(value) =>
                  setConfig((current) => ({
                    ...current,
                    retention_days: value,
                  }))
                }
              />
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
                  setConfig((current) => ({ ...current, worker_count: value }))
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
                  setConfig((current) => ({ ...current, max_attempts: value }))
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
                label={t('Default node concurrency')}
                value={config.endpoint_concurrency}
                min={1}
                max={256}
                description={t(
                  'Fallback concurrency used by newly configured nodes.'
                )}
                onChange={(value) =>
                  setConfig((current) => ({
                    ...current,
                    endpoint_concurrency: value,
                  }))
                }
              />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle id='audit-nodes' className='scroll-mt-24'>
                {t('Qwen3Guard nodes')}
              </CardTitle>
              <CardDescription>
                {t(
                  'Nodes are tried in order. Tokens are write-only; leave the field untouched to preserve the saved token.'
                )}
              </CardDescription>
            </CardHeader>
            <CardContent className='space-y-4'>
              {endpoints.map((endpoint, index) => {
                let tokenDescription = t('No token is stored for this node.')
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
                  testLabel = t('Test saved node')
                }

                return (
                  <div
                    key={endpoint.client_key}
                    className='space-y-4 rounded-xl border p-4'
                  >
                    <div className='flex flex-wrap items-center justify-between gap-3'>
                      <div className='flex items-center gap-2'>
                        <Badge variant='outline'>#{index + 1}</Badge>
                        <Switch
                          aria-label={`${t('Enabled')}: ${endpoint.name || endpoint.id || index + 1}`}
                          checked={endpoint.enabled}
                          onCheckedChange={(enabled) =>
                            updateEndpoint(index, { enabled })
                          }
                        />
                        <span className='text-sm font-medium'>
                          {endpoint.name || endpoint.id || t('New audit node')}
                        </span>
                      </div>
                      <div className='flex gap-1'>
                        <Button
                          variant='ghost'
                          size='icon-sm'
                          aria-label={t('Move node up')}
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
                          <ArrowUp />
                        </Button>
                        <Button
                          variant='ghost'
                          size='icon-sm'
                          aria-label={t('Move node down')}
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
                          <ArrowDown />
                        </Button>
                        <Button
                          variant='ghost'
                          size='icon-sm'
                          aria-label={t('Remove node')}
                          onClick={() =>
                            setEndpoints((current) =>
                              current.filter(
                                (_, endpointIndex) => endpointIndex !== index
                              )
                            )
                          }
                        >
                          <Trash2 />
                        </Button>
                      </div>
                    </div>

                    <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
                      <div className='space-y-1.5'>
                        <Label htmlFor={`prompt-audit-node-purpose-${index}`}>
                          {t('Node purpose')}
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
                            {t('Gray-area reviewer')}
                          </NativeSelectOption>
                        </NativeSelect>
                      </div>
                      {endpoint.purpose === 'classify' && (
                        <div className='space-y-1.5'>
                          <Label>{t('Audit directions')}</Label>
                          <div className='flex flex-wrap gap-3 rounded-lg border p-2.5'>
                            {(
                              [
                                ['input', t('Request input')],
                                ['output', t('Generated output')],
                              ] as const
                            ).map(([direction, label]) => (
                              <label
                                key={direction}
                                className='flex items-center gap-2 text-sm'
                              >
                                <Checkbox
                                  checked={endpoint.directions.includes(
                                    direction
                                  )}
                                  onCheckedChange={(checked) =>
                                    updateEndpoint(index, {
                                      directions: checked
                                        ? [...endpoint.directions, direction]
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
                      )}
                      <div className='space-y-1.5'>
                        <Label htmlFor={`prompt-audit-node-id-${index}`}>
                          {t('Node ID')}
                        </Label>
                        <Input
                          id={`prompt-audit-node-id-${index}`}
                          value={endpoint.id}
                          onChange={(event) =>
                            updateEndpoint(index, { id: event.target.value })
                          }
                        />
                      </div>
                      <div className='space-y-1.5'>
                        <Label htmlFor={`prompt-audit-node-name-${index}`}>
                          {t('Name')}
                        </Label>
                        <Input
                          id={`prompt-audit-node-name-${index}`}
                          value={endpoint.name}
                          onChange={(event) =>
                            updateEndpoint(index, { name: event.target.value })
                          }
                        />
                      </div>
                      <div className='space-y-1.5 sm:col-span-2 lg:col-span-1'>
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
                      </div>
                      <div className='space-y-1.5'>
                        <Label htmlFor={`prompt-audit-node-model-${index}`}>
                          {t('Model')}
                        </Label>
                        <Input
                          id={`prompt-audit-node-model-${index}`}
                          value={endpoint.model}
                          onChange={(event) =>
                            updateEndpoint(index, { model: event.target.value })
                          }
                        />
                      </div>
                      <NumberField
                        id={`prompt-audit-node-timeout-${index}`}
                        label={t('Timeout (ms)')}
                        value={endpoint.timeout_ms}
                        min={100}
                        max={120000}
                        description={t('Per-attempt node timeout.')}
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
                        label={t('Node concurrency')}
                        value={endpoint.concurrency}
                        min={1}
                        max={256}
                        description={t(
                          'Maximum calls to this node per process.'
                        )}
                        onChange={(concurrency) =>
                          updateEndpoint(index, { concurrency })
                        }
                      />
                      <div className='space-y-1.5 sm:col-span-2'>
                        <Label htmlFor={`prompt-audit-node-token-${index}`}>
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
                          {endpoint.has_token && !endpoint.token_changed && (
                            <Button
                              variant='ghost'
                              size='sm'
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
                    </div>

                    <div className='flex justify-end'>
                      <Button
                        variant='outline'
                        disabled={
                          !persistedEndpointIDs.has(endpoint.id) ||
                          (testMutation.isPending &&
                            testMutation.variables === endpoint.id)
                        }
                        onClick={() => testMutation.mutate(endpoint.id)}
                      >
                        <FlaskConical />
                        {testLabel}
                      </Button>
                    </div>
                  </div>
                )
              })}

              {endpoints.length === 0 && (
                <p className='text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm'>
                  {t('No audit nodes configured')}
                </p>
              )}
              <Button
                variant='outline'
                onClick={() =>
                  setEndpoints((current) => [
                    ...current,
                    {
                      client_key: crypto.randomUUID(),
                      original_id: '',
                      original_base_url: '',
                      id: `endpoint-${Date.now()}`,
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
                <CirclePlus />
                {t('Add audit node')}
              </Button>
            </CardContent>
          </Card>
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
