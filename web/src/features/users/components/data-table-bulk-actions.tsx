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
import { useQueryClient } from '@tanstack/react-query'
import type { Table } from '@tanstack/react-table'
import { GitBranch, ShieldCheck } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import type { User } from '../types'
import { UserBatchPolicyDialog } from './dialogs/user-batch-policy-dialog'
import { UserBatchRouteDialog } from './dialogs/user-batch-route-dialog'

interface DataTableBulkActionsProps {
  table: Table<User>
}

export function DataTableBulkActions({ table }: DataTableBulkActionsProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [showPolicyDialog, setShowPolicyDialog] = useState(false)
  const [showRouteDialog, setShowRouteDialog] = useState(false)

  const selectedIds = [
    ...new Set(
      table
        .getFilteredSelectedRowModel()
        .flatRows.filter((row) => row.getIsSelected())
        .map((row) => row.original.id)
        .filter((id) => id > 0)
    ),
  ].sort((left, right) => left - right)

  const handleSuccess = () => {
    void queryClient.invalidateQueries({ queryKey: ['users'] })
    table.resetRowSelection()
  }

  return (
    <>
      <BulkActionsToolbar table={table} entityName='user'>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                onClick={() => setShowPolicyDialog(true)}
                className='size-8'
                aria-label={t('Batch update user policy')}
                title={t('Batch update user policy')}
              />
            }
          >
            <ShieldCheck />
            <span className='sr-only'>{t('Batch update user policy')}</span>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Batch update user policy')}</p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                onClick={() => setShowRouteDialog(true)}
                className='size-8'
                aria-label={t('Batch add model route')}
                title={t('Batch add model route')}
              />
            }
          >
            <GitBranch />
            <span className='sr-only'>{t('Batch add model route')}</span>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Batch add model route')}</p>
          </TooltipContent>
        </Tooltip>
      </BulkActionsToolbar>

      <UserBatchPolicyDialog
        open={showPolicyDialog}
        onOpenChange={setShowPolicyDialog}
        userIds={selectedIds}
        onSuccess={handleSuccess}
      />
      <UserBatchRouteDialog
        open={showRouteDialog}
        onOpenChange={setShowRouteDialog}
        userIds={selectedIds}
        onSuccess={handleSuccess}
      />
    </>
  )
}
