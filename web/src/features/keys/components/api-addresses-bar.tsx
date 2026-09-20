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
import { Zap } from 'lucide-react'
import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useApiInfo } from '@/features/dashboard/hooks/use-status-data'
import {
  getDefaultPingStatus,
  testUrlLatency,
} from '@/features/dashboard/lib/api-info'
import type { PingStatusMap } from '@/features/dashboard/types'
import { getBgColorClass } from '@/lib/colors'
import { cn } from '@/lib/utils'

export function ApiAddressesBar() {
  const { t } = useTranslation()
  const { items, loading } = useApiInfo()
  const [pingStatus, setPingStatus] = useState<PingStatusMap>({})

  const handleTest = useCallback(async (url: string) => {
    setPingStatus((current) => ({
      ...current,
      [url]: { latency: null, testing: true, error: false },
    }))
    const result = await testUrlLatency(url)
    setPingStatus((current) => ({ ...current, [url]: result }))
  }, [])

  if (loading) {
    return <Skeleton className='h-10 w-full rounded-lg' />
  }
  if (items.length === 0) return null

  return (
    <section
      aria-label={t('API Addresses')}
      className='border-border/60 bg-muted/20 flex min-w-0 flex-wrap items-center gap-2 rounded-lg border px-2.5 py-2 sm:px-3'
    >
      <div className='text-muted-foreground shrink-0 text-xs font-medium'>
        <span>{t('API Addresses')}</span>
      </div>
      <div className='flex min-w-0 flex-1 flex-wrap items-center gap-2'>
        {items.map((item) => {
          const status = pingStatus[item.url] ?? getDefaultPingStatus()
          return (
            <div
              key={item.url}
              className='bg-background/80 flex max-w-full min-w-0 items-center gap-1.5 rounded-md border px-2 py-1 shadow-xs'
            >
              <span
                className={cn(
                  'size-1.5 shrink-0 rounded-full',
                  getBgColorClass(item.color)
                )}
                aria-hidden='true'
              />
              <span
                className='shrink-0 text-xs font-medium'
                title={item.description}
              >
                {item.route}
              </span>
              <span
                className='text-muted-foreground max-w-80 min-w-0 truncate font-mono text-xs'
                title={item.url}
              >
                {item.url}
              </span>
              {status.testing && (
                <StatusBadge
                  label={t('Testing...')}
                  variant='warning'
                  type='text'
                  copyable={false}
                  className='animate-pulse text-xs'
                />
              )}
              {status.latency !== null && !status.testing && (
                <StatusBadge
                  label={`${status.latency}${t('ms')}`}
                  variant='success'
                  type='text'
                  copyable={false}
                  className='font-mono text-xs'
                />
              )}
              {status.error && (
                <StatusBadge
                  label={t('N/A')}
                  variant='neutral'
                  type='text'
                  copyable={false}
                  className='text-xs'
                />
              )}
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                className='size-6 shrink-0'
                aria-label={`${t('Test Latency')}: ${item.route}`}
                title={t('Test Latency')}
                disabled={status.testing}
                onClick={() => void handleTest(item.url)}
              >
                <Zap className='size-3.5' aria-hidden='true' />
              </Button>
              <CopyButton
                value={item.url}
                variant='ghost'
                size='sm'
                className='size-6 shrink-0 p-0'
                iconClassName='size-3.5'
                tooltip={t('Copy URL')}
                aria-label={`${t('Copy URL')}: ${item.route}`}
              />
            </div>
          )
        })}
      </div>
    </section>
  )
}
