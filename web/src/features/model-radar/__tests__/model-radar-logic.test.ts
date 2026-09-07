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
  listStations,
  filterByStation,
  filterAlertsByStation,
  getHistorySeries,
  getStationLabel,
} from '../lib/model-radar'
import type { ModelRadarConfiguration } from '../types'

function configuration(model: string, effort: string): ModelRadarConfiguration {
  return {
    model,
    effort,
    harness: '',
    runs_24h: null,
    runs_48h: null,
    average_price_usd_by_band: null,
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
  test('formats unknown station names without treating object properties as labels', () => {
    expect(getStationLabel('constructor')).toBe('Constructor')
    expect(getStationLabel('custom')).toBe('Custom')
  })
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
    ).toEqual(['high', 'adaptive', 'turbo'])
  })

  test('counts stations, skips legacy empty harnesses, and sorts by size', () => {
    expect(
      listStations([
        { ...configuration('a', 'low'), harness: 'dsh' },
        { ...configuration('b', 'low'), harness: 'codex' },
        { ...configuration('b', 'high'), harness: 'codex' },
        configuration('legacy', 'low'),
        { ...configuration('c', 'high'), harness: 'newstation' },
      ])
    ).toEqual([
      { key: 'codex', label: 'Codex', count: 2 },
      { key: 'dsh', label: 'DSH', count: 1 },
      { key: 'newstation', label: 'Newstation', count: 1 },
    ])
  })

  test('filters alerts by model and effort together and retains legacy data in All', () => {
    const codex = { ...configuration('shared', 'low'), harness: 'codex' }
    const dsh = { ...configuration('shared', 'high'), harness: 'dsh' }
    const configurations = [codex, dsh, configuration('legacy', 'low')]
    const alerts = [
      {
        model: 'shared',
        effort: 'low',
        iq: 80,
        degradation_12h_iq: 1,
        degradation_24h_iq: 2,
        degradation_48h_iq: 3,
      },
      {
        model: 'shared',
        effort: 'high',
        iq: 90,
        degradation_12h_iq: 1,
        degradation_24h_iq: 2,
        degradation_48h_iq: 3,
      },
    ]
    expect(filterByStation(configurations, 'dsh')).toEqual([dsh])
    expect(filterByStation(configurations, 'all')).toEqual(configurations)
    expect(filterByStation(configurations, 'missing')).toEqual([])
    expect(filterAlertsByStation(alerts, configurations, 'dsh')).toEqual([
      alerts[1],
    ])
    expect(filterAlertsByStation(alerts, configurations, 'all')).toEqual(alerts)
    expect(filterAlertsByStation(alerts, configurations, 'missing')).toEqual([])
  })

  test('limits history to the requested source-relative window and orders it chronologically', () => {
    const point = configuration('a', 'low')
    const history = [
      { ts: 80 * 3600, points: [{ ...point, iq: 90 }] },
      { ts: 8 * 3600, points: [{ ...point, iq: 70 }] },
      { ts: 32 * 3600, points: [{ ...point, iq: 80 }] },
      { ts: 7 * 3600, points: [{ ...point, iq: 60 }] },
    ]
    expect(getHistorySeries(history, 'a', 'low')).toEqual([70, 80, 90])
    expect(getHistorySeries(history, 'a', 'low', 48)).toEqual([80, 90])
    expect(getHistorySeries(history, 'missing', 'low')).toEqual([])
  })
})
