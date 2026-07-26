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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { toIntlLocale } from '@/i18n/languages'

export function useRadarFormatters() {
  const { i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)

  return useMemo(() => {
    const integerFormatter = new Intl.NumberFormat(locale, {
      maximumFractionDigits: 0,
    })
    const decimalFormatter = new Intl.NumberFormat(locale, {
      maximumFractionDigits: 2,
    })
    const compactFormatter = new Intl.NumberFormat(locale, {
      notation: 'compact',
      maximumFractionDigits: 2,
    })
    const percentFormatter = new Intl.NumberFormat(locale, {
      style: 'percent',
      maximumFractionDigits: 1,
    })
    const currencyFormatter = new Intl.NumberFormat(locale, {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: 2,
      maximumFractionDigits: 4,
    })
    const dateTimeFormatter = new Intl.DateTimeFormat(locale, {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    })
    const historyTimeFormatter = new Intl.DateTimeFormat(locale, {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
    })

    return {
      compact: (value: number | null) =>
        value === null ? null : compactFormatter.format(value),
      dateTime: (value: number | null) =>
        value === null ? null : dateTimeFormatter.format(value * 1000),
      decimal: (value: number | null) =>
        value === null ? null : decimalFormatter.format(value),
      historyTime: (value: number) => historyTimeFormatter.format(value * 1000),
      integer: (value: number | null) =>
        value === null ? null : integerFormatter.format(value),
      percent: (value: number | null) =>
        value === null ? null : percentFormatter.format(value),
      usd: (value: number | null) =>
        value === null ? null : currencyFormatter.format(value),
    }
  }, [locale])
}
