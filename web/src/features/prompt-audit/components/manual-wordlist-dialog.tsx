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

import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

import { getManualPromptWordlist, updateManualPromptWordlist } from '../api'

export function ManualWordlistDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<string | null>(null)
  const query = useQuery({
    queryKey: ['prompt-audit', 'manual-words'],
    queryFn: getManualPromptWordlist,
    enabled: props.open,
  })
  const save = useMutation({
    mutationFn: updateManualPromptWordlist,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['prompt-audit'] })
      props.onOpenChange(false)
    },
  })
  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Custom wordlist')}
      description={t('Enter one keyword per line')}
      footer={
        <Button
          disabled={query.isPending || query.isError || save.isPending}
          onClick={() => save.mutate(draft ?? query.data ?? '')}
        >
          {t('Save')}
        </Button>
      }
    >
      {query.isPending && <LoadingState />}
      {query.isError && (
        <ErrorState
          description={query.error.message}
          onRetry={() => void query.refetch()}
        />
      )}
      {query.isSuccess && (
        <div className='space-y-2'>
          <Label htmlFor='manual-wordlist-content'>
            {t('Blocked keywords')}
          </Label>
          <Textarea
            id='manual-wordlist-content'
            rows={14}
            value={draft ?? query.data}
            onChange={(event) => setDraft(event.target.value)}
          />
        </div>
      )}
    </Dialog>
  )
}
