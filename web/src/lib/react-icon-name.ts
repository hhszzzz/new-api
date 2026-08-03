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
export const REACT_ICON_NAME_MAX_LENGTH = 80

const REACT_ICON_NAME_PATTERN =
  /^(?:Ai|Bi|Bs|Cg|Ci|Di|Fa|Fc|Fi|Gi|Go|Gr|Hi|Im|Io|Lia|Lu|Md|Pi|Ri|Rx|Si|Sl|Tb|Tfi|Ti|Vsc|Wi)[A-Z0-9][A-Za-z0-9]*$/

export function normalizeReactIconName(value: unknown): string {
  if (typeof value !== 'string') return ''

  const name = value.trim()
  if (
    name.length === 0 ||
    name.length > REACT_ICON_NAME_MAX_LENGTH ||
    !REACT_ICON_NAME_PATTERN.test(name)
  ) {
    return ''
  }

  return name
}
