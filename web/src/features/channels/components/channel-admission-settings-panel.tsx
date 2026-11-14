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
import { Link } from '@tanstack/react-router'
import { ChevronDown, Settings2, SlidersHorizontal } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { IconBadge } from '@/components/ui/icon-badge'
import { cn } from '@/lib/utils'

import {
  effectiveAdmissionControl,
  formatAdmissionWaitMs,
  formatUserAdmissionLimit,
  hasCustomizedAdmissionControl,
} from '../lib/admission'
import type { Channel } from '../types'

export function ChannelAdmissionSettingsPanel(props: { channel: Channel }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const control = effectiveAdmissionControl(props.channel)
  const customized = hasCustomizedAdmissionControl(props.channel)
  const userLimits = props.channel.user_rate_limits ?? []
  let soupSummary: string
  if (control.soup_mode === 'off') {
    soupSummary = t('Off')
  } else if (control.soup_mode === 'ratio') {
    soupSummary = `${t('Admission ratio')} · ${control.soup_ratio_percent}%`
  } else {
    soupSummary = `${t('Interval')} · ${formatAdmissionWaitMs(control.soup_after_ms)} / ${formatAdmissionWaitMs(control.soup_interval_ms)}`
  }
  const items: Array<[string, string | number]> = [
    [
      t('Concurrency limit'),
      (props.channel.concurrency_limit ?? 0) > 0
        ? (props.channel.concurrency_limit ?? 0)
        : t('Unlimited'),
    ],
    [
      t('RPM limit'),
      (props.channel.rpm_limit ?? 0) > 0
        ? (props.channel.rpm_limit ?? 0)
        : t('Unlimited'),
    ],
    [t('Priority'), props.channel.priority ?? 0],
    [t('Wait timeout'), formatAdmissionWaitMs(control.wait_timeout_ms)],
    [t('Maximum queued requests'), control.max_waiting_total],
    [t('Maximum queued requests per user'), control.max_waiting_per_user],
    [t('Lower-priority release'), soupSummary],
    [
      t('Retry-After (seconds)'),
      formatAdmissionWaitMs(control.retry_after_seconds * 1_000),
    ],
  ]

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <section className='bg-card overflow-hidden rounded-2xl border shadow-xs'>
        <header className='flex flex-wrap items-center gap-2 px-4 py-3 sm:px-5'>
          <CollapsibleTrigger
            render={
              <button
                type='button'
                className='flex min-w-0 flex-1 items-center gap-2 text-left'
                aria-expanded={open}
              />
            }
          >
            <IconBadge tone='neutral' size='sm'>
              <SlidersHorizontal />
            </IconBadge>
            <h2 className='text-sm font-semibold'>{t('Admission settings')}</h2>
            <Badge variant={customized ? 'secondary' : 'outline'}>
              {customized ? t('Customized') : t('Default')}
            </Badge>
            <ChevronDown
              className={cn(
                'text-muted-foreground size-4 transition-transform',
                open && 'rotate-180'
              )}
              aria-hidden='true'
            />
          </CollapsibleTrigger>
          <Button
            variant='outline'
            size='sm'
            className='ml-auto'
            render={<Link to='/channels' search={{ edit: props.channel.id }} />}
          >
            <Settings2 className='size-4' aria-hidden='true' />
            {t('Edit channel settings')}
          </Button>
        </header>
        <CollapsibleContent>
          <div className='flex flex-col gap-3 border-t p-4 sm:p-5'>
            <p className='text-muted-foreground text-xs'>
              {t(
                'These values control how this channel queues requests when its concurrency limit is reached.'
              )}
            </p>
            <dl className='grid grid-cols-2 gap-2.5 sm:grid-cols-3 lg:grid-cols-4'>
              {items.map(([label, value]) => (
                <div
                  key={String(label)}
                  className='bg-muted/40 rounded-xl px-3 py-2.5'
                >
                  <dt className='text-muted-foreground truncate text-[11px] font-medium'>
                    {label}
                  </dt>
                  <dd
                    className='mt-1 truncate font-mono text-sm font-semibold tabular-nums'
                    title={String(value)}
                  >
                    {value}
                  </dd>
                </div>
              ))}
            </dl>
            {userLimits.length > 0 && (
              <div className='bg-muted/30 flex flex-col gap-2 rounded-xl p-3'>
                <p className='text-muted-foreground text-[11px] font-medium'>
                  {t('Per-user limits')}
                </p>
                <div className='flex flex-wrap gap-1.5'>
                  {userLimits.map((limit) => (
                    <Badge
                      key={limit.user_id}
                      variant='secondary'
                      className='font-mono font-normal tabular-nums'
                    >
                      {formatUserAdmissionLimit(limit, t)}
                    </Badge>
                  ))}
                </div>
              </div>
            )}
          </div>
        </CollapsibleContent>
      </section>
    </Collapsible>
  )
}
