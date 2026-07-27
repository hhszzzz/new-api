/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
export function isValidToolPriceEntry(
  identifier: string,
  price: unknown
): price is number {
  const trimmedIdentifier = identifier.trim()
  if (!trimmedIdentifier || trimmedIdentifier !== identifier) return false

  const separatorIndex = identifier.indexOf(':')
  if (separatorIndex === 0) return false
  if (separatorIndex > 0) {
    const modelPrefix = identifier.slice(separatorIndex + 1).replace(/\*$/, '')
    if (!modelPrefix) return false
  }

  return typeof price === 'number' && Number.isFinite(price) && price >= 0
}

export function isToolPriceRecord(
  value: unknown
): value is Record<string, number> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  return Object.entries(value).every(([identifier, price]) =>
    isValidToolPriceEntry(identifier, price)
  )
}
