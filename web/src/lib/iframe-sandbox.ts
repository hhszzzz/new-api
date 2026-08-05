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
const BASE_SANDBOX = 'allow-forms allow-popups allow-presentation allow-scripts'

export function getIframeSandbox(
  url: string | undefined,
  parentOrigin = typeof window === 'undefined'
    ? undefined
    : window.location.origin
): string {
  if (!url || !parentOrigin) return BASE_SANDBOX

  try {
    const target = new URL(url, parentOrigin)
    const isCrossOriginWebUrl =
      (target.protocol === 'http:' || target.protocol === 'https:') &&
      target.origin !== parentOrigin
    return isCrossOriginWebUrl
      ? `${BASE_SANDBOX} allow-same-origin`
      : BASE_SANDBOX
  } catch {
    return BASE_SANDBOX
  }
}
