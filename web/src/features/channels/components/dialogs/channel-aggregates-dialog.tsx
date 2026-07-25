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
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

import {
  createChannelAggregate,
  deleteChannelAggregate,
  getChannelAggregates,
  updateChannelAggregate,
} from '../../api'
import type { ChannelAggregate, ChannelAggregateInput } from '../../types'

type Props = { open: boolean; onOpenChange: (open: boolean) => void }

const emptyForm: ChannelAggregateInput = { name: '', base_url: '', remark: '' }

export function ChannelAggregatesDialog(props: Props) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const currentUser = useAuthStore((state) => state.auth.user)
  const canEditSensitive = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.SENSITIVE_WRITE
  )
  const query = useQuery({
    queryKey: ['channel-aggregates'],
    queryFn: getChannelAggregates,
    enabled: props.open,
  })
  const [form, setForm] = useState<ChannelAggregateInput>(emptyForm)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<ChannelAggregate | null>(
    null
  )
  const mutation = useMutation({
    mutationFn: async () => {
      if (!canEditSensitive) {
        throw new Error(
          t('You do not have permission to edit sensitive channel settings.')
        )
      }
      const payload = {
        name: form.name.trim(),
        base_url: form.base_url.trim(),
        remark: form.remark.trim(),
      }
      if (!payload.name) throw new Error(t('Aggregate name is required'))
      if (editingId) return updateChannelAggregate(editingId, payload)
      return createChannelAggregate(payload)
    },
    onSuccess: (response) => {
      if (!response.success) {
        toast.error(response.message || t('Operation failed'))
        return
      }
      toast.success(
        editingId
          ? t('Channel aggregate updated')
          : t('Channel aggregate created')
      )
      setForm(emptyForm)
      setEditingId(null)
      queryClient.invalidateQueries({ queryKey: ['channel-aggregates'] })
      queryClient.invalidateQueries({ queryKey: ['channels'] })
    },
    onError: (error: Error) =>
      toast.error(error.message || t('Operation failed')),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: number) => {
      if (!canEditSensitive) {
        throw new Error(
          t('You do not have permission to edit sensitive channel settings.')
        )
      }
      return deleteChannelAggregate(id)
    },
    onSuccess: (response) => {
      if (!response.success) {
        toast.error(response.message || t('Operation failed'))
        return
      }
      toast.success(t('Aggregate detached; child channels were kept'))
      setDeleteTarget(null)
      queryClient.invalidateQueries({ queryKey: ['channel-aggregates'] })
      queryClient.invalidateQueries({ queryKey: ['channels'] })
    },
    onError: (error: Error) =>
      toast.error(error.message || t('Operation failed')),
  })

  useEffect(() => {
    if (!props.open) {
      setForm(emptyForm)
      setEditingId(null)
      setDeleteTarget(null)
    }
  }, [props.open])

  const beginEdit = (aggregate: ChannelAggregate) => {
    if (!canEditSensitive) return
    setEditingId(aggregate.id)
    setForm({
      name: aggregate.name,
      base_url: aggregate.base_url,
      remark: aggregate.remark,
    })
  }

  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={props.onOpenChange}
        title={t('Channel aggregates')}
        description={t(
          'Group channels that share an endpoint. Keys, health, routing, and billing remain independent per child channel.'
        )}
        contentClassName='max-w-3xl'
        bodyClassName='space-y-5'
        footer={
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Close')}
          </Button>
        }
      >
        <Alert>
          <AlertDescription>
            {t(
              'Deleting an aggregate only detaches its children. If they inherited the shared URL, that URL is copied to each child before detaching.'
            )}
          </AlertDescription>
        </Alert>
        {!canEditSensitive && (
          <Alert>
            <AlertDescription>
              {t(
                'You do not have permission to edit sensitive channel settings.'
              )}
            </AlertDescription>
          </Alert>
        )}
        <div className='space-y-3'>
          <div className='flex items-center justify-between gap-2'>
            <h3 className='font-medium'>
              {editingId ? t('Edit aggregate') : t('New aggregate')}
            </h3>
            {editingId && (
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={() => {
                  setEditingId(null)
                  setForm(emptyForm)
                }}
              >
                {t('Cancel edit')}
              </Button>
            )}
          </div>
          <div className='grid gap-3 sm:grid-cols-2'>
            <Input
              disabled={!canEditSensitive}
              value={form.name}
              placeholder={t('Aggregate name')}
              onChange={(event) =>
                setForm((current) => ({ ...current, name: event.target.value }))
              }
            />
            <Input
              disabled={!canEditSensitive}
              value={form.base_url}
              placeholder={t('Shared base URL (optional)')}
              onChange={(event) =>
                setForm((current) => ({
                  ...current,
                  base_url: event.target.value,
                }))
              }
            />
          </div>
          <Textarea
            disabled={!canEditSensitive}
            value={form.remark}
            placeholder={t('Remark (optional)')}
            onChange={(event) =>
              setForm((current) => ({ ...current, remark: event.target.value }))
            }
          />
          <Button
            type='button'
            onClick={() => mutation.mutate()}
            disabled={!canEditSensitive || mutation.isPending}
          >
            {editingId ? (
              <Pencil className='h-4 w-4' />
            ) : (
              <Plus className='h-4 w-4' />
            )}
            {editingId ? t('Save aggregate') : t('Create aggregate')}
          </Button>
        </div>
        <div className='space-y-2'>
          <h3 className='font-medium'>{t('Existing aggregates')}</h3>
          {query.isLoading ? (
            <div className='text-muted-foreground text-sm'>
              {t('Loading...')}
            </div>
          ) : null}
          {!query.isLoading && (query.data?.data ?? []).length === 0 ? (
            <div className='text-muted-foreground text-sm'>
              {t('No channel aggregates')}
            </div>
          ) : null}
          {(query.data?.data ?? []).map((aggregate) => (
            <div
              key={aggregate.id}
              className='flex items-center justify-between gap-3 rounded-lg border p-3'
            >
              <div className='min-w-0'>
                <div className='flex items-center gap-2 font-medium'>
                  <span className='truncate'>{aggregate.name}</span>
                  <span className='text-muted-foreground text-xs'>
                    {t('{{count}} child channels', {
                      count: aggregate.child_count,
                    })}
                  </span>
                </div>
                {aggregate.base_url && (
                  <div className='text-muted-foreground truncate text-xs'>
                    {aggregate.base_url}
                  </div>
                )}
                {aggregate.remark && (
                  <div className='text-muted-foreground truncate text-xs'>
                    {aggregate.remark}
                  </div>
                )}
              </div>
              <div className='flex shrink-0 gap-1'>
                <Button
                  disabled={!canEditSensitive}
                  variant='ghost'
                  size='icon'
                  onClick={() => beginEdit(aggregate)}
                  aria-label={t('Edit aggregate')}
                >
                  <Pencil className='h-4 w-4' />
                </Button>
                <Button
                  disabled={!canEditSensitive}
                  variant='ghost'
                  size='icon'
                  className='text-destructive'
                  onClick={() => setDeleteTarget(aggregate)}
                  aria-label={t('Delete aggregate')}
                >
                  <Trash2 className='h-4 w-4' />
                </Button>
              </div>
            </div>
          ))}
        </div>
      </Dialog>
      <ConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t('Detach channel aggregate?')}
        desc={t(
          'This only removes the parent relationship and keeps all child channels. Continue?'
        )}
        destructive
        confirmText={t('Detach')}
        isLoading={deleteMutation.isPending}
        handleConfirm={() => {
          if (canEditSensitive && deleteTarget) {
            deleteMutation.mutate(deleteTarget.id)
          }
        }}
      />
    </>
  )
}
