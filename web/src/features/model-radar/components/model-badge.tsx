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
import { getLobeIcon } from '@/lib/lobe-icon'

import {
  resolveRadarModel,
  type ModelRadarIconRegistry,
} from '../lib/model-radar'
import type { ModelRadarSettings } from '../types'

// Provider icon for a radar model, falling back to its group color dot.
export function ModelBadge(props: {
  color: string
  model: string
  settings?: ModelRadarSettings
  iconRegistry?: ModelRadarIconRegistry
}) {
  const { iconKey } = resolveRadarModel(
    props.model,
    props.settings,
    props.iconRegistry
  )
  if (iconKey) {
    return (
      <span
        className='flex size-6 shrink-0 items-center justify-center'
        aria-hidden='true'
      >
        {getLobeIcon(iconKey, 20)}
      </span>
    )
  }
  return (
    <span
      className='size-2.5 shrink-0 rounded-full'
      style={{ backgroundColor: props.color }}
      aria-hidden='true'
    />
  )
}
