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

export const DATA_TABLE_PAGE_SIZE_OPTIONS = [10, 20, 30, 40, 50, 100] as const

const DATA_TABLE_PAGE_SIZE_SET = new Set<number>(DATA_TABLE_PAGE_SIZE_OPTIONS)

export function isDataTablePageSize(value: unknown): value is number {
  return (
    typeof value === 'number' &&
    Number.isInteger(value) &&
    DATA_TABLE_PAGE_SIZE_SET.has(value)
  )
}
