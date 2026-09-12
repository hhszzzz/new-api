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
  gatewayEffortCandidates,
  getModelIconKey,
  groupConfigurations,
  matrixEfforts,
  listVendors,
  filterByVendor,
  filterAlertsByVendor,
  matchRadarModelToUserModels,
  pickAutoEffort,
  isRadarAutoEffortAllowed,
  resolveRadarSettings,
  resolveRadarModel,
  resolveDefaultVendor,
  splitRadarAliases,
  DEFAULT_RADAR_SETTINGS,
  getHistorySeries,
  getVendorMeta,
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
  test('groups by provider regardless of stale runner metadata', () => {
    const glm = { ...configuration('glm-5.3', 'high'), harness: 'codex' }
    const grok = { ...configuration('grok-4.6', 'high'), harness: 'codex' }
    expect(resolveRadarModel(glm.model).vendor).toBe('zhipu')
    expect(resolveRadarModel(grok.model).vendor).toBe('xai')
    expect(
      filterByVendor([glm, grok], DEFAULT_RADAR_SETTINGS, 'zhipu')
    ).toEqual([glm])
  })
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
    ['dsh-deepseek-v4-flash-vision-exp', 'DeepSeek.Color'],
    ['k3', 'Moonshot.Color'],
    ['hy4-preview', 'Hunyuan.Color'],
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

  test('resolves DSH models through their underlying model while preserving exact icon overrides', () => {
    const iconRegistry = {
      modelIcons: new Map([['deepseek-v4-pro', 'DeepSeek']]),
      providerIcons: new Map([['deepseek', 'DeepSeek.Color']]),
    }
    expect(getModelIconKey('dsh-deepseek-v4-pro', iconRegistry)).toBe(
      'DeepSeek'
    )
    expect(getModelIconKey('dsh-deepseek-v4-flash', iconRegistry)).toBe(
      'DeepSeek.Color'
    )
    iconRegistry.modelIcons.set('dsh-deepseek-v4-pro', 'DeepSeek.Color')
    expect(getModelIconKey('dsh-deepseek-v4-pro', iconRegistry)).toBe(
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
      'max',
      'low',
      'turbo',
    ])
    expect(groups[1].configurations.map((item) => item.effort)).toEqual([
      'high',
      'medium',
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

  test('counts distinct visible models instead of tiers and sorts by size then label', () => {
    const settings = resolveRadarSettings({
      models: { hidden: { hidden: true } },
    })
    const vendors = listVendors(
      [
        configuration('gpt-a', 'low'),
        configuration('gpt-a', 'high'),
        configuration('claude-a', 'low'),
        configuration('claude-b', 'low'),
        configuration('deepseek-a', 'high'),
        configuration('hidden', 'high'),
      ],
      settings
    )
    expect(vendors.map(({ key, modelCount }) => ({ key, modelCount }))).toEqual(
      [
        { key: 'anthropic', modelCount: 2 },
        { key: 'deepseek', modelCount: 1 },
        { key: 'openai', modelCount: 1 },
      ]
    )
  })

  test('filters configurations and alerts by vendor and drops hidden and absent tiers even in All', () => {
    const openai = configuration('gpt-a', 'low')
    const deepseek = configuration('dsh-deepseek-v4-pro', 'high')
    const configurations = [openai, deepseek, configuration('legacy', 'low')]
    const settings = resolveRadarSettings({
      models: { legacy: { hidden: true } },
    })
    const alerts = [
      {
        model: 'gpt-a',
        effort: 'low',
        iq: 80,
        degradation_12h_iq: 1,
        degradation_24h_iq: 2,
        degradation_48h_iq: 3,
      },
      {
        model: 'dsh-deepseek-v4-pro',
        effort: 'high',
        iq: 90,
        degradation_12h_iq: 1,
        degradation_24h_iq: 2,
        degradation_48h_iq: 3,
      },
    ]
    alerts.push(
      { ...alerts[0], model: 'legacy' },
      { ...alerts[0], effort: 'missing' }
    )
    expect(filterByVendor(configurations, settings, 'deepseek')).toEqual([
      deepseek,
    ])
    expect(filterByVendor(configurations, settings, 'all')).toEqual([
      openai,
      deepseek,
    ])
    expect(filterByVendor(configurations, settings, 'missing')).toEqual([])
    expect(
      filterAlertsByVendor(alerts, configurations, settings, 'deepseek')
    ).toEqual([alerts[1]])
    expect(
      filterAlertsByVendor(alerts, configurations, settings, 'all')
    ).toEqual(alerts.slice(0, 2))
    expect(
      filterAlertsByVendor(alerts, configurations, settings, 'missing')
    ).toEqual([])
  })

  test('resolves admin overrides before aliases and prefixes while preserving pricing icon precedence', () => {
    expect(resolveRadarModel('k3')).toMatchObject({
      displayName: 'kimi-k3',
      vendor: 'moonshot',
      iconKey: 'Moonshot.Color',
      source: 'alias',
    })
    expect(resolveRadarModel('hy4-preview')).toMatchObject({
      displayName: 'hy4-preview',
      vendor: 'tencent',
      iconKey: 'Hunyuan.Color',
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
    const registry = {
      modelIcons: new Map([['kimi-k3', 'Kimi.Color']]),
      providerIcons: new Map<string, string>(),
    }
    expect(
      resolveRadarModel('k3', DEFAULT_RADAR_SETTINGS, registry).iconKey
    ).toBe('Kimi.Color')
    registry.modelIcons.set('k3', 'Moonshot')
    expect(resolveRadarModel('k3', settings, registry).iconKey).toBe('Moonshot')
  })

  test('tolerates missing and invalid settings while respecting explicit false', () => {
    for (const raw of [undefined, null, [], 'invalid', {}]) {
      expect(resolveRadarSettings(raw)).toEqual(DEFAULT_RADAR_SETTINGS)
    }
    expect(
      resolveRadarSettings({
        show_degradation_alerts: false,
        models: { invalid: null, k3: { hidden: true } },
      })
    ).toEqual({
      ...DEFAULT_RADAR_SETTINGS,
      show_degradation_alerts: false,
      models: { k3: { hidden: true } },
    })
    expect(
      resolveRadarSettings({ default_vendor: 'invalid vendor' }).default_vendor
    ).toBe('openai')
  })

  test('selects the configured vendor only when it has visible models', () => {
    expect(
      resolveDefaultVendor(DEFAULT_RADAR_SETTINGS, [{ key: 'openai' }])
    ).toBe('openai')
    expect(
      resolveDefaultVendor(DEFAULT_RADAR_SETTINGS, [{ key: 'anthropic' }])
    ).toBe('all')
    expect(
      resolveDefaultVendor(
        resolveRadarSettings({ default_vendor: 'anthropic' }),
        [{ key: 'anthropic' }]
      )
    ).toBe('anthropic')
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

describe('model radar automatic reasoning tiers', () => {
  test('keeps only expressible, well-sampled tiers, deduped and IQ ordered', () => {
    const candidates = gatewayEffortCandidates([
      configuration('gpt-a', 'ultra'),
      { ...configuration('gpt-a', 'low'), iq: 95, valid_tasks: 2 },
      { ...configuration('gpt-a', 'Low'), iq: 70, average_price_usd: 5 },
      {
        ...configuration('gpt-a', 'medium'),
        iq: 90,
        average_price_usd: 2,
      },
      {
        ...configuration('gpt-a', 'high'),
        iq: 90,
        average_price_usd_by_band: { off_peak: 1, peak: 3 },
      },
      { ...configuration('gpt-a', 'xhigh'), iq: 80, average_price_usd: 8 },
    ])
    expect(candidates.map((item) => [item.effort, item.iq])).toEqual([
      ['medium', 90],
      ['high', 90],
      ['xhigh', 80],
      ['low', 70],
    ])
    expect(candidates.map((item) => item.priceUsd)).toEqual([2, 2, 8, 5])
    expect(candidates[1].radarEffort).toBe('high')
  })

  test('applies each selection strategy and reports whether the tier changes', () => {
    const candidates = gatewayEffortCandidates([
      { ...configuration('m', 'high'), iq: 100, average_price_usd: 10 },
      { ...configuration('m', 'medium'), iq: 90, average_price_usd: 1 },
      { ...configuration('m', 'low'), iq: 60, average_price_usd: 5 },
    ])
    expect(pickAutoEffort(candidates, 'highest_iq', 5)).toEqual({
      effort: 'high',
      iq: 100,
      changed: true,
    })
    expect(pickAutoEffort(candidates, 'highest_iq', 5, 'high')).toEqual({
      effort: 'high',
      iq: 100,
      changed: false,
    })
    expect(pickAutoEffort(candidates, 'iq_per_cost', 5)).toEqual({
      effort: 'medium',
      iq: 90,
      changed: true,
    })
    // The client's own tier survives while the best tier stays inside the gap.
    expect(pickAutoEffort(candidates, 'min_iq_delta', 45, 'low')).toEqual({
      effort: 'low',
      iq: 60,
      changed: false,
    })
    expect(pickAutoEffort(candidates, 'min_iq_delta', 40, 'low')).toEqual({
      effort: 'high',
      iq: 100,
      changed: true,
    })
    // An unpriced model cannot be compared by value, so the IQ leader wins.
    expect(
      pickAutoEffort(
        gatewayEffortCandidates([
          configuration('m', 'high'),
          { ...configuration('m', 'low'), iq: 70 },
        ]),
        'iq_per_cost',
        5
      )
    ).toEqual({ effort: 'high', iq: 100, changed: true })
    expect(pickAutoEffort([], 'highest_iq', 5)).toBeNull()
  })

  test('matches a radar model to the user names it can be called by', () => {
    expect(matchRadarModelToUserModels('gpt-5.4', undefined, ['gpt-5.4'])).toBe(
      true
    )
    expect(
      matchRadarModelToUserModels('gpt-5.4', undefined, ['openai/gpt-5.4'])
    ).toBe(true)
    expect(matchRadarModelToUserModels('openai/kimi-k3', ['k3'], ['K3'])).toBe(
      true
    )
    expect(matchRadarModelToUserModels('kimi-k3', ['k3'], ['other'])).toBe(
      false
    )
    expect(matchRadarModelToUserModels('kimi-k3', undefined, [])).toBe(false)
  })

  test('normalizes alias lists and drops unusable entries from stored settings', () => {
    expect(splitRadarAliases(' K3 , k3-turbo\n k3 ')).toEqual([
      'k3',
      'k3-turbo',
    ])
    const aliases = (list: string[]) =>
      resolveRadarSettings({ models: { 'kimi-k3': { aliases: list } } }).models[
        'kimi-k3'
      ].aliases
    expect(aliases([' KIMI-K3 ', 'k3', '', 'k3', 'x'.repeat(129)])).toEqual([
      'k3',
    ])
    expect(
      aliases(Array.from({ length: 20 }, (_, index) => `a${index}`))
    ).toEqual(Array.from({ length: 16 }, (_, index) => `a${index}`))
  })

  test('opts a model in for tier adjustment only when the administrator allowed it', () => {
    const settings = resolveRadarSettings({
      models: {
        'kimi-k3': { auto_effort: true },
        'gpt-5.4': { hidden: true },
        'claude-sonnet': { auto_effort: 'yes' },
      },
    })
    expect(settings.models['kimi-k3'].auto_effort).toBe(true)
    expect(settings.models['gpt-5.4'].auto_effort).toBeUndefined()
    expect(settings.models['claude-sonnet'].auto_effort).toBeUndefined()
    expect(isRadarAutoEffortAllowed(settings, 'kimi-k3')).toBe(true)
    expect(isRadarAutoEffortAllowed(settings, 'gpt-5.4')).toBe(false)
    expect(isRadarAutoEffortAllowed(undefined, 'kimi-k3')).toBe(false)
  })
})
