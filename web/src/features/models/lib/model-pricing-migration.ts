/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

export type ModelPricingMaps = {
  price: Record<string, number>
  ratio: Record<string, number>
  cache: Record<string, number>
  completion: Record<string, number>
  image: Record<string, number>
  audio: Record<string, number>
  audioCompletion: Record<string, number>
}

export type ModelPricingDraft = {
  price?: string
  ratio?: string
  cacheRatio?: string
  completionRatio?: string
  imageRatio?: string
  audioRatio?: string
  audioCompletionRatio?: string
}

const MODEL_PRICING_NUMBER_PATTERN = /^(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?$/i

export function parseModelPricingNumber(value: string): number | undefined {
  const normalized = value.trim()
  if (!MODEL_PRICING_NUMBER_PATTERN.test(normalized)) return undefined
  const parsed = Number(normalized)
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : undefined
}

type ReconcileModelPricingParams = {
  maps: ModelPricingMaps
  draft: ModelPricingDraft
  mode: 'per-token' | 'per-request'
  isEditing: boolean
  sourceName: string
  targetName: string
  loadedPricingName: string
}

export function reconcileModelPricingMaps({
  maps,
  draft,
  mode,
  isEditing,
  sourceName,
  targetName,
  loadedPricingName,
}: ReconcileModelPricingParams): ModelPricingMaps {
  const next = Object.fromEntries(
    Object.entries(maps).map(([key, value]) => [key, { ...value }])
  ) as ModelPricingMaps
  const allMaps = Object.values(next)
  const relevantDraftValues =
    mode === 'per-request'
      ? [draft.price]
      : [
          draft.ratio,
          draft.cacheRatio,
          draft.completionRatio,
          draft.imageRatio,
          draft.audioRatio,
          draft.audioCompletionRatio,
        ]
  const providedDraftValues = relevantDraftValues.filter(
    (value): value is string => value !== undefined && value !== ''
  )
  if (
    providedDraftValues.some(
      (value) => parseModelPricingNumber(value) === undefined
    )
  ) {
    return next
  }
  const hasDraftPricing = providedDraftValues.length > 0

  const isRename = isEditing && sourceName !== '' && sourceName !== targetName
  const targetHadPricing = Object.values(maps).some((map) =>
    Object.hasOwn(map, targetName)
  )

  if (isRename) {
    for (const map of allMaps) delete map[sourceName]
    if (targetHadPricing) return next
  }

  if (hasDraftPricing || targetName === loadedPricingName) {
    for (const map of allMaps) delete map[targetName]
  }
  if (!hasDraftPricing) return next

  if (mode === 'per-request' && draft.price !== undefined) {
    const price = parseModelPricingNumber(draft.price)
    if (price !== undefined) next.price[targetName] = price
    return next
  }

  const ratioFields: Array<
    [keyof Omit<ModelPricingMaps, 'price'>, keyof ModelPricingDraft]
  > = [
    ['ratio', 'ratio'],
    ['cache', 'cacheRatio'],
    ['completion', 'completionRatio'],
    ['image', 'imageRatio'],
    ['audio', 'audioRatio'],
    ['audioCompletion', 'audioCompletionRatio'],
  ]
  for (const [mapName, fieldName] of ratioFields) {
    const value = draft[fieldName]
    if (value !== undefined && value !== '') {
      const ratio = parseModelPricingNumber(value)
      if (ratio !== undefined) next[mapName][targetName] = ratio
    }
  }
  return next
}
