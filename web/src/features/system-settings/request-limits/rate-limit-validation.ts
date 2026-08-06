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
const MAX_RATE_LIMIT = 2_147_483_647

export const isValidRequestCountJSON = (value: string | undefined) => {
  if (!value || value.trim() === '') return true
  try {
    const parsed = JSON.parse(value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return false
    }
    return Object.values(parsed).every(
      (limits) =>
        Array.isArray(limits) &&
        limits.length === 2 &&
        Number.isInteger(limits[0]) &&
        Number.isInteger(limits[1]) &&
        limits[0] >= 0 &&
        limits[1] >= 1 &&
        limits[0] <= MAX_RATE_LIMIT &&
        limits[1] <= MAX_RATE_LIMIT
    )
  } catch {
    return false
  }
}

function isValidLimitValue(value: unknown) {
  return (
    value === undefined ||
    value === null ||
    (Number.isInteger(value) &&
      (value as number) >= 1 &&
      (value as number) <= MAX_RATE_LIMIT)
  )
}

export const isValidGroupPoliciesJSON = (value: string | undefined) => {
  if (!value || value.trim() === '') return true
  try {
    const parsed = JSON.parse(value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return false
    }
    const normalizedGroups = new Set<string>()
    for (const [group, policyValue] of Object.entries(parsed)) {
      const normalizedGroup = group.trim()
      if (
        !normalizedGroup ||
        normalizedGroup.length > 64 ||
        normalizedGroups.has(normalizedGroup) ||
        !policyValue ||
        typeof policyValue !== 'object' ||
        Array.isArray(policyValue)
      ) {
        return false
      }
      normalizedGroups.add(normalizedGroup)
      const policy = policyValue as Record<string, unknown>
      for (const layerName of ['member_limits', 'shared_pool'] as const) {
        const layer = policy[layerName]
        if (layer === undefined || layer === null) continue
        if (typeof layer !== 'object' || Array.isArray(layer)) return false
        const limits = layer as Record<string, unknown>
        if (
          !isValidLimitValue(limits.rpm_limit) ||
          !isValidLimitValue(limits.concurrency_limit) ||
          !isValidLimitValue(limits.stream_tps_limit) ||
          (layerName === 'member_limits' &&
            !isValidLimitValue(limits.first_token_delay_ms)) ||
          (layerName === 'shared_pool' &&
            Object.hasOwn(limits, 'first_token_delay_ms'))
        ) {
          return false
        }
      }
    }
    return true
  } catch {
    return false
  }
}
