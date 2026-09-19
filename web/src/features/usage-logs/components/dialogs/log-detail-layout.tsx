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
import { ChevronDown } from 'lucide-react'
import type { ReactNode } from 'react'

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { IconBadge, type IconBadgeTone } from '@/components/ui/icon-badge'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

export function DetailRow(props: {
  label: ReactNode
  value: ReactNode
  /** Optional small, uncolored icon rendered before the label. */
  icon?: ReactNode
  mono?: boolean
  muted?: boolean
  highlight?: boolean
}) {
  return (
    <div className='grid min-w-0 grid-cols-[5.25rem_minmax(0,1fr)] gap-2 text-sm sm:grid-cols-[7rem_minmax(0,1fr)] sm:gap-3'>
      {props.icon ? (
        <span className='text-muted-foreground flex min-w-0 items-center gap-1 text-xs'>
          <span className='flex shrink-0 items-center'>{props.icon}</span>
          <span className='min-w-0'>{props.label}</span>
        </span>
      ) : (
        <span className='text-muted-foreground min-w-0 text-xs'>
          {props.label}
        </span>
      )}
      <span
        className={cn(
          'max-w-full min-w-0 text-xs break-all sm:wrap-break-word',
          props.mono && 'font-mono',
          props.muted && 'text-muted-foreground',
          props.highlight && 'font-medium text-sky-600 dark:text-sky-400'
        )}
      >
        {props.value}
      </span>
    </div>
  )
}

export function CollapsibleDetailSection(props: {
  label: string
  count?: number
  children: ReactNode
}) {
  return (
    <Collapsible className='min-w-0'>
      <CollapsibleTrigger className='group focus-visible:ring-ring/50 bg-muted/30 hover:bg-muted/50 flex w-full items-center gap-2 rounded-md border px-2.5 py-2 text-left text-xs font-semibold outline-none focus-visible:ring-3'>
        <span className='min-w-0 flex-1'>{props.label}</span>
        {props.count != null && (
          <span className='text-muted-foreground bg-background/70 rounded px-1.5 py-0.5 font-mono text-[11px] leading-none tabular-nums'>
            {props.count}
          </span>
        )}
        <ChevronDown
          className='text-muted-foreground size-3.5 shrink-0 transition-transform group-aria-expanded:rotate-180'
          aria-hidden='true'
        />
      </CollapsibleTrigger>
      <CollapsibleContent className='data-open:animate-accordion-down data-closed:animate-accordion-up overflow-hidden'>
        <div className='bg-muted/20 mt-1 min-w-0 space-y-1 overflow-hidden rounded-md border p-2.5 max-sm:p-2'>
          {props.children}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

export function DetailSection(props: {
  icon?: ReactNode
  /** 'plain' renders the icon without the colored badge background. */
  iconTone?: IconBadgeTone | 'plain'
  label: string
  variant?: 'default' | 'danger'
  children: ReactNode
}) {
  const isDanger = props.variant === 'danger'
  const iconTone = isDanger ? 'destructive' : props.iconTone
  return (
    <div className='min-w-0 space-y-1.5'>
      <Label
        className={cn(
          'flex items-center gap-1.5 text-xs font-semibold',
          isDanger && 'text-red-500'
        )}
      >
        {props.icon &&
          (iconTone === 'plain' ? (
            <span className='text-muted-foreground flex shrink-0 items-center'>
              {props.icon}
            </span>
          ) : (
            <IconBadge tone={iconTone} size='xs'>
              {props.icon}
            </IconBadge>
          ))}
        {props.label}
      </Label>
      <div
        className={cn(
          'min-w-0 space-y-1 overflow-hidden rounded-md border p-2.5 max-sm:p-2',
          isDanger
            ? 'border-red-200 bg-red-50 dark:border-red-900 dark:bg-red-950/20'
            : 'bg-muted/30'
        )}
      >
        {props.children}
      </div>
    </div>
  )
}
