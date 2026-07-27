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
import { useQuery } from '@tanstack/react-query'
import {
  GitBranch,
  Loader2,
  Pencil,
  Plus,
  TriangleAlert,
  Trash2,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { MultiSelect } from '@/components/multi-select'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { ComboboxInput } from '@/components/ui/combobox-input'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { useDebounce } from '@/hooks'

import {
  createUserModelRoute,
  deleteUserModelRoute,
  getUserModelRouteCandidates,
  getUserModelRoutes,
  updateUserModelRoute,
} from '../../api'
import type { UserModelRoute } from '../../types'

type UserModelRoutesDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  userId: number | null
  username?: string
  onSuccess?: () => void
}

type RouteDraft = {
  source_model: string
  target_model: string
  pool_name: string
  inject_prompt: string
  execution_group: string
  all_groups: boolean
  groups: string[]
  channel_ids: string[]
  enabled: boolean
}

const EMPTY_DRAFT: RouteDraft = {
  source_model: '',
  target_model: '',
  pool_name: '',
  inject_prompt: '',
  execution_group: '',
  all_groups: true,
  groups: [],
  channel_ids: [],
  enabled: true,
}

const EMPTY_ROUTES: UserModelRoute[] = []
const EMPTY_GROUPS: string[] = []

// Mirrors UserModelRouteMaxInjectPrompt in model/user_model_route.go so the form
// rejects an over-long prompt before the request reaches the server.
const INJECT_PROMPT_MAX_LENGTH = 8000

function draftFromRoute(route: UserModelRoute): RouteDraft {
  return {
    source_model: route.source_model,
    target_model: route.target_model,
    pool_name: route.pool_name,
    inject_prompt: route.inject_prompt || '',
    execution_group: route.execution_group,
    all_groups: route.all_groups,
    groups: route.groups || [],
    channel_ids: (route.channel_ids || []).map(String),
    enabled: route.enabled,
  }
}

function routePayload(draft: RouteDraft) {
  return {
    source_model: draft.source_model.trim(),
    target_model: draft.target_model.trim(),
    pool_name: draft.pool_name.trim(),
    inject_prompt: draft.inject_prompt.trim(),
    execution_group: draft.execution_group,
    all_groups: draft.all_groups,
    groups: draft.all_groups ? [] : draft.groups,
    channel_ids: draft.channel_ids.map(Number).filter((id) => id > 0),
    enabled: draft.enabled,
  }
}

