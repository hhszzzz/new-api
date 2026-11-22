import type { DistributionRiskToken } from '../types'

export function distributionRiskTokenLabel(
  token: DistributionRiskToken,
  includeID = false
): string {
  const name = token.name.trim()
  if (!name) return `#${token.id}`
  return includeID ? `${name} (#${token.id})` : name
}
