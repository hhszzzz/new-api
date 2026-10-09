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

import type { ChannelContextLimitRule } from '../types'

export const MAX_CONTEXT_LIMIT_TOKENS = 100_000_000

const contextLimitRuleSchema = z
  .object({
    model_pattern: z.string().trim().min(1),
    context_limit: z.number().int().positive().max(MAX_CONTEXT_LIMIT_TOKENS),
  })
  .strict()
  .refine((rule) => {
    try {
      new RegExp(rule.model_pattern)
      return true
    } catch {
      return false
    }
  })

export const contextLimitsSchema = z.array(contextLimitRuleSchema)

export const contextLimitsTextSchema = z.string().refine((value) => {
  try {
    const parsed: unknown = JSON.parse(value.trim() || '[]')
    return contextLimitsSchema.safeParse(parsed).success
  } catch {
    return false
  }
}, 'Context limits must be a valid JSON array with model patterns and positive token limits.')

export function parseContextLimits(
  value: string | undefined
): ChannelContextLimitRule[] {
  const parsed: unknown = JSON.parse(value?.trim() || '[]')
  return contextLimitsSchema.parse(parsed)
}

export function formatContextLimits(value: unknown): string {
  const parsed = contextLimitsSchema.safeParse(value)
  return JSON.stringify(parsed.success ? parsed.data : [], null, 2)
}
