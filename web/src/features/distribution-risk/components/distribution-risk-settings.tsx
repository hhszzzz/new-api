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

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import type {
  DistributionRiskConfig,
  DistributionRiskConfigUpdate,
} from '../types'

type NumericSetting = {
  key: keyof Pick<
    DistributionRiskConfigUpdate,
    | 'ip_floor'
    | 'user_agent_floor'
    | 'session_floor'
    | 'requests_per_minute_floor'
    | 'concurrency_floor'
    | 'min_requests'
    | 'min_active_days'
  >
  label: string
  min: number
  max: number
}

const numericSettings: NumericSetting[] = [
  { key: 'ip_floor', label: 'IP diversity floor', min: 2, max: 1000 },
  {
    key: 'user_agent_floor',
    label: 'User-agent diversity floor',
    min: 2,
    max: 1000,
  },
  {
    key: 'session_floor',
    label: 'Session diversity floor',
    min: 2,
    max: 10000,
  },
  {
    key: 'requests_per_minute_floor',
    label: 'Requests per minute floor',
    min: 10,
    max: 100000,
  },
  {
    key: 'concurrency_floor',
    label: 'Concurrent requests floor',
    min: 2,
    max: 10000,
  },
  {
    key: 'min_requests',
    label: 'Minimum baseline requests',
    min: 100,
    max: 1000000,
  },
  {
    key: 'min_active_days',
    label: 'Minimum active baseline days',
    min: 3,
    max: 30,
  },
]

type Props = {
  config: DistributionRiskConfig
  isSaving: boolean
  onSave: (update: DistributionRiskConfigUpdate) => void
}

export function DistributionRiskSettings(props: Props) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<DistributionRiskConfigUpdate>({
    enabled: props.config.enabled,
    ip_floor: props.config.ip_floor,
    user_agent_floor: props.config.user_agent_floor,
    session_floor: props.config.session_floor,
    requests_per_minute_floor: props.config.requests_per_minute_floor,
    concurrency_floor: props.config.concurrency_floor,
    min_requests: props.config.min_requests,
    min_active_days: props.config.min_active_days,
  })
  return (
    <section className='space-y-4 border-b pb-5'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div className='flex items-center gap-3'>
          <Switch
            checked={draft.enabled}
            onCheckedChange={(enabled) =>
              setDraft((current) => ({ ...current, enabled }))
            }
            aria-label={t('Enable distribution risk detection')}
          />
          <div>
            <h3 className='text-sm font-semibold'>{t('Detection settings')}</h3>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Enabling starts a seven-day shadow period before enforcement.'
              )}
            </p>
          </div>
        </div>
        <div className='flex items-center gap-2'>
          <span className='text-muted-foreground text-sm'>
            {t('Mode')}: {t(modeLabel(props.config.mode))}
          </span>
          <Button
            disabled={props.isSaving}
            onClick={() => props.onSave(draft)}
            size='sm'
          >
            {props.isSaving ? t('Saving...') : t('Save settings')}
          </Button>
        </div>
      </div>
      <div className='grid grid-cols-1 gap-x-5 gap-y-3 sm:grid-cols-2 lg:grid-cols-4'>
        {numericSettings.map((field) => (
          <label className='space-y-1.5' key={field.key}>
            <span className='text-xs font-medium'>{t(field.label)}</span>
            <Input
              type='number'
              min={field.min}
              max={field.max}
              value={draft[field.key]}
              onChange={(event) =>
                setDraft((current) => ({
                  ...current,
                  [field.key]: Number(event.target.value),
                }))
              }
            />
          </label>
        ))}
      </div>
    </section>
  )
}

function modeLabel(mode: DistributionRiskConfig['mode']) {
  if (mode === 'enforce') return 'Enforcing'
  if (mode === 'shadow') return 'Shadow'
  return 'Disabled'
}
