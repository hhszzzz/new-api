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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { RedemptionBatches } from './redemption-batches'
import { RedemptionsTable } from './redemptions-table'

export function RedemptionViews() {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = useState('codes')

  return (
    <Tabs
      value={activeTab}
      onValueChange={setActiveTab}
      aria-label={t('Redemption Codes')}
      className='flex h-full min-h-0 flex-col gap-3'
    >
      <TabsList className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'>
        <TabsTrigger value='codes'>{t('Redemption Codes')}</TabsTrigger>
        <TabsTrigger value='batches'>{t('Redemption batches')}</TabsTrigger>
      </TabsList>
      <TabsContent value='codes' className='min-h-0 flex-1'>
        <RedemptionsTable />
      </TabsContent>
      <TabsContent value='batches' className='min-h-0 flex-1'>
        <RedemptionBatches />
      </TabsContent>
    </Tabs>
  )
}
