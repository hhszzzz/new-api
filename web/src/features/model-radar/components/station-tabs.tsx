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
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { ALL_STATIONS, type listStations } from '../lib/model-radar'

export function StationTabs(props: {
  stations: ReturnType<typeof listStations>
  total: number
  value: string
  onValueChange: (station: string) => void
  children?: ReactNode
}) {
  const { t } = useTranslation()
  if (props.stations.length <= 1) return props.children ?? null

  const stations = [
    { key: ALL_STATIONS, label: t('All'), count: props.total },
    ...props.stations,
  ]
  return (
    <Tabs
      value={props.value}
      onValueChange={(value) => props.onValueChange(String(value))}
      className='mt-5'
    >
      <div className='overflow-x-auto pb-1'>
        <TabsList variant='line' aria-label={t('Station')}>
          {stations.map((station) => (
            <TabsTrigger
              key={station.key}
              value={station.key}
              className='gap-2 px-3'
            >
              {station.label}{' '}
              <Badge variant='secondary' className='tabular-nums'>
                {station.count}
              </Badge>
            </TabsTrigger>
          ))}
        </TabsList>
      </div>
      <TabsContent value={props.value}>{props.children}</TabsContent>
    </Tabs>
  )
}
