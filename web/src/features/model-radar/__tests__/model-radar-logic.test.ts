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
  benchEffortCandidates,
  createModelRadarIconRegistry,
  DEFAULT_RADAR_SETTINGS,
  getModelIconKey,
  getVendorMeta,
  isRadarAutoEffortAllowed,
  matchRadarModelToUserModels,
  pickAutoEffort,
  resolveRadarModel,
  resolveRadarSettings,
  splitRadarAliases,
} from '../lib/model-radar'
import type { ModelRadarBenchCell } from '../types'

function benchCell(
  effort: string,
  score: number | null,
  coverage = 64,
  price: number | null = null
): ModelRadarBenchCell {
  return {
    model: 'gpt-a',
    effort,
    score,
    coverage,
    required_tasks: 64,
    score_status: coverage === 64 ? 'complete' : 'provisional',
    graded_runs_count: coverage,
    source_scores: {},
    price: { mean: price, known_samples: coverage, total_samples: coverage },
    duration: { mean: null, known_samples: null, total_samples: null },
    statistics_state: 'available',
    detail_metrics: null,
    history_state: 'not_provided',
    history: [],
    last_graded_day: 0,
    read_at: 1,
  }
}

describe('model radar settings and model resolution', () => {
  test('does not treat inherited object properties as vendor or model metadata', () => {
    expect(getVendorMeta('constructor').label).toBe('constructor')
    expect(resolveRadarModel('constructor').vendor).toBe('other')
    expect(resolveRadarModel('__proto__').hidden).toBe(false)
  })

  test.each([
    ['gpt-5.4', 'OpenAI.Color'],
    ['openai/gpt-5.4', 'OpenAI.Color'],
    ['anthropic/claude-opus-4.1', 'Claude.Color'],
    ['google/gemini-2.5-pro', 'Gemini.Color'],
    ['meta-llama/llama-4', 'Meta.Color'],
    ['stepfun/step-3.5-flash', 'Stepfun.Color'],
    ['dsh-deepseek-v4-flash', 'DeepSeek.Color'],
    ['provider/dsh-deepseek-v4-pro', 'DeepSeek.Color'],
    ['k3', 'Moonshot.Color'],
    ['hy4-preview', 'Hunyuan.Color'],
    ['unknown-model', null],
  ])(
    'resolves %s to the default provider icon when no configuration exists',
    (model, expected) => {
      expect(getModelIconKey(model)).toBe(expected)
    }
  )

  test('resolves admin overrides before aliases and prefixes', () => {
    expect(resolveRadarModel('k3')).toMatchObject({
      displayName: 'kimi-k3',
      vendor: 'moonshot',
      iconKey: 'Moonshot.Color',
      source: 'alias',
    })
    const settings = resolveRadarSettings({
      models: {
        k3: { display_name: 'Custom Kimi', vendor: 'tencent', hidden: true },
      },
    })
    expect(resolveRadarModel('k3', settings)).toMatchObject({
      displayName: 'Custom Kimi',
      vendor: 'tencent',
      hidden: true,
      iconKey: 'Hunyuan.Color',
      source: 'override',
    })
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

  test('tolerates missing and invalid settings while ignoring removed legacy fields', () => {
    for (const raw of [undefined, null, [], 'invalid', {}]) {
      expect(resolveRadarSettings(raw)).toEqual(DEFAULT_RADAR_SETTINGS)
    }
    expect(
      resolveRadarSettings({
        auto_effort_enabled: false,
        default_vendor: 'invalid vendor',
        show_degradation_alerts: false,
        models: { invalid: null, k3: { hidden: true, auto_effort: true } },
      })
    ).toEqual({
      ...DEFAULT_RADAR_SETTINGS,
      auto_effort_enabled: false,
      models: { k3: { hidden: true, auto_effort: true } },
    })
  })

  test('splits, lowercases and de-duplicates alias lists', () => {
    expect(splitRadarAliases(' KIMI-K3, kimi-k3  k3-turbo ')).toEqual([
      'kimi-k3',
      'k3-turbo',
    ])
  })
})

describe('model radar automatic reasoning tiers', () => {
  test('keeps expressible, well-covered tiers, converts the display scale and orders by IQ', () => {
    const candidates = benchEffortCandidates([
      benchCell('ultra', 150),
      benchCell('max', 148, 2),
      benchCell('low', 105),
      benchCell('medium', 135, 64, 2),
      benchCell('High', 135),
      benchCell('xhigh', 120),
    ])

    expect(candidates.map((item) => [item.effort, item.iq])).toEqual([
      ['medium', 90],
      ['high', 90],
      ['xhigh', 80],
      ['low', 70],
    ])
    expect(candidates[0].priceUsd).toBe(2)
  })

  test('skips a selection without a score', () => {
    expect(benchEffortCandidates([benchCell('high', null)])).toEqual([])
  })

  test('picks the best tier, the best value tier, or keeps a close client tier', () => {
    const candidates = benchEffortCandidates([
      benchCell('low', 105, 64, 1),
      benchCell('medium', 135, 64, 4),
      benchCell('high', 150, 64, 10),
    ])

    expect(pickAutoEffort(candidates, 'highest_iq', 5)).toMatchObject({
      effort: 'high',
      changed: true,
    })
    expect(pickAutoEffort(candidates, 'iq_per_cost', 5)).toMatchObject({
      effort: 'low',
    })
    expect(pickAutoEffort(candidates, 'min_iq_delta', 50, 'low')).toMatchObject(
      { effort: 'low', changed: false }
    )
    expect(pickAutoEffort(candidates, 'min_iq_delta', 5, 'low')).toMatchObject({
      effort: 'high',
      changed: true,
    })
    expect(pickAutoEffort([], 'highest_iq', 5)).toBeNull()
  })

  test('matches gateway names through aliases and requires the admin opt-in', () => {
    expect(
      matchRadarModelToUserModels('gpt-a', ['local-gpt'], ['local-gpt'])
    ).toBe(true)
    expect(
      matchRadarModelToUserModels('gpt-a', undefined, ['provider/gpt-a'])
    ).toBe(true)
    expect(matchRadarModelToUserModels('gpt-a', undefined, ['gpt-b'])).toBe(
      false
    )

    const settings = resolveRadarSettings({
      models: { 'gpt-a': { auto_effort: true } },
    })
    expect(isRadarAutoEffortAllowed(settings, 'gpt-a')).toBe(true)
    expect(isRadarAutoEffortAllowed(settings, 'gpt-b')).toBe(false)
    expect(isRadarAutoEffortAllowed(undefined, 'gpt-a')).toBe(false)
  })
})
