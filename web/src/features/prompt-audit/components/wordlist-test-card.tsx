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
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'

import { testPromptWordlists } from '../api'
import { PROMPT_AUDIT_SCOPES, promptAuditScopeLabel } from '../scopes'
import type { PromptAuditScope } from '../types'

export function WordlistTestCard() {
  const { t } = useTranslation()
  const [scope, setScope] = useState<PromptAuditScope>('user')
  const [text, setText] = useState('')
  const test = useMutation({
    mutationFn: () => testPromptWordlists(scope, text),
    meta: { errorToast: false },
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Test inspection rules')}</CardTitle>
        <CardDescription>
          {t(
            'Test saved wordlist rules without storing the text or calling a model.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-3'>
        <div className='space-y-1.5'>
          <Label htmlFor='wordlist-test-scope'>{t('Text source')}</Label>
          <NativeSelect
            id='wordlist-test-scope'
            value={scope}
            disabled={test.isPending}
            onChange={(event) => {
              setScope(event.target.value as PromptAuditScope)
              test.reset()
            }}
          >
            {PROMPT_AUDIT_SCOPES.map((value) => (
              <NativeSelectOption key={value} value={value}>
                {promptAuditScopeLabel(value, t)}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
        <div className='space-y-1.5'>
          <Label htmlFor='wordlist-test-text'>{t('Test text')}</Label>
          <Textarea
            id='wordlist-test-text'
            value={text}
            maxLength={16384}
            disabled={test.isPending}
            onChange={(event) => {
              setText(event.target.value)
              test.reset()
            }}
          />
        </div>
        <Button
          disabled={!text.trim() || test.isPending}
          onClick={() => test.mutate()}
        >
          {test.isPending ? t('Testing...') : t('Test rules')}
        </Button>
        {test.isError && (
          <p role='alert' className='text-destructive text-sm'>
            {test.error.message}
          </p>
        )}
        {test.data && (
          <div role='status' className='space-y-1 text-sm'>
            <p>
              {test.data.match
                ? t('Matched wordlist: {{name}}', {
                    name:
                      test.data.match.id === 'manual'
                        ? t('Custom wordlist')
                        : test.data.match.name,
                  })
                : t('No wordlist matched')}
            </p>
            <p className='text-muted-foreground'>
              {test.data.model_audit
                ? t('Model review is selected for this source.')
                : t('Model review is inactive for this source.')}
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
