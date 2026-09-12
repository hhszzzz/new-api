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

import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

const BILLING_PREFERENCES = [
  { value: 'subscription_first', label: 'Subscription First' },
  { value: 'wallet_first', label: 'Wallet First' },
  { value: 'subscription_only', label: 'Subscription Only' },
  { value: 'wallet_only', label: 'Wallet Only' },
] as const

interface BillingPreferenceSelectProps {
  value: string
  onChange: (value: string) => void
}

// Only shown while the user holds a subscription that is not tied to a group:
// group-bound subscriptions always pay for their own group, so the order in
// which wallet and subscription are tried has nothing left to decide.
export function BillingPreferenceSelect(props: BillingPreferenceSelectProps) {
  const { t } = useTranslation()
  const current =
    BILLING_PREFERENCES.find((item) => item.value === props.value) ??
    BILLING_PREFERENCES[0]

  return (
    <Select
      items={BILLING_PREFERENCES.map((item) => ({
        value: item.value,
        label: t(item.label),
      }))}
      value={props.value}
      onValueChange={(value) => value !== null && props.onChange(value)}
    >
      <SelectTrigger
        className='h-8 w-full text-xs sm:w-[150px]'
        aria-label={t('Billing preference')}
      >
        <SelectValue>{t(current.label)}</SelectValue>
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false}>
        <SelectGroup>
          {BILLING_PREFERENCES.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {t(item.label)}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}
