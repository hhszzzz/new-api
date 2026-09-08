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

import { resolveRadarSettings } from '@/features/model-radar/lib/model-radar'
import type { ModelRadarModelOverride } from '@/features/model-radar/types'

const vendorSchema = z.string().regex(/^[a-z0-9-]{0,32}$/)

export const modelRadarSchema = z.object({
  defaultVendor: vendorSchema,
  showDegradationAlerts: z.boolean(),
  models: z
    .array(
      z.object({
        model: z.string().trim().min(1).max(128),
        displayName: z.string().trim().max(128),
        vendor: vendorSchema.refine((value) => value !== 'all'),
        hidden: z.boolean(),
      })
    )
    // The table can contain 256 source models plus 256 saved, retired models.
    // Only nonempty overrides are written to the option's 256-model map.
    .max(512)
    .refine(
      (rows) =>
        rows.filter((row) => row.displayName || row.vendor || row.hidden)
          .length <= 256,
      'You can configure up to 256 model overrides.'
    )
    .refine(
      (rows) => new Set(rows.map((row) => row.model)).size === rows.length
    ),
})

export type ModelRadarFormValues = z.infer<typeof modelRadarSchema>

export function parseModelRadarSettings(raw: string): ModelRadarFormValues {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    parsed = undefined
  }
  const settings = resolveRadarSettings(parsed)
  return {
    defaultVendor: settings.default_vendor,
    showDegradationAlerts: settings.show_degradation_alerts,
    models: Object.entries(settings.models).map(([model, override]) => ({
      model,
      displayName: override.display_name ?? '',
      vendor: override.vendor ?? '',
      hidden: override.hidden ?? false,
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
    const override: ModelRadarModelOverride = {}
    if (row.displayName.trim()) override.display_name = row.displayName.trim()
    if (row.vendor) override.vendor = row.vendor
    if (row.hidden) override.hidden = true
    if (Object.keys(override).length > 0) {
      models.push([row.model.trim(), override])
    }
  }
  return JSON.stringify({
    default_vendor: values.defaultVendor || 'openai',
    show_degradation_alerts: values.showDegradationAlerts,
    models: Object.fromEntries(models),
  })
}
