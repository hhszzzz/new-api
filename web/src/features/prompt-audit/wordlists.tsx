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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'

import {
  deletePromptWordlist,
  listPromptWordlists,
  syncPromptWordlist,
  updatePromptWordlist,
} from './api'
import { ManualWordlistDialog } from './components/manual-wordlist-dialog'
import { PromptAuditNavigation } from './components/prompt-audit-navigation'
import { WordlistImportDialog } from './components/wordlist-import-dialog'
import { WordlistTestCard } from './components/wordlist-test-card'
import {
  promptAuditScopeLabel,
  promptWordlistError,
  promptWordlistStatus,
} from './scopes'
import type { PromptWordlist } from './types'

export function PromptWordlists() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [importOpen, setImportOpen] = useState(false)
  const [manualOpen, setManualOpen] = useState(false)
  const [deleting, setDeleting] = useState<PromptWordlist | null>(null)
  const query = useQuery({
    queryKey: ['prompt-audit', 'wordlists'],
    queryFn: listPromptWordlists,
    refetchInterval: 2000,
  })
  const update = useMutation({
    mutationFn: (input: {
      id: string
      enabled?: boolean
      auto_update?: boolean
    }) => updatePromptWordlist(input.id, input),
    onSuccess: () =>
      queryClient.invalidateQueries({
        queryKey: ['prompt-audit', 'wordlists'],
      }),
  })
  const sync = useMutation({
    mutationFn: syncPromptWordlist,
    onSuccess: () =>
      queryClient.invalidateQueries({
        queryKey: ['prompt-audit', 'wordlists'],
      }),
  })
  const remove = useMutation({
    mutationFn: deletePromptWordlist,
    onSuccess: async () => {
      setDeleting(null)
      await queryClient.invalidateQueries({ queryKey: ['prompt-audit'] })
    },
  })
  const columns: StaticDataTableColumn<PromptWordlist>[] = [
    {
      id: 'name',
      header: t('Wordlist'),
      cell: (row) => (
        <div className='max-w-sm space-y-1 py-2'>
          <p className='font-medium'>
            {row.id === 'manual' ? t('Custom wordlist') : row.name}
          </p>
          {row.source_url && (
            <a
              href={row.source_url}
              target='_blank'
              rel='noreferrer'
              className='text-muted-foreground block truncate text-xs'
            >
              {row.source_url}
            </a>
          )}
          {row.content_hash && (
            <p className='text-muted-foreground text-xs'>
              {t('Version')}: {row.content_hash.slice(0, 12)}
            </p>
          )}
        </div>
      ),
    },
    {
      id: 'status',
      header: t('Status'),
      cell: (row) => (
        <div className='max-w-xs space-y-1'>
          <Badge
            variant={row.status === 'failed' ? 'destructive' : 'secondary'}
          >
            {promptWordlistStatus(row.status, t)}
          </Badge>
          <p className='text-xs'>
            {t('{{count}} words', { count: row.word_count })}
          </p>
          {row.last_success_at > 0 && (
            <p className='text-muted-foreground text-xs'>
              {new Date(row.last_success_at * 1000).toLocaleString()}
            </p>
          )}
          {row.last_error && (
            <p className='text-destructive text-xs'>
              {promptWordlistError(row.last_error, t)}{' '}
              {row.word_count > 0
                ? t('The previous successful version is retained.')
                : ''}
            </p>
          )}
        </div>
      ),
    },
    {
      id: 'scopes',
      header: t('Apply to'),
      cell: (row) => (
        <p className='max-w-xs text-xs whitespace-normal'>
          {row.scopes
            .map((scope) => promptAuditScopeLabel(scope, t))
            .join(' · ') || t('Not assigned')}
        </p>
      ),
    },
    {
      id: 'auto_update',
      header: t('Daily updates'),
      cell: (row) =>
        row.id === 'manual' ? (
          '—'
        ) : (
          <Switch
            aria-label={`${t('Daily updates')}: ${row.name}`}
            checked={row.auto_update}
            disabled={update.isPending}
            onCheckedChange={(auto_update) =>
              update.mutate({ id: row.id, auto_update })
            }
          />
        ),
    },
    {
      id: 'enabled',
      header: t('Enabled'),
      cell: (row) => (
        <Switch
          aria-label={`${t('Enabled')}: ${
            row.id === 'manual' ? t('Custom wordlist') : row.name
          }`}
          checked={row.enabled}
          disabled={update.isPending}
          onCheckedChange={(enabled) => update.mutate({ id: row.id, enabled })}
        />
      ),
    },
    {
      id: 'actions',
      header: t('Actions'),
      cell: (row) =>
        row.id === 'manual' ? (
          <Button
            variant='outline'
            size='sm'
            onClick={() => setManualOpen(true)}
          >
            {t('Edit')}
          </Button>
        ) : (
          <div className='flex gap-2'>
            <Button
              variant='outline'
              size='sm'
              disabled={sync.isPending || row.status === 'updating'}
              onClick={() => sync.mutate(row.id)}
            >
              {t('Update now')}
            </Button>
            <Button
              variant='destructive'
              size='sm'
              onClick={() => setDeleting(row)}
            >
              {t('Delete')}
            </Button>
          </div>
        ),
    },
  ]
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Wordlists')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button onClick={() => setImportOpen(true)}>
          {t('Import wordlist')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='mx-auto max-w-6xl space-y-4'>
          <PromptAuditNavigation />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Manage sources without loading their entries. Disable a wordlist to stop all of its assignments; automatic updates pause while disabled.'
            )}
          </p>
          {query.isPending && <LoadingState />}
          {query.isError && (
            <ErrorState
              description={query.error.message}
              onRetry={() => void query.refetch()}
            />
          )}
          {query.isSuccess && (
            <StaticDataTable
              columns={columns}
              data={query.data}
              getRowKey={(row) => row.id}
            />
          )}
          <WordlistTestCard />
        </div>
        {importOpen && (
          <WordlistImportDialog open onOpenChange={setImportOpen} />
        )}
        {manualOpen && (
          <ManualWordlistDialog open onOpenChange={setManualOpen} />
        )}
        <ConfirmDialog
          open={deleting !== null}
          onOpenChange={(open) => {
            if (!open) setDeleting(null)
          }}
          title={t('Delete wordlist')}
          desc={t(
            'The wordlist and its rule assignments will be removed. Audit records are kept.'
          )}
          confirmText={t('Delete')}
          destructive
          isLoading={remove.isPending}
          handleConfirm={() => {
            if (deleting) remove.mutate(deleting.id)
          }}
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
