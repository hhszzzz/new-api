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

import {
  createModelRadarIconRegistry,
  getModelIconKey,
  groupConfigurations,
  matrixEfforts,
} from '../lib/model-radar'
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
  test.each([
    ['gpt-5.4', 'OpenAI.Color'],
    ['openai/gpt-5.4', 'OpenAI.Color'],
    ['anthropic/claude-opus-4.1', 'Claude.Color'],
    ['google/gemini-2.5-pro', 'Gemini.Color'],
    ['meta-llama/llama-4', 'Meta.Color'],
    ['stepfun/step-3.5-flash', 'Stepfun.Color'],
    ['unknown-model', null],
  ])(
    'resolves %s to the default provider icon when no configuration exists',
    (model, expected) => {
      expect(getModelIconKey(model)).toBe(expected)
    }
  )

  test('uses the configured vendor icon variant before the radar fallback', () => {
    const iconRegistry = {
      modelIcons: new Map<string, string>(),
      providerIcons: new Map([['deepseek', 'DeepSeek.Color']]),
    }

    expect(getModelIconKey('deepseek-v3.2', iconRegistry)).toBe(
      'DeepSeek.Color'
    )
  })

  test('builds the radar icon registry from pricing vendor configuration', () => {
    const iconRegistry = createModelRadarIconRegistry({
      vendors: [{ id: 1, name: 'DeepSeek', icon: 'DeepSeek.Color' }],
      data: [
        {
          id: 1,
          model_name: 'deepseek-v3.2',
          icon: 'DeepSeek',
          vendor_id: 1,
          quota_type: 0,
          model_ratio: 1,
          completion_ratio: 1,
          enable_groups: ['default'],
        },
      ],
    })

    expect(getModelIconKey('provider/deepseek-v3.2', iconRegistry)).toBe(
      'DeepSeek.Color'
    )
  })

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

  test('keeps fallback model colors stable when source order changes', () => {
    const forward = groupConfigurations([
      configuration('model-a', 'low'),
      configuration('model-b', 'low'),
    ])
    const reversed = groupConfigurations([
      configuration('model-b', 'low'),
      configuration('model-a', 'low'),
    ])

    const forwardColors = Object.fromEntries(
      forward.map((group) => [group.model, group.color])
    )
    const reversedColors = Object.fromEntries(
      reversed.map((group) => [group.model, group.color])
    )
    expect(reversedColors).toEqual(forwardColors)
  })

  test('appends unknown efforts after the canonical matrix columns', () => {
    expect(
      matrixEfforts([
        configuration('model-a', 'turbo'),
        configuration('model-b', 'high'),
        configuration('model-c', 'adaptive'),
      ])
    ).toEqual([
      'low',
      'medium',
      'high',
      'xhigh',
      'max',
      'ultra',
      'adaptive',
      'turbo',
    ])
  })
})
