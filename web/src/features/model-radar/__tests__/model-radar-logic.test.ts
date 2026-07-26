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
import { describe, expect, test } from 'vitest'

import { groupConfigurations } from '../lib/model-radar'
import type { ModelRadarConfiguration } from '../types'

function configuration(model: string, effort: string): ModelRadarConfiguration {
  return {
    model,
    effort,
    iq: 100,
    passed: 2,
    valid_tasks: 3,
    average_price_usd: null,
    price_samples: null,
    average_minutes: null,
    duration_samples: null,
    incomplete_cost_samples: null,
    total_runs: null,
    latest_graded_at: null,
    average_agent_steps: null,
    agent_steps_samples: null,
    average_total_tokens: null,
    token_samples: null,
    cache_hit_rate: null,
    cache_token_samples: null,
    combined_cost_index: null,
  }
}

describe('model radar configuration grouping', () => {
  test('preserves first model appearance while sorting known efforts before unknown efforts', () => {
    const groups = groupConfigurations([
      configuration('model-b', 'turbo'),
      configuration('model-a', 'high'),
      configuration('model-b', 'max'),
      configuration('model-b', 'low'),
      configuration('model-a', 'medium'),
    ])

    expect(groups.map((group) => group.model)).toEqual(['model-b', 'model-a'])
    expect(groups[0].configurations.map((item) => item.effort)).toEqual([
      'low',
      'max',
      'turbo',
    ])
    expect(groups[1].configurations.map((item) => item.effort)).toEqual([
      'medium',
      'high',
    ])
  })
})
