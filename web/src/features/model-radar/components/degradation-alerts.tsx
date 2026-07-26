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
import { Alert02Icon, TickDouble02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import type { ModelRadarDegradationAlert } from '../types'

export function DegradationAlerts(props: {
  alerts: ModelRadarDegradationAlert[]
}) {
  const { t } = useTranslation()

  return (
    <section aria-labelledby='degradation-alerts-title' className='py-6'>
      <header className='mb-4'>
        <div className='flex items-center gap-2'>
          <HugeiconsIcon
            icon={Alert02Icon}
            className='size-4 text-amber-600 dark:text-amber-400'
            strokeWidth={2}
            aria-hidden='true'
          />
          <h2 id='degradation-alerts-title' className='text-base font-semibold'>
            {t('Degradation alerts')}
          </h2>
        </div>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t('Configurations whose IQ has declined across recent windows.')}
        </p>
      </header>

      {props.alerts.length === 0 ? (
        <div
          role='status'
          className='border-border/70 bg-muted/20 flex min-h-24 items-center gap-3 rounded-lg border px-4 py-5'
        >
          <span className='flex size-9 shrink-0 items-center justify-center rounded-lg bg-emerald-500/10 text-emerald-700 dark:text-emerald-400'>
            <HugeiconsIcon
              icon={TickDouble02Icon}
              className='size-4'
              strokeWidth={2}
              aria-hidden='true'
            />
          </span>
          <div>
            <p className='text-sm font-medium'>{t('No degradation alerts')}</p>
            <p className='text-muted-foreground mt-0.5 text-xs'>
              {t('No current IQ decline meets the source alert threshold.')}
            </p>
          </div>
        </div>
      ) : (
        <div className='grid gap-3 lg:grid-cols-2'>
          {props.alerts.map((alert) => (
            <article
              key={`${alert.model}:${alert.effort}`}
              className='border-border/70 bg-card rounded-lg border px-4 py-3'
              aria-label={`${alert.model} ${alert.effort}`}
            >
              <div className='flex min-w-0 items-start justify-between gap-4'>
                <div className='min-w-0'>
                  <p className='truncate text-sm font-semibold'>
                    {alert.model}
                  </p>
                  <p className='text-muted-foreground mt-0.5 text-xs uppercase'>
                    {alert.effort}
                  </p>
                </div>
                <div className='shrink-0 text-right'>
                  <p className='text-lg font-semibold tabular-nums'>
                    {alert.iq.toFixed(1)}
                  </p>
                  <p className='text-muted-foreground text-[11px]'>IQ</p>
                </div>
              </div>
              <dl className='mt-3 grid grid-cols-3 gap-2 border-t pt-3'>
                <Decline
                  label={t('12 hours')}
                  value={alert.degradation_12h_iq}
                />
                <Decline
                  label={t('24 hours')}
                  value={alert.degradation_24h_iq}
                />
                <Decline
                  label={t('48 hours')}
                  value={alert.degradation_48h_iq}
                />
              </dl>
            </article>
          ))}
        </div>
      )}
    </section>
  )
}

function Decline(props: { label: string; value: number }) {
  return (
    <div>
      <dt className='text-muted-foreground text-[11px]'>{props.label}</dt>
      <dd className='text-destructive mt-0.5 text-sm font-medium tabular-nums'>
        -{Math.abs(props.value).toFixed(1)} IQ
      </dd>
    </div>
  )
}
