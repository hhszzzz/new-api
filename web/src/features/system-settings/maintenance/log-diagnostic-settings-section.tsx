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
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

type LogDiagnosticDefaults = {
  'log_diagnostic_setting.record_ip': boolean
  'log_diagnostic_setting.record_headers': boolean
  'log_diagnostic_setting.extra_headers': string[]
}

type Props = { defaultValues: LogDiagnosticDefaults }

function normalizeHeaders(headers: string[]): string[] {
  return [
    ...new Set(headers.map((header) => header.trim().toLowerCase())),
  ].filter(Boolean)
}

export function LogDiagnosticSettingsSection(props: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [recordIp, setRecordIp] = useState(
    props.defaultValues['log_diagnostic_setting.record_ip']
  )
  const [recordHeaders, setRecordHeaders] = useState(
    props.defaultValues['log_diagnostic_setting.record_headers']
  )
  const [headers, setHeaders] = useState<string[]>(
    normalizeHeaders(
      props.defaultValues['log_diagnostic_setting.extra_headers']
    )
  )
  const [newHeader, setNewHeader] = useState('')
  const baseline = useMemo(
    () => ({
      recordIp: props.defaultValues['log_diagnostic_setting.record_ip'],
      recordHeaders:
        props.defaultValues['log_diagnostic_setting.record_headers'],
      headers: normalizeHeaders(
        props.defaultValues['log_diagnostic_setting.extra_headers']
      ),
    }),
    [props.defaultValues]
  )

  useEffect(() => {
    setRecordIp(baseline.recordIp)
    setRecordHeaders(baseline.recordHeaders)
    setHeaders(baseline.headers)
  }, [baseline])

  const addHeader = () => {
    const normalized = newHeader.trim().toLowerCase()
    if (!normalized || headers.includes(normalized) || headers.length >= 16) {
      return
    }
    if (!/^[a-z0-9_-]+$/.test(normalized)) return
    setHeaders((current) => [...current, normalized])
    setNewHeader('')
  }

  const onSubmit = async () => {
    const normalizedHeaders = normalizeHeaders(headers).slice(0, 16)
    const updates: Array<{ key: string; value: string | boolean }> = []
    if (recordIp !== baseline.recordIp) {
      updates.push({
        key: 'log_diagnostic_setting.record_ip',
        value: recordIp,
      })
    }
    if (recordHeaders !== baseline.recordHeaders) {
      updates.push({
        key: 'log_diagnostic_setting.record_headers',
        value: recordHeaders,
      })
    }
    if (
      JSON.stringify(normalizedHeaders) !== JSON.stringify(baseline.headers)
    ) {
      updates.push({
        key: 'log_diagnostic_setting.extra_headers',
        value: JSON.stringify(normalizedHeaders),
      })
    }
    for (const update of updates) await updateOption.mutateAsync(update)
  }

  return (
    <SettingsSection title={t('Log Diagnostics')}>
      <Alert>
        <AlertDescription>
          {t(
            'Request bodies are never recorded. Authorization, cookies, API keys, and proxy credentials are always excluded.'
          )}
        </AlertDescription>
      </Alert>
      <SettingsForm
        onSubmit={(event) => {
          event.preventDefault()
          void onSubmit()
        }}
      >
        <SettingsPageFormActions
          onSave={() => void onSubmit()}
          isSaving={updateOption.isPending}
        />
        <SettingsSwitchItem>
          <SettingsSwitchContent>
            <div className='text-sm font-medium'>{t('Record client IP')}</div>
            <div className='text-muted-foreground text-xs'>
              {t(
                'Keep full IP addresses in usage logs; the IP column remains hidden by default.'
              )}
            </div>
          </SettingsSwitchContent>
          <Switch checked={recordIp} onCheckedChange={setRecordIp} />
        </SettingsSwitchItem>
        <SettingsSwitchItem>
          <SettingsSwitchContent>
            <div className='text-sm font-medium'>
              {t('Record safe request headers')}
            </div>
            <div className='text-muted-foreground text-xs'>
              {t('Only a small allowlist of diagnostic headers is retained.')}
            </div>
          </SettingsSwitchContent>
          <Switch checked={recordHeaders} onCheckedChange={setRecordHeaders} />
        </SettingsSwitchItem>
        <div className='space-y-3 lg:col-span-2'>
          <div>
            <div className='text-sm font-medium'>
              {t('Additional safe headers')}
            </div>
            <div className='text-muted-foreground text-xs'>
              {t(
                'Up to 16 names; each value is limited by the server and sensitive names are rejected.'
              )}
            </div>
          </div>
          <div className='flex gap-2'>
            <Input
              value={newHeader}
              onChange={(event) => setNewHeader(event.target.value)}
              placeholder={t('e.g. x-request-id')}
              onKeyDown={(event) => {
                if (event.key === 'Enter') {
                  event.preventDefault()
                  addHeader()
                }
              }}
            />
            <Button
              type='button'
              variant='outline'
              onClick={addHeader}
              disabled={headers.length >= 16}
            >
              {t('Add')}
            </Button>
          </div>
          <div className='flex flex-wrap gap-2'>
            {headers.map((header) => (
              <Button
                key={header}
                type='button'
                variant='secondary'
                size='sm'
                onClick={() =>
                  setHeaders((current) =>
                    current.filter((item) => item !== header)
                  )
                }
              >
                {header} ×
              </Button>
            ))}
          </div>
        </div>
      </SettingsForm>
    </SettingsSection>
  )
}
