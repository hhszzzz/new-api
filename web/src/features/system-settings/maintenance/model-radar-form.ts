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

import {
  MAX_RADAR_ALIASES_PER_MODEL,
  MAX_RADAR_ALIAS_RUNES,
  resolveRadarSettings,
  splitRadarAliases,
} from '@/features/model-radar/lib/model-radar'
import type { ModelRadarModelOverride } from '@/features/model-radar/types'

const vendorSchema = z.string().regex(/^[a-z0-9-]{0,32}$/)

const aliasSchema = z
  .string()
  .max(2048)
  .refine(
    (value) => splitRadarAliases(value).length <= MAX_RADAR_ALIASES_PER_MODEL,
    `You can configure up to ${MAX_RADAR_ALIASES_PER_MODEL} aliases per model.`
  )
  .refine(
    (value) =>
      splitRadarAliases(value).every(
        (name) => [...name].length <= MAX_RADAR_ALIAS_RUNES
      ),
    `Each alias must be at most ${MAX_RADAR_ALIAS_RUNES} characters long.`
  )

export const createModelRadarSchema = (
  describeAliasConflict: (alias: string, model: string) => string
) =>
  z.object({
    autoEffortEnabled: z.boolean(),
    defaultVendor: vendorSchema,
    showDegradationAlerts: z.boolean(),
    models: z
      .array(
        z.object({
          model: z.string().trim().min(1).max(128),
          displayName: z.string().trim().max(128),
          vendor: vendorSchema.refine((value) => value !== 'all'),
          hidden: z.boolean(),
          autoEffort: z.boolean(),
          aliases: aliasSchema,
        })
      )
      // The table can contain 256 source models plus 256 saved, retired models.
      // Only nonempty overrides are written to the option's 256-model map.
      .max(512)
      .refine(
        (rows) =>
          rows.filter(
            (row) =>
              row.displayName ||
              row.vendor ||
              row.hidden ||
              row.autoEffort ||
              splitRadarAliases(row.aliases).length > 0
          ).length <= 256,
        'You can configure up to 256 model overrides.'
      )
      .refine(
        (rows) => new Set(rows.map((row) => row.model)).size === rows.length,
        'Duplicate source model mappings are not allowed'
      )
      .superRefine((rows, ctx) => {
        // A gateway model name resolves to exactly one radar model, so an alias
        // may not collide with a visible model's name or with another visible
        // model's alias. Hidden models are never resolved, so they neither
        // claim names for themselves nor collide with anyone.
        const owners = new Map<string, { index: number; model: string }>()
        rows.forEach((row, index) => {
          if (row.hidden) return
          owners.set(row.model.trim().toLowerCase(), {
            index,
            model: row.model,
          })
        })
        rows.forEach((row, index) => {
          if (row.hidden) return
          for (const name of splitRadarAliases(row.aliases)) {
            const owner = owners.get(name)
            if (owner) {
              if (owner.index === index) continue
              ctx.addIssue({
                code: 'custom',
                path: [index, 'aliases'],
                message: describeAliasConflict(name, owner.model),
              })
              continue
            }
            owners.set(name, { index, model: row.model })
          }
        })
      }),
  })

export type ModelRadarFormValues = z.infer<
  ReturnType<typeof createModelRadarSchema>
>

export function parseModelRadarSettings(raw: string): ModelRadarFormValues {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    parsed = undefined
  }
  const settings = resolveRadarSettings(parsed)
  return {
    autoEffortEnabled: settings.auto_effort_enabled,
    defaultVendor: settings.default_vendor,
    showDegradationAlerts: settings.show_degradation_alerts,
    models: Object.entries(settings.models).map(([model, override]) => ({
      model,
      displayName: override.display_name ?? '',
      vendor: override.vendor ?? '',
      hidden: override.hidden ?? false,
      autoEffort: override.auto_effort ?? false,
      aliases: (override.aliases ?? []).join(', '),
    })),
  }
}

export function serializeModelRadarSettings(
  values: ModelRadarFormValues
): string {
  const models: Array<[string, ModelRadarModelOverride]> = []
  for (const row of [...values.models].sort((left, right) =>
    left.model.localeCompare(right.model)
  )) {
    const model = row.model.trim()
    const override: ModelRadarModelOverride = {}
    if (row.displayName.trim()) override.display_name = row.displayName.trim()
    if (row.vendor) override.vendor = row.vendor
    if (row.hidden) override.hidden = true
    if (row.autoEffort) override.auto_effort = true
    const aliases = splitRadarAliases(row.aliases).filter(
      (alias) => alias !== model.toLowerCase()
    )
    if (aliases.length) override.aliases = aliases
    if (Object.keys(override).length > 0) {
      models.push([model, override])
    }
  }
  return JSON.stringify({
    auto_effort_enabled: values.autoEffortEnabled,
    default_vendor: values.defaultVendor || 'openai',
    show_degradation_alerts: values.showDegradationAlerts,
    models: Object.fromEntries(models),
  })
}
