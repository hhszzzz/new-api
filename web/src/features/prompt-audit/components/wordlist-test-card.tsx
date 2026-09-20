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

import { testPromptAuditPolicy } from '../api'
import { PROMPT_AUDIT_SCOPES, promptAuditScopeLabel } from '../scopes'
import type { PromptAuditDirection, PromptAuditScope } from '../types'

export function WordlistTestCard() {
  const { t } = useTranslation()
  const [scope, setScope] = useState<PromptAuditScope>('user')
  const [direction, setDirection] = useState<PromptAuditDirection>('input')
  const [text, setText] = useState('')
  const [output, setOutput] = useState('')
  const test = useMutation({
    mutationFn: async () => {
      const result = await testPromptAuditPolicy({
        direction,
        segments: [
          {
            role: scope,
            scope,
            user: scope === 'user' || scope === 'task',
            text,
          },
        ],
        output: direction === 'output' ? output : undefined,
      })
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Inspection test failed'))
      }
      return result.data
    },
    meta: { errorToast: false },
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Test inspection rules')}</CardTitle>
        <CardDescription>
          {t(
            'Preview the complete saved policy without forwarding a generation request, charging quota, or storing the test text.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-3'>
        <div className='space-y-1.5'>
          <Label htmlFor='prompt-audit-test-direction'>
            {t('Audit stage')}
          </Label>
          <NativeSelect
            id='prompt-audit-test-direction'
            value={direction}
            disabled={test.isPending}
            onChange={(event) => {
              setDirection(event.target.value as PromptAuditDirection)
              test.reset()
            }}
          >
            <NativeSelectOption value='input'>
              {t('Request input')}
            </NativeSelectOption>
            <NativeSelectOption value='output'>
              {t('Generated output')}
            </NativeSelectOption>
          </NativeSelect>
        </div>
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
        {direction === 'output' && (
          <div className='space-y-1.5'>
            <Label htmlFor='wordlist-test-output'>{t('Generated reply')}</Label>
            <Textarea
              id='wordlist-test-output'
              value={output}
              maxLength={65536}
              disabled={test.isPending}
              onChange={(event) => {
                setOutput(event.target.value)
                test.reset()
              }}
            />
          </div>
        )}
        <Button
          disabled={
            !text.trim() ||
            (direction === 'output' && !output.trim()) ||
            test.isPending
          }
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
              {test.data.wordlist
                ? t('Matched wordlist: {{name}}', {
                    name:
                      test.data.wordlist.id === 'manual'
                        ? t('Custom wordlist')
                        : test.data.wordlist.name,
                  })
                : t('No wordlist matched')}
            </p>
            <p className='text-muted-foreground'>
              {t('Decision')}: {t(test.data.decision || 'pass')} ·{' '}
              {test.data.safety || '—'}
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
