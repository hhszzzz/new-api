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
import { CirclePlus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import type { PromptAuditEndpointDraft } from '../lib'
import { AuditModelCard } from './audit-model-card'

type AuditModelsSectionProps = {
  endpoints: PromptAuditEndpointDraft[]
  persistedEndpointIDs: Set<string>
  pendingTestID: string | null
  onAdd: (clientKey: string) => void
  onChange: (index: number, update: Partial<PromptAuditEndpointDraft>) => void
  onMove: (index: number, offset: -1 | 1) => void
  onRemove: (index: number) => void
  onTest: (endpoint: PromptAuditEndpointDraft) => void
}

// Section ②: one collapsed row per node. The expanded key lives here because
// the add button and the rows share it — a node added by the user opens
// immediately, and the whole page remounts per save anyway, so no persistence.
export function AuditModelsSection({
  endpoints,
  persistedEndpointIDs,
  pendingTestID,
  onAdd,
  onChange,
  onMove,
  onRemove,
  onTest,
}: AuditModelsSectionProps) {
  const { t } = useTranslation()
  const [openClientKey, setOpenClientKey] = useState<string | null>(
    () => endpoints.at(0)?.client_key ?? null
  )

  const handleAdd = () => {
    const clientKey = crypto.randomUUID()
    setOpenClientKey(clientKey)
    onAdd(clientKey)
  }

  const handleRemove = (index: number) => {
    if (endpoints[index]?.client_key === openClientKey) {
      // Let the successor take over the slot, else the predecessor, else none.
      setOpenClientKey(
        endpoints[index + 1]?.client_key ??
          endpoints[index - 1]?.client_key ??
          null
      )
    }
    onRemove(index)
  }

  return (
    <section className='flex flex-col gap-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <h3 className='text-base font-semibold'>{t('Audit models')}</h3>
        <Button variant='outline' size='sm' onClick={handleAdd}>
          <CirclePlus className='size-4' />
          {t('Add audit model')}
        </Button>
      </div>
      <p className='text-muted-foreground text-xs leading-relaxed'>
        {t(
          'Add an OpenAI-compatible chat model used to review content, for example a Qwen3Guard deployment. Models are tried in order; tokens are write-only, so leave the field untouched to preserve the saved token. Save first, then test.'
        )}
      </p>

      {endpoints.length === 0 ? (
        <p className='text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm'>
          {t('No audit models configured')}
        </p>
      ) : (
        <div className='flex flex-col gap-2'>
          {endpoints.map((endpoint, index) => {
            let tokenDescription = t('No token is stored for this audit model.')
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
            if (pendingTestID === endpoint.id) {
              testLabel = t('Testing...')
            } else if (persistedEndpointIDs.has(endpoint.id)) {
              testLabel = t('Test saved audit model')
            }

            return (
              <AuditModelCard
                key={endpoint.client_key}
                endpoint={endpoint}
                index={index}
                isFirst={index === 0}
                isLast={index === endpoints.length - 1}
                open={openClientKey === endpoint.client_key}
                onOpenChange={(open) =>
                  setOpenClientKey(open ? endpoint.client_key : null)
                }
                onChange={(update) => onChange(index, update)}
                onMoveUp={() => onMove(index, -1)}
                onMoveDown={() => onMove(index, 1)}
                onRemove={() => handleRemove(index)}
                canTest={
                  persistedEndpointIDs.has(endpoint.id) &&
                  pendingTestID !== endpoint.id
                }
                testLabel={testLabel}
                onTest={() => onTest(endpoint)}
                tokenDescription={tokenDescription}
              />
            )
          })}
        </div>
      )}
    </section>
  )
}
