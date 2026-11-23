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
import { Download, Ban, Info } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationNext,
  PaginationPrevious,
} from '@/components/ui/pagination'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  disableUnusedRedemptionBatch,
  exportRedemptionBatch,
  getRedemptionBatch,
  getRedemptionBatches,
} from '../api'
import { REDEMPTION_STATUSES, REDEMPTION_TYPES } from '../constants'
import { isRedemptionExpired } from '../lib'
import type { RedemptionBatch } from '../types'

export function RedemptionBatches() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [pendingBatch, setPendingBatch] = useState<RedemptionBatch | null>(null)
  const [selectedBatchId, setSelectedBatchId] = useState<number | null>(null)
  const pageSize = 50
  const {
    data,
    isLoading,
    isFetching,
    isError,
    refetch: refetchBatches,
  } = useQuery({
    queryKey: ['redemption-batches', page],
    queryFn: async () =>
      requireServerSuccess(
        await getRedemptionBatches({ p: page, page_size: pageSize })
      ),
    placeholderData: (previousData) => previousData,
  })
  const {
    data: detailResult,
    isLoading: isLoadingDetail,
    isError: isDetailError,
    refetch: refetchDetail,
  } = useQuery({
    queryKey: ['redemption-batch', selectedBatchId],
    queryFn: async () => {
      if (selectedBatchId === null) {
        throw new Error('A batch must be selected')
      }
      return requireServerSuccess(await getRedemptionBatch(selectedBatchId))
    },
    enabled: selectedBatchId !== null,
  })
  const disableMutation = useMutation({
    mutationFn: disableUnusedRedemptionBatch,
    onSuccess: async (result, batchId) => {
      if (!result.success) {
        handleServerError(result, t('Failed to disable unused codes'))
        return
      }
      toast.success(
        t('Disabled {{count}} unused codes', { count: result.data ?? 0 })
      )
      setPendingBatch(null)
      await queryClient.invalidateQueries({ queryKey: ['redemption-batches'] })
      await queryClient.invalidateQueries({
        queryKey: ['redemption-batch', batchId],
      })
      await queryClient.invalidateQueries({ queryKey: ['redemptions'] })
    },
    onError: (error) =>
      handleServerError(error, t('Failed to disable unused codes')),
  })

  const batches = data?.data?.items ?? []
  const totalBatches = data?.data?.total ?? 0
  const totalPages = Math.ceil(totalBatches / pageSize)

  return (
    <section className='flex h-full min-h-0 flex-col gap-3'>
      <h2 className='text-sm font-semibold'>{t('Redemption batches')}</h2>
      {isLoading && (
        <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
      )}
      {isError && <ErrorState onRetry={() => void refetchBatches()} />}
      {!isLoading && !isError && batches.length === 0 && (
        <p className='text-muted-foreground text-sm'>
          {t('No redemption batches found')}
        </p>
      )}
      {!isLoading && !isError && batches.length > 0 && (
        <div className='flex min-h-0 flex-1 flex-col gap-3'>
          <div className='min-h-0 flex-1 overflow-auto'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Batch')}</TableHead>
                  <TableHead>{t('Code type')}</TableHead>
                  <TableHead>{t('Created')}</TableHead>
                  <TableHead>{t('Available')}</TableHead>
                  <TableHead>{t('Used')}</TableHead>
                  <TableHead>{t('Disabled')}</TableHead>
                  <TableHead>{t('Expired')}</TableHead>
                  <TableHead className='text-right'>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {batches.map((batch) => {
                  const type = REDEMPTION_TYPES.find(
                    (item) => item.value === batch.type
                  )
                  return (
                    <TableRow key={batch.id}>
                      <TableCell>
                        <button
                          type='button'
                          className='text-start font-medium underline-offset-4 hover:underline'
                          onClick={() => setSelectedBatchId(batch.id)}
                        >
                          {batch.name}
                        </button>
                        <div className='text-muted-foreground font-mono text-xs'>
                          #{batch.id}
                        </div>
                      </TableCell>
                      <TableCell>
                        {type ? t(type.labelKey) : batch.type}
                      </TableCell>
                      <TableCell>
                        {formatTimestampToDate(batch.created_at)}
                      </TableCell>
                      <TableCell>{batch.available_count}</TableCell>
                      <TableCell>{batch.used_count}</TableCell>
                      <TableCell>{batch.disabled_count}</TableCell>
                      <TableCell>{batch.expired_count}</TableCell>
                      <TableCell>
                        <div className='flex justify-end gap-1'>
                          <Button
                            type='button'
                            size='icon'
                            variant='ghost'
                            title={t('View batch details')}
                            aria-label={t('View batch details')}
                            onClick={() => setSelectedBatchId(batch.id)}
                          >
                            <Info className='h-4 w-4' />
                          </Button>
                          <Button
                            type='button'
                            size='icon'
                            variant='ghost'
                            title={t('Export batch codes')}
                            aria-label={t('Export batch codes')}
                            onClick={() =>
                              void exportRedemptionBatch(batch.id).catch(
                                (error: unknown) => handleServerError(error)
                              )
                            }
                          >
                            <Download className='h-4 w-4' />
                          </Button>
                          {batch.available_count + batch.expired_count > 0 && (
                            <Button
                              type='button'
                              size='icon'
                              variant='ghost'
                              title={t('Disable unused codes')}
                              aria-label={t('Disable unused codes')}
                              onClick={() => setPendingBatch(batch)}
                            >
                              <Ban className='h-4 w-4' />
                            </Button>
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>
          {totalPages > 1 && (
            <div className='flex flex-wrap items-center justify-between gap-3 border-t pt-3'>
              <span className='text-muted-foreground text-xs'>
                {t('{{total}} records · Page {{page}} of {{pages}}', {
                  total: totalBatches,
                  page,
                  pages: totalPages,
                })}
              </span>
              <Pagination className='mx-0 w-auto justify-end'>
                <PaginationContent>
                  <PaginationItem>
                    <PaginationPrevious
                      href='#'
                      text={t('Previous page')}
                      aria-disabled={page <= 1 || isFetching}
                      tabIndex={page <= 1 || isFetching ? -1 : undefined}
                      onClick={(event) => {
                        event.preventDefault()
                        if (page > 1 && !isFetching) setPage(page - 1)
                      }}
                    />
                  </PaginationItem>
                  <PaginationItem>
                    <PaginationNext
                      href='#'
                      text={t('Next page')}
                      aria-disabled={page >= totalPages || isFetching}
                      tabIndex={
                        page >= totalPages || isFetching ? -1 : undefined
                      }
                      onClick={(event) => {
                        event.preventDefault()
                        if (page < totalPages && !isFetching) setPage(page + 1)
                      }}
                    />
                  </PaginationItem>
                </PaginationContent>
              </Pagination>
            </div>
          )}
        </div>
      )}
      <ConfirmDialog
        destructive
        open={pendingBatch !== null}
        onOpenChange={(open) => !open && setPendingBatch(null)}
        handleConfirm={() =>
          pendingBatch && disableMutation.mutate(pendingBatch.id)
        }
        isLoading={disableMutation.isPending}
        title={t('Disable unused codes?')}
        desc={t('This disables all unused codes in batch {{name}}.', {
          name: pendingBatch?.name ?? '',
        })}
        confirmText={t('Disable unused codes')}
      />
      <Dialog
        open={selectedBatchId !== null}
        onOpenChange={(open) => !open && setSelectedBatchId(null)}
        title={detailResult?.data?.name ?? t('Redemption batch')}
        description={
          detailResult?.data
            ? t('Batch ID: {{id}}', { id: detailResult.data.id })
            : undefined
        }
        contentClassName='sm:max-w-3xl'
        bodyClassName='space-y-4'
      >
        {isLoadingDetail && (
          <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
        )}
        {!isLoadingDetail && isDetailError && (
          <ErrorState onRetry={() => void refetchDetail()} />
        )}
        {!isLoadingDetail && !isDetailError && (
          <>
            {detailResult?.data && (
              <div className='flex flex-wrap gap-x-5 gap-y-2 border-b pb-3 text-sm'>
                <span>
                  {t('Count')}: {detailResult.data.count}
                </span>
                <span>
                  {t('Expires')}:{' '}
                  {detailResult.data.expired_time === 0
                    ? t('Never')
                    : formatTimestampToDate(detailResult.data.expired_time)}
                </span>
                <span>
                  {t('Available')}: {detailResult.data.available_count}
                </span>
                <span>
                  {t('Used')}: {detailResult.data.used_count}
                </span>
                <span>
                  {t('Disabled')}: {detailResult.data.disabled_count}
                </span>
                <span>
                  {t('Expired')}: {detailResult.data.expired_count}
                </span>
              </div>
            )}
            <div className='max-h-[55vh] overflow-auto'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('ID')}</TableHead>
                    <TableHead>{t('Redemption Code')}</TableHead>
                    <TableHead>{t('Status')}</TableHead>
                    <TableHead>{t('Redeemed By')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(detailResult?.data?.redemptions ?? []).map((code) => (
                    <TableRow key={code.id}>
                      <TableCell>{code.id}</TableCell>
                      <TableCell className='font-mono text-xs'>
                        {code.key}
                      </TableCell>
                      <TableCell>
                        {isRedemptionExpired(code.expired_time, code.status)
                          ? t('Expired')
                          : t(
                              REDEMPTION_STATUSES[code.status]?.labelKey ??
                                'Unknown'
                            )}
                      </TableCell>
                      <TableCell>{code.used_user_id || '-'}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </>
        )}
      </Dialog>
    </section>
  )
}
