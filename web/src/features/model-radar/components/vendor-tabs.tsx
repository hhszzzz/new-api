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
import { useEffect, useRef, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { getLobeIcon } from '@/lib/lobe-icon'

import { ALL_VENDORS, OTHER_VENDOR, type listVendors } from '../lib/model-radar'

export function VendorTabs(props: {
  vendors: ReturnType<typeof listVendors>
  totalModels: number
  value: string
  onValueChange: (vendor: string) => void
  children?: ReactNode
}) {
  const { t } = useTranslation()
  const scrollerRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const scroller = scrollerRef.current
    if (!scroller) return
    const revealSelectedVendor = () => {
      const selected = scroller.querySelector('[aria-selected="true"]')
      if (!selected) return
      const viewport = scroller.getBoundingClientRect()
      const tab = selected.getBoundingClientRect()
      if (tab.left < viewport.left) {
        scroller.scrollLeft += tab.left - viewport.left
      } else if (tab.right > viewport.right) {
        scroller.scrollLeft += tab.right - viewport.right
      }
    }
    revealSelectedVendor()
    const observer = new ResizeObserver(revealSelectedVendor)
    observer.observe(scroller)
    return () => observer.disconnect()
  }, [props.value, props.vendors.length, t])
  if (props.vendors.length <= 1) return props.children ?? null

  const vendors = [
    {
      key: ALL_VENDORS,
      label: t('All'),
      icon: null,
      modelCount: props.totalModels,
    },
    ...props.vendors,
  ]
  return (
    <Tabs
      value={props.value}
      onValueChange={(value) => props.onValueChange(String(value))}
      className='mt-5'
    >
      <div ref={scrollerRef} className='overflow-x-auto pb-1'>
        <TabsList variant='line' aria-label={t('Vendor')}>
          {vendors.map((vendor) => (
            <TabsTrigger
              key={vendor.key}
              value={vendor.key}
              className='gap-2 px-3'
            >
              {vendor.icon ? (
                <span aria-hidden='true'>{getLobeIcon(vendor.icon, 14)}</span>
              ) : null}
              {vendor.key === OTHER_VENDOR ? t('Other') : vendor.label}{' '}
              <Badge variant='secondary' className='tabular-nums'>
                {vendor.modelCount}
              </Badge>
            </TabsTrigger>
          ))}
        </TabsList>
      </div>
      <TabsContent value={props.value}>{props.children}</TabsContent>
    </Tabs>
  )
}
