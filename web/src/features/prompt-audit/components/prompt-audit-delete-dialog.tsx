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
import { useMutation, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'

import { deletePromptAudits, previewPromptAuditDelete } from '../api'
import type { PromptAuditDeleteFilter } from '../types'

type PromptAuditDeleteDialogProps = {
  open: boolean
  filter: PromptAuditDeleteFilter | null
  onOpenChange: (open: boolean) => void
  onDeleted: () => void
}

export function PromptAuditDeleteDialog({
  open,
  filter,
  onOpenChange,
  onDeleted,
}: PromptAuditDeleteDialogProps) {
  const { t } = useTranslation()
  const previewQuery = useQuery({
    queryKey: ['prompt-audit', 'delete-preview', filter],
    enabled: open && filter !== null,
    queryFn: async () => {
      const result = await previewPromptAuditDelete(filter ?? {})
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to preview deletion'))
      }
      return result.data
    },
  })
  const deleteMutation = useMutation({
    mutationFn: async () => {
      if (!filter || !previewQuery.data) return 0
      const result = await deletePromptAudits(filter, previewQuery.data)
      if (!result.success) {
        throw new Error(result.message || t('Failed to delete audit records'))
      }
      return result.data?.deleted_count ?? 0
    },
    onSuccess: (count) => {
      toast.success(t('Deleted {{count}} audit records', { count }))
      onOpenChange(false)
      onDeleted()
    },
    onError: (error) => toast.error(error.message),
  })

  const preview = previewQuery.data
  const blockedByActive = (preview?.active_count ?? 0) > 0
  const canDelete =
    Boolean(preview) &&
    !blockedByActive &&
    (preview?.eligible_count ?? 0) > 0 &&
    !deleteMutation.isPending

  let previewDescription = t(
    '{{eligible}} terminal records are eligible. {{active}} active tasks are in this scope.',
    {
      eligible: preview?.eligible_count ?? 0,
      active: preview?.active_count ?? 0,
    }
  )
  if (previewQuery.isLoading) {
    previewDescription = t('Calculating the exact deletion scope...')
  } else if (previewQuery.isError) {
    previewDescription = t(
      'The deletion scope could not be calculated. Try again.'
    )
  }

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {t('Delete prompt audit records?')}
          </AlertDialogTitle>
          <AlertDialogDescription>{previewDescription}</AlertDialogDescription>
          {blockedByActive && (
            <p className='text-destructive text-sm'>
              {t(
                'Active tasks cannot be deleted. Narrow the filters and preview again.'
              )}
            </p>
          )}
          {!blockedByActive && preview?.eligible_count === 0 && (
            <p className='text-muted-foreground text-sm'>
              {t('No terminal audit records match this scope.')}
            </p>
          )}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={deleteMutation.isPending}>
            {t('Cancel')}
          </AlertDialogCancel>
          <AlertDialogAction
            variant='destructive'
            disabled={!canDelete}
            onClick={() => deleteMutation.mutate()}
          >
            {deleteMutation.isPending
              ? t('Deleting...')
              : t('Delete permanently')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
