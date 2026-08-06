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
import type { Option } from '@/components/multi-select'

import { safeJsonParseWithValidation } from '../utils/json-parser'
import { isArray } from '../utils/json-validators'

export type ChatEntryData = {
  name: string
  url: string
  groups: string[]
}

type RestrictedChatValue = {
  url: string
  groups: string[]
}

export function parseChatEntries(value: string): ChatEntryData[] {
  const parsed = safeJsonParseWithValidation<unknown[]>(value, {
    fallback: [],
    validator: isArray,
    validatorMessage: 'Chats must be a JSON array',
    context: 'chats',
  })

  return parsed
    .map((item) => {
      if (typeof item !== 'object' || item === null || Array.isArray(item)) {
        return null
      }
      const entries = Object.entries(item)
      if (entries.length !== 1) return null

      const [name, rawValue] = entries[0]
      if (typeof rawValue === 'string') {
        return { name, url: rawValue, groups: [] }
      }
      if (
        typeof rawValue !== 'object' ||
        rawValue === null ||
        Array.isArray(rawValue)
      ) {
        return null
      }

      const restricted = rawValue as Record<string, unknown>
      const url = restricted.url
      const rawGroups = restricted.groups
      if (
        typeof url !== 'string' ||
        (rawGroups !== undefined &&
          (!Array.isArray(rawGroups) ||
            rawGroups.some((group) => typeof group !== 'string')))
      ) {
        return null
      }
      const groups = Array.isArray(rawGroups)
        ? [...new Set(rawGroups.map((group) => group.trim()).filter(Boolean))]
        : []
      return { name, url, groups }
    })
    .filter((item): item is ChatEntryData => item !== null)
}

export function serializeChatEntry(
  entry: ChatEntryData
): Record<string, string | RestrictedChatValue> {
  if (entry.groups.length === 0) {
    return { [entry.name]: entry.url }
  }
  return {
    [entry.name]: {
      url: entry.url,
      groups: [
        ...new Set(entry.groups.map((group) => group.trim()).filter(Boolean)),
      ],
    },
  }
}

export function buildChatGroupOptions(
  groupRatioValue: string,
  userUsableGroupsValue: string
): Option[] {
  let ratios: Record<string, unknown> = {}
  let descriptions: Record<string, unknown> = {}
  try {
    const parsed = JSON.parse(groupRatioValue || '{}')
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      ratios = parsed as Record<string, unknown>
    }
  } catch {
    ratios = {}
  }
  try {
    const parsed = JSON.parse(userUsableGroupsValue || '{}')
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      descriptions = parsed as Record<string, unknown>
    }
  } catch {
    descriptions = {}
  }

  return Object.keys(ratios)
    .filter((group) => group !== 'auto')
    .sort((left, right) => left.localeCompare(right))
    .map((group) => {
      const description = descriptions[group]
      return {
        value: group,
        label:
          typeof description === 'string' && description.trim() !== ''
            ? `${group} — ${description.trim()}`
            : group,
      }
    })
}
