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
import type { ModelRadarFourTaskCell } from '../types'

/** Score dimensions in display order; the keys match the upstream components. */
export const FOUR_TASK_DIMENSIONS = [
  { key: 'total', label: 'Total score' },
  { key: 'combined', label: 'Qin Shi Huang' },
  { key: 'pompeii', label: 'Pompeii' },
  { key: 'pelican', label: 'Pelican' },
  { key: 'candy', label: 'Candy' },
] as const

export type FourTaskDimension = (typeof FOUR_TASK_DIMENSIONS)[number]['key']

/** Maps a four-task configuration to its status label. A total is published
 * only when all four components were graded, so a partially graded tier stays
 * visibly different from one with no results at all. */
export function fourTaskStatusLabelKey(
  cell: Pick<ModelRadarFourTaskCell, 'score' | 'graded_tasks'>
): string {
  if (cell.score !== null) return 'Complete'
  if (cell.graded_tasks === 0) return 'No valid grades'
  return 'Missing'
}

/** Maps one component's result to its status label. */
export function fourTaskComponentStatusLabelKey(score: number | null): string {
  return score === null ? 'Missing' : 'Complete'
}
