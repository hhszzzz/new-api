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
import { useTranslation } from 'react-i18next'

import { LanguageSwitcher } from '@/components/language-switcher'
import { RichContent } from '@/components/rich-content'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { toIntlLocale } from '@/i18n/languages'

type SiteClosureViewProps = {
  systemName?: string
  logo?: string
  title?: string
  message?: string
  expectedRecoveryAt?: number
  onRetry: () => void
  retrying?: boolean
}

export function SiteClosureView({
  systemName,
  logo,
  title,
  message,
  expectedRecoveryAt,
  onRetry,
  retrying = false,
}: SiteClosureViewProps) {
  const { t, i18n } = useTranslation()
  const hasCustomTitle = Boolean(title?.trim())
  const hasCustomMessage = Boolean(message?.trim())
  const recoveryAt =
    typeof expectedRecoveryAt === 'number' && expectedRecoveryAt > 0
      ? new Date(expectedRecoveryAt * 1000)
      : null
  const recoveryLabel = recoveryAt
    ? new Intl.DateTimeFormat(
        toIntlLocale(i18n.resolvedLanguage || i18n.language),
        { dateStyle: 'medium', timeStyle: 'short' }
      ).format(recoveryAt)
    : ''

  return (
    <div className='bg-background text-foreground relative flex min-h-svh flex-col'>
      <header className='flex items-center justify-between gap-4 px-4 py-4 sm:px-6'>
        <div className='flex min-w-0 items-center gap-3'>
          {logo ? (
            <img
              src={logo}
              alt={systemName || ''}
              className='size-9 rounded-lg object-contain'
            />
          ) : null}
          {systemName ? (
            <span className='truncate text-sm font-medium'>{systemName}</span>
          ) : null}
        </div>
        <LanguageSwitcher />
      </header>

      <main className='flex flex-1 items-center justify-center px-4 pb-16 sm:px-6'>
        <Card className='w-full max-w-2xl'>
          <CardContent className='flex flex-col items-center gap-5 py-12 text-center'>
            <span className='text-muted-foreground text-xs font-medium tracking-[0.2em] uppercase'>
              503
            </span>
            <h1 className='text-3xl font-semibold tracking-tight text-balance sm:text-4xl'>
              {hasCustomTitle
                ? title
                : t('Website is under maintenance!')}
            </h1>

            {hasCustomMessage ? (
              <RichContent
                content={message ?? ''}
                breaks
                className='text-muted-foreground max-w-xl text-base'
              />
            ) : (
              <p className='text-muted-foreground max-w-xl text-base'>
                {t('The site is not available at the moment.')} <br />
                {t("We'll be back online shortly.")}
              </p>
            )}

            {recoveryLabel ? (
              <p className='text-muted-foreground text-sm'>
                {t('Expected recovery time')}:{' '}
                <span className='text-foreground font-medium'>
                  {recoveryLabel}
                </span>
              </p>
            ) : null}

            <div className='mt-2 flex flex-wrap items-center justify-center gap-3'>
              <Button variant='outline' onClick={onRetry} disabled={retrying}>
                {t('Retry')}
              </Button>
              <Button
                variant='ghost'
                render={
                  <Link to='/sign-in' search={{ redirect: '/' }} />
                }
              >
                {t('Admin sign in')}
              </Button>
            </div>
          </CardContent>
        </Card>
      </main>
    </div>
  )
}