export function UserModelRoutesDialog(props: UserModelRoutesDialogProps) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<RouteDraft>(EMPTY_DRAFT)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [saving, setSaving] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<UserModelRoute | null>(null)
  const [deleting, setDeleting] = useState(false)

  const routesQuery = useQuery({
    queryKey: ['user-model-routes', props.userId],
    queryFn: () => getUserModelRoutes(props.userId as number),
    enabled: props.open && props.userId !== null,
    staleTime: 30 * 1000,
  })
  const candidatesQuery = useQuery({
    queryKey: ['user-model-route-candidates', props.userId],
    queryFn: () => getUserModelRouteCandidates(props.userId as number),
    enabled: props.open && props.userId !== null,
    staleTime: 60 * 1000,
  })
  const debouncedTargetModel = useDebounce(draft.target_model.trim(), 350)
  const channelsQuery = useQuery({
    queryKey: [
      'user-model-route-candidate-channels',
      props.userId,
      debouncedTargetModel,
      draft.execution_group,
    ],
    queryFn: () =>
      getUserModelRouteCandidates(props.userId as number, {
        target_model: debouncedTargetModel,
        execution_group: draft.execution_group,
      }),
    enabled:
      props.open &&
      props.userId !== null &&
      debouncedTargetModel.length > 0 &&
      draft.execution_group.length > 0,
    staleTime: 30 * 1000,
  })
  const targetModel = draft.target_model.trim()
  const targetQuerySettled =
    targetModel.length > 0 && debouncedTargetModel === targetModel
  const channelQueryReady =
    targetQuerySettled && draft.execution_group.trim().length > 0
  const channelData =
    channelQueryReady && channelsQuery.data?.success === true
      ? channelsQuery.data.data
      : undefined

  const routes =
    routesQuery.data?.success === true
      ? (routesQuery.data.data ?? EMPTY_ROUTES)
      : EMPTY_ROUTES
  const candidateData =
    candidatesQuery.data?.success === true
      ? candidatesQuery.data.data
      : undefined
  const applicableGroups = candidateData?.applicable_groups ?? EMPTY_GROUPS
  const executionGroups = candidateData?.execution_groups ?? EMPTY_GROUPS
  const executionGroupCounts = useMemo(
    () => channelData?.execution_group_channel_counts ?? {},
    [channelData]
  )
  const sourceModelOptions = useMemo(() => {
    const values = new Set([
      ...(candidateData?.source_models || []),
      ...routes.map((route) => route.source_model),
      draft.source_model,
    ])
    return [...values]
      .filter(Boolean)
      .sort((left, right) => left.localeCompare(right))
      .map((model) => ({ value: model, label: model }))
  }, [candidateData?.source_models, routes, draft.source_model])
  const targetModelOptions = useMemo(() => {
    const values = new Set([
      ...(candidateData?.target_models || []),
      ...routes.map((route) => route.target_model),
      draft.target_model,
    ])
    return [...values]
      .filter(Boolean)
      .sort((left, right) => left.localeCompare(right))
      .map((model) => ({ value: model, label: model }))
  }, [candidateData?.target_models, routes, draft.target_model])
  const executionGroupOptions = useMemo(
    () =>
      executionGroups.map((group) => {
        const channelCount = executionGroupCounts[group] ?? 0
        const channelCountsLoaded = channelData !== undefined
        const disabled = channelCountsLoaded && channelCount === 0
        let label = group
        if (channelCountsLoaded) {
          const countLabel = t('{{count}} channels', { count: channelCount })
          label = disabled
            ? `${group} (${countLabel} · ${t(
                'No enabled channels for this model in this group'
              )})`
            : `${group} (${countLabel})`
        }
        return { value: group, label, disabled }
      }),
    [channelData, executionGroupCounts, executionGroups, t]
  )
  const applicableGroupOptions = useMemo(
    () => applicableGroups.map((group) => ({ value: group, label: group })),
    [applicableGroups]
  )
  const channelOptions = useMemo(() => {
    const selectedIds = new Set(draft.channel_ids)
    const channels = channelData?.channels ?? []
    const aggregateChildren = new Map<
      number,
      { name: string; channelIds: string[] }
    >()
    for (const channel of channels) {
      if (!channel.aggregate_id || !channel.aggregate_name) continue
      const aggregate = aggregateChildren.get(channel.aggregate_id) ?? {
        name: channel.aggregate_name,
        channelIds: [],
      }
      aggregate.channelIds.push(String(channel.id))
      aggregateChildren.set(channel.aggregate_id, aggregate)
    }
    const compatibilitySummary = (channel: (typeof channels)[number]) => {
      const matrix = channel.protocol_compatibility ?? {}
      return (['chat', 'messages', 'responses', 'gemini'] as const)
        .map((protocol) => {
          const status = matrix[protocol] ?? 'incompatible'
          let statusLabel = t('Incompatible')
          if (status === 'native') statusLabel = t('Native')
          else if (status === 'convertible') {
            statusLabel = t('Convertible')
          }
          return `${protocol}: ${statusLabel}`
        })
        .join(' · ')
    }
    return [
      ...[...aggregateChildren.entries()].map(([aggregateId, aggregate]) => ({
        value: `aggregate:${aggregateId}`,
        label: `${t('Aggregate channel')}: ${aggregate.name} (${t(
          '{{count}} channels',
          { count: aggregate.channelIds.length }
        )})`,
      })),
      ...channels.map((channel) => ({
        value: String(channel.id),
        label: `${channel.aggregate_name ? `${channel.aggregate_name} / ` : ''}${
          channel.name || `#${channel.id}`
        } (#${channel.id}) · ${t('Priority')} ${channel.priority ?? 0} · ${t(
          'Weight'
        )} ${channel.weight} · ${compatibilitySummary(channel)}`,
      })),
      ...[...selectedIds]
        .filter((id) => !channels.some((channel) => String(channel.id) === id))
        .map((id) => ({ value: id, label: `#${id}` })),
    ]
  }, [channelData, draft.channel_ids, t])

  const routesFailed =
    routesQuery.isError || routesQuery.data?.success === false
  const editorFailed =
    candidatesQuery.isError || candidatesQuery.data?.success === false
  const editorLoading = candidatesQuery.isLoading
  const channelsFailed =
    channelQueryReady &&
    (channelsQuery.isError || channelsQuery.data?.success === false)
  const channelsLoading =
    channelQueryReady && (channelsQuery.isLoading || channelsQuery.isFetching)
  const draftComplete =
    draft.source_model.trim().length > 0 &&
    draft.target_model.trim().length > 0 &&
    draft.execution_group.trim().length > 0 &&
    (draft.all_groups || draft.groups.length > 0) &&
    draft.channel_ids.length > 0
  const canSave =
    props.userId !== null &&
    !saving &&
    !deleting &&
    !routesQuery.isLoading &&
    !routesFailed &&
    !editorLoading &&
    !editorFailed &&
    channelQueryReady &&
    !channelsLoading &&
    !channelsFailed &&
    channelData !== undefined &&
    draftComplete
  const availableChannelIds = useMemo(
    () =>
      new Set(
        (channelData?.channels ?? []).map((channel) => String(channel.id))
      ),
    [channelData]
  )
  const invalidSelectedChannelIds = useMemo(
    () =>
      editingId === null || channelsLoading
        ? []
        : draft.channel_ids.filter((id) => !availableChannelIds.has(id)),
    [availableChannelIds, channelsLoading, draft.channel_ids, editingId]
  )
  const canSaveRoute = canSave && invalidSelectedChannelIds.length === 0

  useEffect(() => {
    if (props.open) {
      setDraft(EMPTY_DRAFT)
      setEditingId(null)
    }
  }, [props.open, props.userId])

  useEffect(() => {
    if (!draft.execution_group && executionGroups.length > 0) {
      setDraft((current) => ({
        ...current,
        execution_group: executionGroups[0],
      }))
    }
  }, [draft.execution_group, executionGroups])

  useEffect(() => {
    if (!channelData || channelsQuery.isFetching) return

    const queryTarget = debouncedTargetModel
    const queryGroup = draft.execution_group
    const recommendedGroup = channelData.recommended_execution_group || ''
    const currentGroupCount =
      channelData.execution_group_channel_counts?.[queryGroup] ?? 0
    if (
      editingId === null &&
      recommendedGroup &&
      currentGroupCount === 0 &&
      recommendedGroup !== queryGroup
    ) {
      setDraft((current) => {
        if (
          current.target_model.trim() !== queryTarget ||
          current.execution_group !== queryGroup
        ) {
          return current
        }
        return {
          ...current,
          execution_group: recommendedGroup,
          channel_ids: [],
        }
      })
      return
    }

    const eligibleIds = new Set(
      (channelData.channels || []).map((channel) => String(channel.id))
    )
    setDraft((current) => {
      if (
        current.target_model.trim() !== queryTarget ||
        current.execution_group !== queryGroup
      ) {
        return current
      }
      if (editingId !== null) return current
      const channelIds = current.channel_ids.filter((id) => eligibleIds.has(id))
      if (channelIds.length === current.channel_ids.length) return current
      return { ...current, channel_ids: channelIds }
    })
  }, [
    channelData,
    channelsQuery.isFetching,
    debouncedTargetModel,
    draft.execution_group,
    editingId,
  ])

  const updateChannelSelection = (values: string[]) => {
    const selected = new Set(
      values.filter((value) => !value.startsWith('aggregate:'))
    )
    const channels = channelData?.channels ?? []
    for (const value of values) {
      if (!value.startsWith('aggregate:')) continue
      const aggregateId = Number(value.slice('aggregate:'.length))
      for (const channel of channels) {
        if (channel.aggregate_id === aggregateId) {
          selected.add(String(channel.id))
        }
      }
    }
    setDraft((current) => ({ ...current, channel_ids: [...selected] }))
  }

  const resetDraft = () => {
    setDraft({
      ...EMPTY_DRAFT,
      execution_group: executionGroups[0] || '',
    })
    setEditingId(null)
  }

  const handleSave = async () => {
    if (!props.userId) return
    const payload = routePayload(draft)
    if (!payload.source_model || !payload.target_model) {
      toast.error(t('Source and target models are required'))
      return
    }
    if (!payload.execution_group) {
      toast.error(t('Select an execution group'))
      return
    }
    if (!payload.all_groups && payload.groups.length === 0) {
      toast.error(t('Select at least one applicable group'))
      return
    }
    if (payload.channel_ids.length === 0) {
      toast.error(t('Select at least one channel'))
      return
    }

    setSaving(true)
    try {
      const result = editingId
        ? await updateUserModelRoute(props.userId, editingId, payload)
        : await createUserModelRoute(props.userId, payload)
      if (!result.success) {
        toast.error(result.message || t('Failed to save model route'))
        return
      }
      toast.success(
        editingId ? t('Model route updated') : t('Model route added')
      )
      resetDraft()
      await routesQuery.refetch()
      props.onSuccess?.()
    } catch {
      toast.error(t('Operation failed'))
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!props.userId || !deleteTarget) return
    setDeleting(true)
    try {
      const result = await deleteUserModelRoute(props.userId, deleteTarget.id)
      if (!result.success) {
        toast.error(result.message || t('Failed to delete model route'))
        return
      }
      toast.success(t('Model route deleted'))
      if (editingId === deleteTarget.id) resetDraft()
      setDeleteTarget(null)
      await routesQuery.refetch()
      props.onSuccess?.()
    } catch {
      toast.error(t('Operation failed'))
    } finally {
      setDeleting(false)
    }
  }

  const handleClose = (open: boolean) => {
    if (!open && !saving && !deleting) resetDraft()
    props.onOpenChange(open)
  }

  const renderRouteList = () => {
    if (routesQuery.isLoading) {
      return (
        <div className='text-muted-foreground flex items-center justify-center py-8 text-sm'>
          <Loader2 className='mr-2 size-4 animate-spin' />
          {t('Loading...')}
        </div>
      )
    }
    if (routesFailed) {
      return (
        <ErrorState
          title={t('Failed to load')}
          onRetry={() => routesQuery.refetch()}
          className='min-h-32 py-6'
        />
      )
    }
    if (routes.length === 0) {
      return (
        <EmptyState
          icon={GitBranch}
          title={t('No model routes')}
          className='min-h-32 py-6'
          bordered
        />
      )
    }
    return (
      <div className='space-y-2'>
        {routes.map((route) => (
          <div
            key={route.id}
            className='flex flex-col gap-3 rounded-md border p-3 sm:flex-row sm:items-center sm:justify-between'
          >
            <div className='min-w-0 space-y-1'>
              <div className='flex min-w-0 items-center gap-2 text-sm font-medium'>
                <span className='truncate'>{route.source_model}</span>
                <span className='text-muted-foreground'>-&gt;</span>
                <span className='truncate'>{route.target_model}</span>
              </div>
              <div className='text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-1 text-xs'>
                <span>
                  {t('Execution group')}: {route.execution_group}
                </span>
                <span>
                  {t('Channel pool name')}: {route.pool_name}
                </span>
                <span>
                  {route.all_groups
                    ? t('All user groups')
                    : t('{{count}} groups', {
                        count: route.groups?.length || 0,
                      })}
                </span>
                <span>
                  {t('{{count}} channels', {
                    count: route.channel_ids?.length || 0,
                  })}
                </span>
              </div>
            </div>
            <div className='flex items-center gap-2 self-end sm:self-auto'>
              <StatusBadge
                label={route.enabled ? t('Enabled') : t('Disabled')}
                variant={route.enabled ? 'success' : 'neutral'}
                copyable={false}
              />
              <Button
                variant='ghost'
                size='icon-sm'
                onClick={() => {
                  setEditingId(route.id)
                  setDraft(draftFromRoute(route))
                }}
                aria-label={t('Edit')}
              >
                <Pencil />
              </Button>
              <Button
                variant='ghost'
                size='icon-sm'
                onClick={() => setDeleteTarget(route)}
                aria-label={t('Delete')}
              >
                <Trash2 className='text-destructive' />
              </Button>
            </div>
          </div>
        ))}
      </div>
    )
  }

  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={handleClose}
        title={t('User Model Routes')}
        description={`${props.username || '-'} (ID: ${props.userId || '-'})`}
        contentClassName='sm:max-w-3xl'
        bodyClassName='space-y-5'
        footer={
          <>
            <Button
              variant='outline'
              onClick={() => handleClose(false)}
              disabled={saving}
            >
              {t('Close')}
            </Button>
            <Button onClick={handleSave} disabled={!canSaveRoute}>
              {saving ? <Loader2 className='animate-spin' /> : <GitBranch />}
              {editingId ? t('Save route') : t('Add route')}
            </Button>
          </>
        }
      >
        <div className='space-y-5'>
          <section className='space-y-4 rounded-md border p-4'>
            <div className='flex items-center justify-between gap-3'>
              <div>
                <h3 className='text-sm font-medium'>
                  {editingId ? t('Edit model route') : t('New model route')}
                </h3>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Keep the requested model name while selecting a different upstream target'
                  )}
                </p>
              </div>
              {editingId ? (
                <Button
                  variant='ghost'
                  size='sm'
                  onClick={resetDraft}
                  disabled={saving}
                >
                  {t('Clear')}
                </Button>
              ) : null}
            </div>

            {editorFailed && (
              <ErrorState
                title={t('Failed to load')}
                onRetry={() => {
                  void candidatesQuery.refetch()
                }}
                className='min-h-32 py-6'
              />
            )}
            {!editorFailed && editorLoading && (
              <div className='text-muted-foreground flex min-h-32 items-center justify-center text-sm'>
                <Loader2 className='mr-2 size-4 animate-spin' />
                {t('Loading...')}
              </div>
            )}
            {!editorFailed && !editorLoading && (
              <>
                <div className='grid gap-4 sm:grid-cols-2'>
                  <div className='space-y-2'>
                    <Label htmlFor='route-source-model'>
                      {t('Source model')}
                    </Label>
                    <ComboboxInput
                      id='route-source-model'
                      options={sourceModelOptions}
                      value={draft.source_model}
                      onValueChange={(value) =>
                        setDraft((current) => ({
                          ...current,
                          source_model: value,
                        }))
                      }
                      placeholder={t('Select source model')}
                      emptyText={t('No models found')}
                    />
                  </div>
                  <div className='space-y-2'>
                    <Label htmlFor='route-target-model'>
                      {t('Target model')}
                    </Label>
                    <ComboboxInput
                      id='route-target-model'
                      options={targetModelOptions}
                      value={draft.target_model}
                      onValueChange={(value) =>
                        setDraft((current) => ({
                          ...current,
                          target_model: value,
                          channel_ids:
                            current.target_model === value
                              ? current.channel_ids
                              : [],
                        }))
                      }
                      placeholder={t('Select or enter target model')}
                      emptyText={t('No models found')}
                      allowCustomValue
                    />
                  </div>
                </div>

                <div className='space-y-2'>
                  <Label htmlFor='route-pool-name'>
                    {t('Channel pool name')}
                  </Label>
                  <Input
                    id='route-pool-name'
                    value={draft.pool_name}
                    onChange={(event) => {
                      const poolName = event.currentTarget.value
                      setDraft((current) => ({
                        ...current,
                        pool_name: poolName,
                      }))
                    }}
                    placeholder={
                      draft.source_model && draft.target_model
                        ? `${draft.source_model} -> ${draft.target_model}`
                        : t('Optional channel pool name')
                    }
                    maxLength={191}
                  />
                </div>

                <div className='space-y-2'>
                  <Label htmlFor='route-inject-prompt'>
                    {t('Injected system prompt')}
                  </Label>
                  <Textarea
                    id='route-inject-prompt'
                    value={draft.inject_prompt}
                    onChange={(event) => {
                      const injectPrompt = [...event.currentTarget.value]
                        .slice(0, INJECT_PROMPT_MAX_LENGTH)
                        .join('')
                      setDraft((current) => ({
                        ...current,
                        inject_prompt: injectPrompt,
                      }))
                    }}
                    placeholder={t(
                      'Optional. Placed ahead of both the caller and channel system prompts.'
                    )}
                    rows={4}
                    aria-describedby='route-inject-prompt-hint'
                  />
                  <p
                    id='route-inject-prompt-hint'
                    className='text-muted-foreground text-xs'
                  >
                    {t(
                      'Applied only when this route is used. It takes priority over the channel system prompt.'
                    )}{' '}
                    <span className='tabular-nums'>
                      {[...draft.inject_prompt].length}/
                      {INJECT_PROMPT_MAX_LENGTH}
                    </span>
                  </p>
                </div>

                <div className='grid gap-4 sm:grid-cols-2'>
                  <div className='space-y-2'>
                    <Label htmlFor='route-execution-group'>
                      {t('Execution group')}
                    </Label>
                    <Select
                      items={executionGroupOptions}
                      value={draft.execution_group || null}
                      onValueChange={(value) =>
                        value !== null &&
                        setDraft((current) => ({
                          ...current,
                          execution_group: value,
                          channel_ids:
                            current.execution_group === value
                              ? current.channel_ids
                              : [],
                        }))
                      }
                    >
                      <SelectTrigger id='route-execution-group'>
                        <SelectValue
                          placeholder={t('Select execution group')}
                        />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {executionGroupOptions.map((group) => (
                            <SelectItem
                              key={group.value}
                              value={group.value}
                              disabled={group.disabled}
                            >
                              {group.label}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </div>
                  <div className='flex items-center justify-between gap-3 rounded-md border px-3 py-2'>
                    <div className='space-y-1'>
                      <Label htmlFor='route-enabled'>{t('Enabled')}</Label>
                      <p className='text-muted-foreground text-xs'>
                        {t(
                          'Disabled routes are ignored during model selection'
                        )}
                      </p>
                    </div>
                    <Switch
                      id='route-enabled'
                      checked={draft.enabled}
                      onCheckedChange={(enabled) =>
                        setDraft((current) => ({ ...current, enabled }))
                      }
                      aria-label={t('Enabled')}
                    />
                  </div>
                </div>

                <div className='flex items-center justify-between gap-3 rounded-md border px-3 py-2'>
                  <div className='space-y-1'>
                    <Label htmlFor='route-all-groups'>
                      {t('All user groups')}
                    </Label>
                    <p className='text-muted-foreground text-xs'>
                      {t(
                        'Apply this route to every group assigned to the user'
                      )}
                    </p>
                  </div>
                  <Switch
                    id='route-all-groups'
                    checked={draft.all_groups}
                    onCheckedChange={(allGroups) =>
                      setDraft((current) => ({
                        ...current,
                        all_groups: allGroups,
                      }))
                    }
                    aria-label={t('All user groups')}
                  />
                </div>

                {!draft.all_groups && (
                  <div className='space-y-2'>
                    <Label htmlFor='route-applicable-groups'>
                      {t('Applicable groups')}
                    </Label>
                    <MultiSelect
                      id='route-applicable-groups'
                      options={applicableGroupOptions}
                      selected={draft.groups}
                      onChange={(groupsValue) =>
                        setDraft((current) => ({
                          ...current,
                          groups: groupsValue,
                        }))
                      }
                      placeholder={t('Select applicable groups')}
                    />
                  </div>
                )}

                <div className='space-y-2'>
                  <Label htmlFor='route-channel-pool'>
                    {t('Channel pool')}
                  </Label>
                  {channelsFailed && channelQueryReady ? (
                    <ErrorState
                      title={t('Failed to load')}
                      onRetry={() => channelsQuery.refetch()}
                      className='min-h-32 py-6'
                    />
                  ) : (
                    <MultiSelect
                      id='route-channel-pool'
                      options={channelOptions}
                      selected={draft.channel_ids}
                      onChange={updateChannelSelection}
                      placeholder={t('Select channels')}
                      emptyText={
                        channelsLoading
                          ? t('Loading...')
                          : t('No channels found')
                      }
                      disabled={!channelQueryReady || channelsLoading}
                      maxVisibleChips={4}
                    />
                  )}
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Channel priority, weight, affinity, and retry rules remain active'
                    )}
                  </p>
                  {invalidSelectedChannelIds.length > 0 ? (
                    <div
                      role='alert'
                      className='text-destructive flex items-start gap-2 text-xs'
                    >
                      <TriangleAlert className='mt-0.5 size-3.5 shrink-0' />
                      <span>
                        {t(
                          'This route contains unavailable channels: {{channels}}. Select a valid channel pool before saving.',
                          { channels: invalidSelectedChannelIds.join(', ') }
                        )}
                      </span>
                    </div>
                  ) : null}
                </div>
              </>
            )}
          </section>

          <section className='space-y-3'>
            <div className='flex items-center justify-between gap-3'>
              <h3 className='text-sm font-medium'>{t('Configured routes')}</h3>
              <span className='text-muted-foreground text-xs tabular-nums'>
                {routes.length}
              </span>
            </div>
            {renderRouteList()}
          </section>

          <Button
            variant='outline'
            className='w-full'
            onClick={resetDraft}
            disabled={saving}
          >
            <Plus />
            {t('Start a new route')}
          </Button>
        </div>
      </Dialog>

      {deleteTarget ? (
        <ConfirmDialog
          open
          onOpenChange={(open) => !open && setDeleteTarget(null)}
          title={t('Delete model route')}
          desc={t('Delete the route from {{source}} to {{target}}?', {
            source: deleteTarget.source_model,
            target: deleteTarget.target_model,
          })}
          confirmText={t('Delete')}
          destructive
          handleConfirm={handleDelete}
          isLoading={deleting}
        />
      ) : null}
    </>
  )
}
