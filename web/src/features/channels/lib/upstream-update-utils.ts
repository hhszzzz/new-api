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
export function normalizeModelList(models: unknown[] = []): string[] {
  return [
    ...new Set(
      (models || []).map((model) => String(model || '').trim()).filter(Boolean)
    ),
  ]
}

export function parseUpstreamUpdateMeta(settings: unknown): {
  enabled: boolean
  pendingAddModels: string[]
  pendingRemoveModels: string[]
} {
  let parsed: Record<string, unknown> | null = null
  if (settings && typeof settings === 'object' && !Array.isArray(settings)) {
    parsed = settings as Record<string, unknown>
  } else if (typeof settings === 'string') {
    try {
      parsed = JSON.parse(settings)
    } catch {
      parsed = null
    }
  }

  if (!parsed || typeof parsed !== 'object') {
    return { enabled: false, pendingAddModels: [], pendingRemoveModels: [] }
  }

  return {
    enabled: parsed.upstream_model_update_check_enabled === true,
    pendingAddModels: normalizeModelList(
      (parsed.upstream_model_update_last_detected_models as unknown[]) || []
    ),
    pendingRemoveModels: normalizeModelList(
      (parsed.upstream_model_update_last_removed_models as unknown[]) || []
    ),
  }
}

export type UpstreamUpdateSummary = {
  addModelCount: number
  removeModelCount: number
  addChannelCount: number
  removeChannelCount: number
}

export function summarizeUpstreamUpdates(
  channels: ReadonlyArray<{ settings: unknown }>
): UpstreamUpdateSummary {
  const summary: UpstreamUpdateSummary = {
    addModelCount: 0,
    removeModelCount: 0,
    addChannelCount: 0,
    removeChannelCount: 0,
  }

  for (const channel of channels) {
    const meta = parseUpstreamUpdateMeta(channel.settings)
    if (!meta.enabled) continue

    if (meta.pendingAddModels.length > 0) {
      summary.addModelCount += meta.pendingAddModels.length
      summary.addChannelCount++
    }
    if (meta.pendingRemoveModels.length > 0) {
      summary.removeModelCount += meta.pendingRemoveModels.length
      summary.removeChannelCount++
    }
  }

  return summary
}
