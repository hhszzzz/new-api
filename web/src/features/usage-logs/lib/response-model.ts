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
import type { LogOtherData } from '../types'

export type ResponseModelObservation = NonNullable<
  LogOtherData['response_model']
>

/**
 * Split a provider path prefix such as "vendor/" from the base model name.
 * The path is significant: "vendor/mapped" and "other/mapped" are treated as
 * different upstream models even though they share a base name.
 */
function splitProviderPath(model: string): { path: string; base: string } {
  const index = model.lastIndexOf('/')
  if (index >= 0) {
    return { path: model.slice(0, index + 1), base: model.slice(index + 1) }
  }
  return { path: '', base: model }
}

/**
 * Decide whether an upstream response model deserves a mismatch warning.
 *
 * Mirrors relay/common/response_model.go: the provider path must be identical,
 * and the base name must be equal ignoring case or extend the expected name as
 * a dated or variant version. A returned name behind a different provider path
 * is a different model even when the base name matches. Nothing is stored;
 * every row is judged with the current rule.
 */
export function isResponseModelMismatch(
  observation: ResponseModelObservation | undefined
): boolean {
  if (!observation) return false
  const returnedName = (observation.returned_model ?? '').toLowerCase()
  if (returnedName.trim() === '') return false
  const returned = splitProviderPath(returnedName)
  for (const candidate of [
    observation.requested_model,
    observation.upstream_model,
  ]) {
    const expected = splitProviderPath((candidate ?? '').toLowerCase())
    if (
      expected.base === '' ||
      returned.path !== expected.path ||
      !returned.base.startsWith(expected.base)
    ) {
      continue
    }
    return false
  }
  return true
}
