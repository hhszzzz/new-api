/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import {
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { toIntlLocale } from '@/i18n/languages'
import { api } from '@/lib/api'
import { formatNumber } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

type GroupEntry = {
  name: string
  description: string
  ratio: number
  sort_order: number
}

type GroupDraft = {
  name: string
  description: string
  ratio: string
  sort_order: string
}

type GroupRegistryEditorProps = {
  onChanged?: () => void
}

const emptyDraft: GroupDraft = {
  name: '',
  description: '',
  ratio: '1',
  sort_order: '0',
}

const emptyGroups: GroupEntry[] = []

async function fetchGroupRegistry() {
  const response = await api.get<{
    success: boolean
    message: string
    data: GroupEntry[]
  }>('/api/group/manage')
  return requireServerSuccess(response.data).data
}

export function GroupRegistryEditor({ onChanged }: GroupRegistryEditorProps) {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const [sheetOpen, setSheetOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<GroupEntry | null>(null)
  const [editingName, setEditingName] = useState<string | null>(null)
  const [draft, setDraft] = useState<GroupDraft>(emptyDraft)
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const groupsQuery = useQuery({
    queryKey: ['group-registry'],
    queryFn: fetchGroupRegistry,
  })
  const groups = groupsQuery.data ?? emptyGroups

  const saveMutation = useMutation({
    mutationFn: async () => {
      const ratio = Number(draft.ratio)
      const sortOrder = Number(draft.sort_order)
      if (!draft.name.trim() || !Number.isFinite(ratio) || ratio < 0) {
        throw new Error(t('Enter a group name and a non-negative ratio.'))
      }
      if (!Number.isInteger(sortOrder)) {
        throw new Error(t('Sort order must be a whole number.'))
      }
      const payload = {
        old_name: editingName ?? '',
        name: draft.name.trim(),
        description: draft.description.trim(),
        ratio,
        sort_order: sortOrder,
      }
      const response = editingName
        ? await api.put('/api/group/manage', payload)
        : await api.post('/api/group/manage', payload)
      return requireServerSuccess(response.data)
    },
    onSuccess: async () => {
      toast.success(t(editingName ? 'Group updated' : 'Group created'))
      setSheetOpen(false)
      await refreshGroupSurfaces(queryClient)
      onChanged?.()
    },
    onError: (error) => handleServerError(error),
  })

  const deleteMutation = useMutation({
    mutationFn: async (group: GroupEntry) => {
      const response = await api.delete('/api/group/manage', {
        params: { name: group.name },
      })
      return requireServerSuccess(response.data)
    },
    onSuccess: async () => {
      toast.success(t('Group deleted'))
      setDeleteTarget(null)
      await refreshGroupSurfaces(queryClient)
      onChanged?.()
    },
    onError: (error) => handleServerError(error),
  })

  const sortedGroups = useMemo(
    () =>
      [...groups].sort(
        (left, right) =>
          left.sort_order - right.sort_order ||
          left.name.localeCompare(right.name)
      ),
    [groups]
  )

  const startCreate = () => {
    setEditingName(null)
    setDraft({ ...emptyDraft, sort_order: String(groups.length) })
    setSheetOpen(true)
  }

  const startEdit = (group: GroupEntry) => {
    setEditingName(group.name)
    setDraft({
      name: group.name,
      description: group.description,
      ratio: String(group.ratio),
      sort_order: String(group.sort_order),
    })
    setSheetOpen(true)
  }

  return (
    <div className='min-w-0 space-y-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div>
          <h3 className='text-base font-semibold'>{t('Group registry')}</h3>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('Manage group names, descriptions, and billing ratios.')}
          </p>
        </div>
        <Button size='sm' onClick={startCreate}>
          <Plus aria-hidden='true' />
          {t('Add group')}
        </Button>
      </div>

      {groupsQuery.isError ? (
        <div role='alert' className='text-destructive text-sm'>
          {groupsQuery.error.message}
          <Button
            variant='outline'
            size='sm'
            className='ml-2'
            onClick={() => void groupsQuery.refetch()}
          >
            {t('Retry')}
          </Button>
        </div>
      ) : (
        <StaticDataTable
          tableClassName='min-w-[640px]'
          tableProps={{ 'aria-label': t('Group registry') }}
          data={sortedGroups}
          getRowKey={(group) => group.name}
          emptyContent={
            <p className='text-muted-foreground py-8 text-center text-sm'>
              {t('No groups configured')}
            </p>
          }
          columns={[
            {
              id: 'name',
              header: t('Group name'),
              cell: (group) => (
                <span className='font-medium'>{group.name}</span>
              ),
            },
            {
              id: 'description',
              header: t('Description'),
              cell: (group) => (
                <span className='text-muted-foreground'>
                  {group.description || group.name}
                </span>
              ),
            },
            {
              id: 'ratio',
              header: t('Ratio'),
              className: 'w-28 text-right',
              cellClassName: 'text-right tabular-nums',
              cell: (group) => formatNumber(group.ratio, locale),
            },
            {
              id: 'sort_order',
              header: t('Sort order'),
              className: 'w-24 text-right',
              cellClassName: 'text-right tabular-nums',
              cell: (group) => formatNumber(group.sort_order, locale),
            },
            {
              id: 'actions',
              header: t('Actions'),
              className: 'w-24 text-right',
              cellClassName: 'text-right',
              cell: (group) => (
                <div className='flex justify-end gap-1'>
                  <Button
                    variant='ghost'
                    size='icon'
                    aria-label={t('Edit {{group}}', { group: group.name })}
                    onClick={() => startEdit(group)}
                  >
                    <Pencil aria-hidden='true' />
                  </Button>
                  <Button
                    variant='ghost'
                    size='icon'
                    aria-label={t('Delete {{group}}', { group: group.name })}
                    disabled={group.name === 'default'}
                    title={
                      group.name === 'default'
                        ? t('default group cannot be removed or renamed')
                        : undefined
                    }
                    onClick={() => setDeleteTarget(group)}
                  >
                    <Trash2 aria-hidden='true' />
                  </Button>
                </div>
              ),
            },
          ]}
        />
      )}

      <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
        <SheetContent className={sideDrawerContentClassName('sm:max-w-lg')}>
          <SheetHeader className={sideDrawerHeaderClassName()}>
            <SheetTitle>
              {t(editingName ? 'Edit group' : 'Add group')}
            </SheetTitle>
            <SheetDescription>
              {editingName
                ? t(
                    'Renaming updates current group assignments and keeps the old name for historical records.'
                  )
                : t(
                    'New groups become available in user, key, channel, subscription, and route settings.'
                  )}
            </SheetDescription>
          </SheetHeader>
          <div className='flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 py-4'>
            <div className='space-y-2'>
              <Label htmlFor='group-name'>{t('Group name')}</Label>
              <Input
                id='group-name'
                value={draft.name}
                maxLength={64}
                disabled={editingName === 'default'}
                title={
                  editingName === 'default'
                    ? t('default group cannot be removed or renamed')
                    : undefined
                }
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    name: event.target.value,
                  }))
                }
              />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='group-description'>{t('Description')}</Label>
              <Input
                id='group-description'
                value={draft.description}
                maxLength={255}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    description: event.target.value,
                  }))
                }
              />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='group-ratio'>{t('Billing ratio')}</Label>
              <Input
                id='group-ratio'
                type='number'
                min={0}
                step='any'
                value={draft.ratio}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    ratio: event.target.value,
                  }))
                }
              />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='group-sort-order'>{t('Sort order')}</Label>
              <Input
                id='group-sort-order'
                type='number'
                step={1}
                value={draft.sort_order}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    sort_order: event.target.value,
                  }))
                }
              />
            </div>
          </div>
          <SheetFooter className={sideDrawerFooterClassName()}>
            <Button
              variant='outline'
              onClick={() => setSheetOpen(false)}
              disabled={saveMutation.isPending}
            >
              {t('Cancel')}
            </Button>
            <Button
              onClick={() => saveMutation.mutate()}
              disabled={saveMutation.isPending}
            >
              {t(saveMutation.isPending ? 'Saving...' : 'Save')}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t('Delete group')}
        desc={t(
          'Delete {{group}} and remove it from active assignments? Plans and active subscriptions must be reassigned first.',
          { group: deleteTarget?.name ?? '' }
        )}
        confirmText={t('Delete')}
        destructive
        isLoading={deleteMutation.isPending}
        handleConfirm={() => {
          if (deleteTarget) deleteMutation.mutate(deleteTarget)
        }}
      />
    </div>
  )
}

async function refreshGroupSurfaces(
  queryClient: ReturnType<typeof useQueryClient>
) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ['group-registry'] }),
    queryClient.invalidateQueries({ queryKey: ['groups'] }),
    queryClient.invalidateQueries({ queryKey: ['group-history'] }),
    queryClient.invalidateQueries({ queryKey: ['user-groups'] }),
    queryClient.invalidateQueries({ queryKey: ['token-auto-groups'] }),
  ])
}
