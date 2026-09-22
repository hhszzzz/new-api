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
import { Save } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { getGroups } from '@/features/users/api'

import {
  getPromptAuditCategories,
  getPromptAuditConfig,
  listPromptWordlists,
  testPromptAuditNode,
  updatePromptAuditConfig,
} from './api'
import { AdvancedLimitsSection } from './components/advanced-limits-section'
import { AuditModelsSection } from './components/audit-models-section'
import { EnforcementSection } from './components/enforcement-section'
import { PromptAuditNavigation } from './components/prompt-audit-navigation'
import { ScopePoliciesSection } from './components/scope-policies-section'
import {
  promptAuditEndpointDrafts,
  promptAuditEndpointUpdate,
  type PromptAuditEndpointDraft,
  validatePromptAuditConfig,
} from './lib'
import { defaultPromptScopePolicies } from './scopes'
import {
  auditDirectionLabel,
  promptAuditTestFailureMessage,
} from './test-failure'
import type {
  PromptAuditCategory,
  PromptAuditConfig,
  PromptAuditConfigUpdate,
} from './types'

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

type PromptAuditSettingsFormProps = {
  initialConfig: PromptAuditConfig
  categories: PromptAuditCategory[]
  availableGroups: string[]
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
        // Name the cause the backend measured. Collapsing every kind into
        // "test failed" left a rejected token, a redirecting base URL, an
        // unreadable verdict, and a model that ignores assistant messages
        // looking identical.
        throw new Error(
          result.data
            ? promptAuditTestFailureMessage(t, result.data)
            : result.message || t('Audit model test failed')
        )
      }
      return result.data
    },
    onSuccess: (result) => {
      toast.success(
        result.tested_directions?.length
          ? t('Audit model passed {{directions}} tests in {{latency}} ms', {
              latency: result.latency_ms,
              directions: result.tested_directions
                .map((direction) => auditDirectionLabel(t, direction))
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

  const updateConfig = (patch: Partial<PromptAuditConfigUpdate>) => {
    setConfig((current) => ({ ...current, ...patch }))
  }

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

  const moveEndpoint = (index: number, offset: -1 | 1) => {
    setEndpoints((current) => {
      const next = [...current]
      const target = index + offset
      if (target < 0 || target >= next.length) return current
      ;[next[index], next[target]] = [next[target], next[index]]
      return next
    })
  }

  const removeEndpoint = (index: number) => {
    setEndpoints((current) =>
      current.filter((_, endpointIndex) => endpointIndex !== index)
    )
  }

  const addEndpoint = (clientKey: string) => {
    setEndpoints((current) => [
      ...current,
      {
        client_key: clientKey,
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

  const groups = [...new Set([...availableGroups, ...config.groups])].sort()
  // testMutation.variables survives a successful run, so gate on isPending here
  // and hand the section a plain id.
  const pendingTestID = testMutation.isPending
    ? (testMutation.variables ?? null)
    : null

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
        {/* No width cap: the sibling records and wordlists tabs fill the
            viewport, so capping only this tab left a 168px dead strip and made
            the content column resize while switching between them. */}
        <div className='flex w-full flex-col gap-6'>
          <PromptAuditNavigation />

          {librariesQuery.isPending && <LoadingState />}
          {librariesQuery.isError && (
            <ErrorState
              description={librariesQuery.error.message}
              onRetry={() => void librariesQuery.refetch()}
            />
          )}

          <EnforcementSection
            config={config}
            categories={categories}
            groups={groups}
            onChange={updateConfig}
          />

          <AuditModelsSection
            endpoints={endpoints}
            persistedEndpointIDs={persistedEndpointIDs}
            pendingTestID={pendingTestID}
            onAdd={addEndpoint}
            onChange={updateEndpoint}
            onMove={moveEndpoint}
            onRemove={removeEndpoint}
            onTest={(endpoint) => testMutation.mutate(endpoint.id)}
          />

          {librariesQuery.isSuccess && (
            <ScopePoliciesSection
              policies={config.scope_policies ?? defaultPromptScopePolicies()}
              libraries={librariesQuery.data}
              wordFilterEnabled={config.word_filter_enabled ?? true}
              onChange={(scope_policies) => updateConfig({ scope_policies })}
              onWordFilterChange={(word_filter_enabled) =>
                updateConfig({ word_filter_enabled })
              }
            />
          )}

          <AdvancedLimitsSection config={config} onChange={updateConfig} />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

export function PromptAuditSettings() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
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
          <LoadingState />
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
          <ErrorState
            description={
              error?.message || t('Prompt audit settings are unavailable')
            }
            onRetry={() =>
              void queryClient.invalidateQueries({ queryKey: ['prompt-audit'] })
            }
          />
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
