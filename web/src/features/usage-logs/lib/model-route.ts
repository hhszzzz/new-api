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

export interface ModelRouteInfo {
  isMapped: boolean
  actualModel?: string
}

/**
 * Resolve administrator-only model routing metadata. New logs store it under
 * admin_info; the top-level fields remain as a compatibility fallback for
 * historical logs. The permission gate is deliberately part of this helper
 * so unexpected backend payloads cannot make routing details visible to a
 * regular user.
 */
export function getModelRouteInfo(
  other: LogOtherData | null,
  canViewModelRoute: boolean
): ModelRouteInfo {
  if (!canViewModelRoute || !other) return { isMapped: false }

  const nestedActualModel = other.admin_info?.upstream_model_name?.trim()
  if (other.admin_info?.is_model_mapped === true && nestedActualModel) {
    return { isMapped: true, actualModel: nestedActualModel }
  }

  const legacyActualModel = other.upstream_model_name?.trim()
  if (other.is_model_mapped === true && legacyActualModel) {
    return { isMapped: true, actualModel: legacyActualModel }
  }

  return { isMapped: false }
}

/**
 * Format a log's requested model and attach routing details only when the
 * current user has permission to view them.
 */
export function formatModelName(
  modelName: string,
  other: LogOtherData | null,
  canViewModelRoute: boolean
): {
  name: string
  isMapped: boolean
  actualModel?: string
} {
  const modelRoute = getModelRouteInfo(other, canViewModelRoute)

  return {
    name: modelName,
    isMapped: modelRoute.isMapped,
    actualModel: modelRoute.actualModel,
  }
}
