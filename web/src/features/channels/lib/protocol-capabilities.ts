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

import type {
  ProtocolCapabilityModelOverride,
  ProtocolSelectionMode,
  UpstreamProtocol,
} from '../types'

export const UPSTREAM_PROTOCOLS = [
  'chat',
  'messages',
  'responses',
  'gemini',
] as const satisfies readonly UpstreamProtocol[]

export const PROTOCOL_SELECTION_MODES = [
  'strict',
  'auto',
] as const satisfies readonly ProtocolSelectionMode[]

export type ProtocolConversionMode = 'inherit' | 'allow' | 'deny'

const upstreamProtocolSchema = z.enum(UPSTREAM_PROTOCOLS)

const protocolListSchema = z
  .array(upstreamProtocolSchema)
  .refine((protocols) => new Set(protocols).size === protocols.length)

const protocolModelOverrideSchema = z
  .object({
    model_pattern: z.string().trim().min(1),
    upstream_protocols: protocolListSchema.optional(),
    allow_conversion: z.boolean().optional(),
  })
  .strict()
  .refine((override) => {
    try {
      new RegExp(override.model_pattern)
      return true
    } catch {
      return false
    }
  })

export const protocolModelOverridesSchema = z.array(protocolModelOverrideSchema)

export const protocolModelOverridesTextSchema = z.string().refine((value) => {
  try {
    const parsed: unknown = JSON.parse(value.trim() || '[]')
    return protocolModelOverridesSchema.safeParse(parsed).success
  } catch {
    return false
  }
}, 'Model overrides must be a valid JSON array with model patterns, protocols, and optional conversion flags.')

export function parseProtocolModelOverrides(
  value: string | undefined
): ProtocolCapabilityModelOverride[] {
  const parsed: unknown = JSON.parse(value?.trim() || '[]')
  return protocolModelOverridesSchema.parse(parsed)
}

export function formatProtocolModelOverrides(value: unknown): string {
  const parsed = protocolModelOverridesSchema.safeParse(value)
  return JSON.stringify(parsed.success ? parsed.data : [], null, 2)
}

export function protocolConversionMode(value: unknown): ProtocolConversionMode {
  if (value === true) return 'allow'
  if (value === false) return 'deny'
  return 'inherit'
}

export function protocolSelectionMode(value: unknown): ProtocolSelectionMode {
  return value === 'auto' ? 'auto' : 'strict'
}

export function defaultUpstreamProtocols(
  channelType: number,
  baseUrl: string | undefined
): UpstreamProtocol[] {
  if (channelType === 57) return ['responses']
  if (channelType === 14 || channelType === 33) return ['messages']
  if (channelType === 3) return ['chat', 'responses']
  if (channelType === 24 || channelType === 41) return ['gemini']
  if ([17, 45, 48].includes(channelType)) return ['chat', 'responses']
  if ([25, 26, 35, 43].includes(channelType)) return ['chat', 'messages']
  if (channelType === 59 || channelType === 60) {
    return ['chat', 'messages', 'responses', 'gemini']
  }
  if (channelType !== 1) return ['chat']

  const normalizedUrl = baseUrl?.trim()
  if (!normalizedUrl) return ['chat', 'responses']

  try {
    const parsed = new URL(normalizedUrl)
    return parsed.hostname.toLowerCase() === 'api.openai.com'
      ? ['chat', 'responses']
      : ['chat']
  } catch {
    return ['chat']
  }
}
