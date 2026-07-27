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
import { LinkSquare01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { EmbeddedPage } from '@/components/embedded-page'
import { PublicLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import type { CustomHeaderNavItem } from '@/lib/nav-modules'

type CustomNavigationPageProps = {
  item: CustomHeaderNavItem
}

export function CustomNavigationPage(props: CustomNavigationPageProps) {
  const { t } = useTranslation()

  return (
    <PublicLayout showMainContainer={false}>
      <main className='flex h-svh min-h-0 flex-col pt-16'>
        <div className='border-border/70 bg-background flex h-11 shrink-0 items-center justify-between gap-3 border-b px-4'>
          <h1 className='min-w-0 truncate text-sm font-medium'>
            {props.item.title}
          </h1>
          <Button
            variant='ghost'
            size='icon-sm'
            title={t('Open in new tab')}
            render={
              <a
                href={props.item.url}
                target='_blank'
                rel='noopener noreferrer'
                aria-label={t('Open in new tab')}
              />
            }
          >
            <HugeiconsIcon
              icon={LinkSquare01Icon}
              strokeWidth={2}
              aria-hidden='true'
            />
          </Button>
        </div>
        <EmbeddedPage
          src={props.item.url}
          title={props.item.title}
          className='min-h-0 flex-1'
        />
      </main>
    </PublicLayout>
  )
}
