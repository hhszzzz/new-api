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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Separator } from '@/components/ui/separator'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { promptAuditPayloadSources } from '../lib'
import { promptAuditScopeLabel } from '../scopes'

/** A stored scan snapshot, not the raw request or a reconstructed conversation. */
export function PromptAuditPayloadView(props: {
  payload?: string
  truncated?: boolean
  background?: boolean
}) {
  const { t } = useTranslation()
  const sources = promptAuditPayloadSources(props.payload)
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const activeSource =
    sources.find((source) => source.key === selectedKey) ?? sources[0]
  return (
    <>
      {!activeSource ? (
        <p className='text-muted-foreground p-3 text-xs'>
          {t('No inspected content retained')}
        </p>
      ) : (
        <Tabs
          value={activeSource.key}
          onValueChange={(value) => setSelectedKey(String(value))}
          className='min-w-0 gap-0'
        >
          <div className='flex min-w-0 items-center gap-2 px-3 py-2'>
            <div className='min-w-0 flex-1 overflow-x-auto'>
              <TabsList className='min-w-max justify-start'>
                {sources.map((source) => {
                  let label = t('Unknown source')
                  if (source.scope) {
                    label = promptAuditScopeLabel(source.scope, t)
                  }
                  if (props.background && source.scope === 'user') {
                    label = t('User-role content')
                  }
                  return (
                    <TabsTrigger key={source.key} value={source.key}>
                      {label}
                    </TabsTrigger>
                  )
                })}
              </TabsList>
            </div>
            <CopyButton
              value={activeSource.blocks.join('\n\n')}
              variant='ghost'
              size='sm'
              tooltip={t('Copy')}
            />
          </div>
          {sources.map((source) => (
            <TabsContent
              key={source.key}
              value={source.key}
              tabIndex={0}
              className='focus-visible:ring-ring/50 max-h-80 min-w-0 overflow-y-auto overscroll-contain p-3 focus-visible:ring-2 focus-visible:ring-inset'
            >
              {Array.from(source.blocks.entries(), ([position, text]) => (
                <div key={`${source.key}-${position}`}>
                  {position > 0 && <Separator className='my-3' />}
                  <pre className='font-mono text-sm leading-relaxed [overflow-wrap:anywhere] break-words whitespace-pre-wrap'>
                    {text}
                  </pre>
                </div>
              ))}
            </TabsContent>
          ))}
        </Tabs>
      )}
      {props.truncated && (
        <p className='text-muted-foreground border-t px-3 py-2 text-xs'>
          {t('Inspected content was truncated to the retention limit.')}
        </p>
      )}
    </>
  )
}
