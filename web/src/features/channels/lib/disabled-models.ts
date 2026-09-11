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
import type { DisabledModelEntry } from '../types'

/**
 * Read the disabled-model list from a channel's other_info JSON. Disabled state
 * lives in other_info so ordinary channel edits cannot clear it.
 */
export function parseDisabledModels(
  otherInfo?: string | null
): DisabledModelEntry[] {
  if (!otherInfo) return []
  try {
    const parsed = JSON.parse(otherInfo)
    const entries = parsed?.disabled_models
    if (!Array.isArray(entries)) return []
    return entries.filter(
      (entry): entry is DisabledModelEntry =>
        entry && typeof entry.model === 'string' && entry.model !== ''
    )
  } catch {
    return []
  }
}

function normalizeGroup(group: unknown): string {
  return typeof group === 'string' ? group.trim() : ''
}

/**
 * Groups in which the model is disabled on this channel. An entry with an empty
 * group means every group, so it expands to all of the channel's groups.
 */
export function disabledGroupsForModel(
  entries: DisabledModelEntry[],
  model: string,
  channelGroups: string[]
): string[] {
  const normalizedModel = model.trim()
  const groups = new Set<string>()
  for (const entry of entries) {
    if (entry.model.trim() !== normalizedModel) continue
    const group = normalizeGroup(entry.group)
    if (group === '') {
      for (const channelGroup of channelGroups) groups.add(channelGroup)
    } else {
      groups.add(group)
    }
  }
  return [...groups]
}
