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
import { HugeiconsIcon } from '@hugeicons/react'

import { ReactIconByName } from '@/components/react-icon-by-name'
import { TOP_NAV_ICONS } from '@/lib/top-nav-icons'

import type { TopNavLink } from '../types'

type TopNavLinkContentProps = Pick<TopNavLink, 'icon'> & {
  title: React.ReactNode
}

export function TopNavLinkContent({ icon, title }: TopNavLinkContentProps) {
  let iconContent: React.ReactNode = null
  if (typeof icon === 'string') {
    iconContent = (
      <ReactIconByName
        name={icon}
        fallback={
          <HugeiconsIcon
            icon={TOP_NAV_ICONS.custom}
            className='size-4 shrink-0'
            data-top-nav-icon=''
            strokeWidth={2}
            aria-hidden='true'
          />
        }
        className='size-4 shrink-0'
        data-top-nav-icon=''
        aria-hidden='true'
      />
    )
  } else if (icon) {
    iconContent = (
      <HugeiconsIcon
        icon={icon}
        className='size-4 shrink-0'
        data-top-nav-icon=''
        strokeWidth={2}
        aria-hidden='true'
      />
    )
  }

  return (
    <span className='inline-flex items-center gap-1.5'>
      {iconContent}
      <span>{title}</span>
    </span>
  )
}
