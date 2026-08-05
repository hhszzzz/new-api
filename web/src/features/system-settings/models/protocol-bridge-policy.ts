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
import { z } from 'zod'

const BYTES_PER_MEBIBYTE = 1024 * 1024

export const DEFAULT_PROTOCOL_BRIDGE_POLICY_FORM = {
  enabled: false,
  default_allow_conversion: false,
  state_ttl_seconds: 3600,
  max_state_turns: 128,
  max_state_mebibytes: 4,
} as const

export const protocolBridgePolicyFormSchema = z.object({
  enabled: z.boolean(),
  default_allow_conversion: z.boolean(),
  state_ttl_seconds: z.coerce
    .number()
    .int()
    .min(60, 'State TTL must be between 60 and 86400 seconds')
    .max(86400, 'State TTL must be between 60 and 86400 seconds'),
  max_state_turns: z.coerce
    .number()
    .int()
    .min(1, 'Maximum state turns must be between 1 and 512')
    .max(512, 'Maximum state turns must be between 1 and 512'),
  max_state_mebibytes: z.coerce
    .number()
    .min(0.0625, 'Maximum state size must be between 0.0625 and 128 MiB')
    .max(128, 'Maximum state size must be between 0.0625 and 128 MiB')
    .refine(
      (value) => Number.isInteger(value * BYTES_PER_MEBIBYTE),
      'Maximum state size must resolve to a whole number of bytes'
    ),
})

export type ProtocolBridgePolicyFormValues = z.output<
  typeof protocolBridgePolicyFormSchema
>

export function parseProtocolBridgePolicy(
  value: string | undefined
): ProtocolBridgePolicyFormValues {
  try {
    const parsed: unknown = JSON.parse(value?.trim() || '{}')
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return { ...DEFAULT_PROTOCOL_BRIDGE_POLICY_FORM }
    }
    const policy = parsed as Record<string, unknown>
    const candidate = {
      enabled:
        typeof policy.enabled === 'boolean'
          ? policy.enabled
          : DEFAULT_PROTOCOL_BRIDGE_POLICY_FORM.enabled,
      default_allow_conversion:
        typeof policy.default_allow_conversion === 'boolean'
          ? policy.default_allow_conversion
          : DEFAULT_PROTOCOL_BRIDGE_POLICY_FORM.default_allow_conversion,
      state_ttl_seconds:
        typeof policy.state_ttl_seconds === 'number'
          ? policy.state_ttl_seconds
          : DEFAULT_PROTOCOL_BRIDGE_POLICY_FORM.state_ttl_seconds,
      max_state_turns:
        typeof policy.max_state_turns === 'number'
          ? policy.max_state_turns
          : DEFAULT_PROTOCOL_BRIDGE_POLICY_FORM.max_state_turns,
      max_state_mebibytes:
        typeof policy.max_state_bytes === 'number'
          ? policy.max_state_bytes / BYTES_PER_MEBIBYTE
          : DEFAULT_PROTOCOL_BRIDGE_POLICY_FORM.max_state_mebibytes,
    }
    const result = protocolBridgePolicyFormSchema.safeParse(candidate)
    return result.success
      ? result.data
      : { ...DEFAULT_PROTOCOL_BRIDGE_POLICY_FORM }
  } catch {
    return { ...DEFAULT_PROTOCOL_BRIDGE_POLICY_FORM }
  }
}

export function serializeProtocolBridgePolicy(
  value: ProtocolBridgePolicyFormValues
): string {
  return JSON.stringify({
    enabled: value.enabled,
    default_allow_conversion: value.default_allow_conversion,
    state_ttl_seconds: value.state_ttl_seconds,
    max_state_turns: value.max_state_turns,
    max_state_bytes: Math.round(value.max_state_mebibytes * BYTES_PER_MEBIBYTE),
  })
}
