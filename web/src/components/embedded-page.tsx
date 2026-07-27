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
import type { ComponentProps } from 'react'

import { cn } from '@/lib/utils'

const EMBEDDED_PAGE_SANDBOX = [
  'allow-downloads',
  'allow-forms',
  'allow-modals',
  'allow-popups',
  'allow-popups-to-escape-sandbox',
  'allow-same-origin',
  'allow-scripts',
  'allow-top-navigation-by-user-activation',
].join(' ')

const EMBEDDED_PAGE_PERMISSIONS = 'clipboard-read; clipboard-write; fullscreen'

type EmbeddedPageProps = Omit<ComponentProps<'iframe'>, 'src'> & {
  src: string
}

export function EmbeddedPage({
  allow,
  className,
  referrerPolicy,
  sandbox,
  src,
  ...props
}: EmbeddedPageProps) {
  return (
    <iframe
      {...props}
      src={src.trim()}
      className={cn('w-full border-0', className)}
      sandbox={sandbox ?? EMBEDDED_PAGE_SANDBOX}
      allow={allow ?? EMBEDDED_PAGE_PERMISSIONS}
      referrerPolicy={referrerPolicy ?? 'strict-origin-when-cross-origin'}
    />
  )
}
