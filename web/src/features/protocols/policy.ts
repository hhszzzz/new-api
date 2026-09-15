import { z } from 'zod'

import type { ProtocolPolicy } from './types'

export const inheritedProtocolPolicy = '{"version":1}'

export function hasProtocolOverrides(value: string | undefined): boolean {
  if (!value) return false
  const policy = parseProtocolPolicy(value)
  return policy === null || Object.keys(policy).length > 1
}

const conversion = z.enum(['native_only', 'lossless', 'safe']).optional()
const protocols = z
  .array(z.string().min(1))
  .refine((values) => new Set(values).size === values.length)
  .optional()
const policySchema = z
  .object({
    version: z.literal(1),
    conversion,
    selection: z.enum(['declared', 'auto']).optional(),
    upstream_protocols: protocols,
    request_mode: z.enum(['structured', 'passthrough']).optional(),
    state_scope: z.enum(['disabled', 'bridge', 'all']).optional(),
    state_ttl_seconds: z
      .union([z.literal(0), z.number().int().min(60).max(2592000)])
      .optional(),
    max_state_turns: z.number().int().min(0).max(4096).optional(),
    max_state_bytes: z
      .union([z.literal(0), z.number().int().min(1024).max(134217728)])
      .optional(),
    rules: z
      .array(
        z
          .object({
            model_pattern: z.string().optional(),
            request_protocol: z.string().optional(),
            target_protocol: z.string().optional(),
            upstream_protocols: protocols,
            conversion,
            channel_ids: z.array(z.number().int().positive()).optional(),
            channel_types: z.array(z.number().int().nonnegative()).optional(),
            require_structured: z.boolean().optional(),
            deny: z.boolean().optional(),
          })
          .strict()
      )
      .optional(),
  })
  .strict()

export function parseProtocolPolicy(value: string): ProtocolPolicy | null {
  try {
    const result = policySchema.safeParse(
      JSON.parse(value || inheritedProtocolPolicy)
    )
    return result.success ? result.data : null
  } catch {
    return null
  }
}
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
