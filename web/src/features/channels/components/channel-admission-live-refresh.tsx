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
import { useTranslation } from 'react-i18next'

import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { ADMISSION_LIVE_REFRESH_SECONDS } from '../lib/admission'

const LIVE_REFRESH_OPTIONS = [
  ...ADMISSION_LIVE_REFRESH_SECONDS.map((seconds) => ({
    value: seconds,
    label: `${seconds}s`,
  })),
  { value: 0, label: 'Off' },
] as const

export function ChannelAdmissionLiveRefreshSelect(props: {
  seconds: number
  onChange: (seconds: number) => void
}) {
  const { t } = useTranslation()
  const optionLabel = (value: number) => (value === 0 ? t('Off') : `${value}s`)

  return (
    <div className='flex items-center gap-1.5'>
      <Label
        htmlFor='admission-live-refresh'
        className='text-muted-foreground text-xs font-normal'
      >
        {t('Auto refresh')}
      </Label>
      <Select
        value={String(props.seconds)}
        onValueChange={(value) => {
          if (value) {
            props.onChange(Number(value))
          }
        }}
        items={LIVE_REFRESH_OPTIONS.map((option) => ({
          value: String(option.value),
          label: optionLabel(option.value),
        }))}
      >
        <SelectTrigger
          id='admission-live-refresh'
          size='sm'
          className='w-20 text-xs'
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {LIVE_REFRESH_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={String(option.value)}>
                {optionLabel(option.value)}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
    </div>
  )
}
