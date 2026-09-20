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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterAll, afterEach, beforeEach, expect, test, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import type { UsageLog } from '../../data/schema'
import type { LogOtherData } from '../../types'
import { useCommonLogsColumns } from '../columns/common-logs-columns'

vi.mock('@lobehub/icons', () => ({}))
vi.hoisted(() => {
  vi.stubGlobal('localStorage', {
    getItem: () => null,
    setItem: () => undefined,
    removeItem: () => undefined,
  })
})
afterAll(() => vi.unstubAllGlobals())

function makeLog(other: LogOtherData, type = 2): UsageLog {
  return {
    id: 1,
    user_id: 1,
    created_at: 1,
    type,
    content: '',
    username: 'user',
    token_name: 'token',
    model_name: 'wan2.5-i2v-preview',
    quota: 5000,
    prompt_tokens: 0,
    completion_tokens: 0,
    use_time: 0,
    is_stream: false,
    channel: 1,
    channel_name: '',
    token_id: 1,
    group: 'default',
    ip: '',
    other: JSON.stringify(other),
    request_id: 'req-1',
    upstream_request_id: '',
  }
}

function DetailPreview(props: {
  other: LogOtherData
  isAdmin: boolean
  logType?: number
}) {
  // eslint-disable-next-line react/incompatible-library -- This test exercises TanStack Table without compiler memoization.
  const table = useReactTable({
    data: [makeLog(props.other, props.logType)],
    columns: useCommonLogsColumns(props.isAdmin, false, props.isAdmin),
    getCoreRowModel: getCoreRowModel(),
  })
  const cell = table
    .getRowModel()
    .rows[0].getAllCells()
    .find((item) => item.column.id === 'content')
  if (!cell) throw new Error('The log must have a content column')
  return flexRender(cell.column.columnDef.cell, cell.getContext())
}
const plugin = {
  key: 'incho',
  name: 'Incho',
  version: '1.0.1',
  author: { name: 'Plugin maintainer' },
}
const previousConfig = useSystemConfigStore.getState().config
let client: QueryClient
const i18n = createInstance()
beforeEach(async () => {
  await i18n.init({
    lng: 'en',
    resources: { en },
    interpolation: { escapeValue: false },
  })
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(['status'], {}, { updatedAt: Date.now() + 60_000 })
  client.setQueryData(
    ['pricing', 'anonymous'],
    { data: [], vendors: [] },
    { updatedAt: Date.now() + 60_000 }
  )
})
afterEach(() => {
  client.clear()
  useSystemConfigStore.getState().setConfig(previousConfig)
})
function renderPreview(other: LogOtherData, isAdmin = true, logType = 2) {
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <DetailPreview other={other} isAdmin={isAdmin} logType={logType} />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return screen.getByRole('button', { name: /./ })
}

const conversionLog: LogOtherData = {
  model_price: 0.25,
  request_path: '/v1/responses',
  diagnostics: { request_protocol: 'responses' },
  admin_info: {
    upstream_protocol: 'messages',
    protocol_converter: 'openai_responses_to_claude_messages',
    conversion_diagnostics: [
      {
        code: 'omitted_presentation_metadata',
        path: 'metadata',
        message: 'target protocol does not carry this display metadata',
        severity: 'warning',
        loss_class: 'presentation',
        from: 'openai-responses',
        to: 'claude',
      },
    ],
  },
}

test('keeps the details cell focused on billing and explains translation in the dialog', async () => {
  const user = userEvent.setup()
  const preview = renderPreview(conversionLog)
  expect(preview).not.toHaveTextContent(/translation/i)
  expect(preview).not.toHaveTextContent('→')
  expect(preview).toHaveTextContent('Per-call · $0.25')
  await user.click(preview)
  const dialog = within(await screen.findByRole('dialog'))
  // Flow rows render as label/value DetailRows instead of a step list.
  expect(dialog.getByText('Client request')).toBeVisible()
  expect(dialog.getByText('Request processing')).toBeVisible()
  expect(dialog.getByText('Upstream request')).toBeVisible()
  expect(dialog.getByText('OpenAI Responses')).toBeVisible()
  expect(dialog.getByText('messages translation')).toBeVisible()
  expect(dialog.getByText('Anthropic Messages')).toBeVisible()
  // Field adjustments start collapsed; open them to read the diagnostics.
  const adjustments = dialog.getByRole('button', { name: /Field adjustments/ })
  await user.click(adjustments)
  expect(dialog.getByText('metadata')).toBeVisible()
  expect(
    dialog.getByText(
      'The target protocol does not support this display metadata; it was omitted.'
    )
  ).toBeVisible()
  // Technical details render inline without a disclosure control.
  expect(dialog.getByText('openai_responses_to_claude_messages')).toBeVisible()
  expect(
    dialog.queryByRole('button', { name: 'Technical details' })
  ).not.toBeInTheDocument()
})

test('hides protocol field diagnostics and their badge outside the administrator view', async () => {
  const preview = renderPreview(conversionLog, false)
  expect(preview).not.toHaveTextContent(/translation/i)
  fireEvent.click(preview)
  const dialog = within(await screen.findByRole('dialog'))
  expect(dialog.queryByText('metadata')).not.toBeInTheDocument()
  expect(dialog.queryByText('Field adjustments')).not.toBeInTheDocument()
})

test('does not present conversion warnings on a failed request as completed field changes', async () => {
  const user = userEvent.setup()
  const preview = renderPreview(conversionLog, true, 5)
  fireEvent.click(preview)
  const dialog = within(await screen.findByRole('dialog'))
  expect(dialog.queryByText('Protocol converted')).not.toBeInTheDocument()
  expect(dialog.queryByText('Adjusted')).not.toBeInTheDocument()
  // The warning badge lives inside the collapsed field-adjustments section.
  await user.click(dialog.getByRole('button', { name: /Field adjustments/ }))
  expect(dialog.getByText('Warning')).toBeVisible()
})

test.each([
  { protocol: 'messages', label: 'messages' },
  { protocol: 'chat', label: 'chat' },
  { protocol: 'gemini', label: 'gemini' },
])(
  'names the $protocol target in lowercase inside the processing row',
  async ({ protocol, label }) => {
    fireEvent.click(
      renderPreview({
        request_conversion: ['openai-responses', protocol],
        admin_info: { upstream_protocol: protocol },
      })
    )
    const dialog = within(await screen.findByRole('dialog'))
    const outcome = dialog.getByText(`${label} translation`)
    // Sentence-case would still match a case-insensitive lookup, so compare the
    // rendered text exactly.
    expect(outcome.textContent).toBe(`${label} translation`)
  }
)

test.each([
  {
    name: 'a planned loss without an actual diagnostic',
    other: { admin_info: { protocol_lossy_conversion: 'metadata' } },
    type: 2,
  },
  {
    name: 'an unrelated warning',
    other: {
      admin_info: {
        conversion_diagnostics: [
          {
            code: 'model_modifier_override',
            message: 'model modifier overrides a parameter',
            severity: 'warning' as const,
          },
        ],
      },
    },
    type: 2,
  },
  { name: 'a rejected request', other: conversionLog, type: 5 },
])('does not label $name as a completed translation', ({ other, type }) => {
  expect(renderPreview(other, true, type)).not.toHaveTextContent('translation')
})

test('does not label native requests as a translation', () => {
  const preview = renderPreview({ request_conversion: ['responses'] })
  expect(preview).not.toHaveTextContent('translation')
})

test.each([
  {
    name: 'fixed expression zero price',
    other: {
      billing_mode: 'tiered_expr',
      billing_unit: 'request' as const,
      fixed_price: 0,
      matched_tier: 'free',
      expr_b64: btoa('tier("free", fixed(0))'),
    },
    expected: 'free · Per-call $0/request',
  },
  {
    name: 'fixed expression trace outside the display grammar',
    other: {
      billing_mode: 'tiered_expr',
      billing_unit: 'request' as const,
      fixed_price: 0.01,
      matched_tier: 'priority',
      expr_b64: btoa(
        'param("fast") == true ? tier("priority", fixed(0.01)) : tier("tokens", p * 2)'
      ),
    },
    expected: 'priority · Per-call $0.01/request',
  },
  {
    name: 'per-call',
    other: { model_price: 0.25 },
    expected: 'Per-call · $0.25',
  },
  {
    name: 'standard',
    other: { model_ratio: 1, completion_ratio: 2 },
    expected: 'Standard · $2 / $4/M',
  },
  {
    name: 'zero price fallback',
    other: { model_price: 0, group_ratio: 1 },
    expected: 'Group Ratio 1x',
  },
  { name: 'missing price fallback', other: {}, expected: '—' },
])('$name stays visible without a plugin counter', ({ other, expected }) => {
  const preview = renderPreview({
    ...other,
    admin_info: { task_plugin: plugin },
  })
  expect(preview.textContent).toBe(expected)
})

test('quota saturation remains first and only billing adds to the counter', () => {
  const preview = renderPreview({
    model_price: 0.25,
    admin_info: {
      task_plugin: plugin,
      quota_saturation: {
        op: 'round',
        kind: 'overflow',
        original: 3e9,
        clamped: 2147483647,
      },
    },
  })
  expect(preview.textContent).toBe('Quota clamped+1')
})

test.each([true, false])(
  'plugin information in the opened dialog respects admin=%s',
  async (isAdmin) => {
    const preview = renderPreview(
      { model_price: 0.25, admin_info: { task_plugin: plugin } },
      isAdmin
    )
    expect(preview.textContent).toBe('Per-call · $0.25')
    fireEvent.click(preview)
    const dialog = within(await screen.findByRole('dialog'))
    if (isAdmin) {
      expect(dialog.getByText('Incho')).toBeVisible()
      expect(dialog.getByText('1.0.1')).toBeVisible()
      expect(dialog.getByText('Plugin maintainer')).toBeVisible()
    } else {
      expect(dialog.queryByText('Incho')).not.toBeInTheDocument()
      expect(dialog.queryByText('Plugin maintainer')).not.toBeInTheDocument()
    }
  }
)

test.each([
  {
    expression: 'tier("music", u("clips") * 0.25)',
    tier: 'music',
    expected: 'music · clips $0.25/unit',
  },
  {
    expression:
      'u("mode") == "pro" ? tier("pro", u("seconds") * 0.8) : tier("std", u("seconds") * 0.4)',
    tier: 'pro',
    expected: 'pro · seconds $0.8/second',
  },
  {
    expression: 'tier("tokens", u("tokens") * 9.8 / 1000000)',
    tier: 'tokens',
    expected: 'tokens · tokens $9.8/1M token',
  },
  {
    expression: 'tier("free", u("clips") * 0)',
    tier: 'free',
    expected: 'free · clips $0/unit',
  },
  {
    expression: 'tier("mixed", 0.1 + u("clips") * 0.25 + u("units") * 0.14)',
    tier: 'mixed',
    expected:
      'mixed · clips $0.25/unit · units $0.14/credit · Additional charge $0.1/request',
  },
])(
  'task expression $tier shows its recorded unit price',
  ({ expression, tier, expected }) => {
    client.setQueryData(['pricing', 'anonymous'], {
      data: [
        {
          model_name: 'wan2.5-i2v-preview',
          billing_expr: 'tier("current", u("clips") * 99)',
          billing_usage_schema: {
            clips: { type: 'number', unit: 'count' },
            seconds: { type: 'number', unit: 'second' },
            tokens: { type: 'number', unit: 'token' },
            units: { type: 'number', unit: 'credit' },
            mode: { enum: ['pro', 'std'] },
          },
        },
      ],
      vendors: [],
    })
    const preview = renderPreview({
      is_task: true,
      billing_mode: 'tiered_expr',
      expr_b64: Buffer.from(expression).toString('base64'),
      matched_tier: tier,
      model_price: 0,
      admin_info: { task_plugin: plugin },
    })
    expect(preview.textContent).toBe(expected)
  }
)

test('task log prices use localized unit labels from pricing metadata', async () => {
  client.setQueryData(['pricing', 'anonymous'], {
    data: [
      {
        model_name: 'wan2.5-i2v-preview',
        billing_usage_schema: {
          images: {
            type: 'number',
            unit: 'count',
            unitLabel: { en: 'image', zh: '张' },
          },
        },
      },
    ],
    vendors: [],
  })
  const preview = renderPreview({
    is_task: true,
    billing_mode: 'tiered_expr',
    expr_b64: btoa('tier("images", u("images") * 0.25)'),
    matched_tier: 'images',
  })
  expect(preview).toHaveTextContent('images · images $0.25/image')
  await act(() => i18n.changeLanguage('zh-CN'))
  expect(
    screen.getByRole('button', { name: /images · images/ })
  ).toHaveTextContent('images · images $0.25/张')
})

test('task log prices select the executing provider’s schema', () => {
  client.setQueryData(['pricing', 'anonymous'], {
    data: [
      {
        model_name: 'wan2.5-i2v-preview',
        billing_usage_schema: { seconds: { type: 'number', unit: 'second' } },
        billing_plugin_variants: [
          {
            plugin_key: 'beta',
            plugin_name: 'Beta',
            billing_expr: 'tier("images", u("images") * 0.25)',
            billing_usage_schema: {
              images: {
                type: 'number',
                unit: 'count',
                unitLabel: { en: 'image' },
              },
            },
          },
        ],
      },
    ],
    vendors: [],
  })
  const preview = renderPreview({
    is_task: true,
    billing_mode: 'tiered_expr',
    expr_b64: btoa('tier("images", u("images") * 0.25)'),
    matched_tier: 'images',
    admin_info: {
      task_plugin: { key: 'beta', name: 'Beta', version: '1.0.0' },
    },
  })
  expect(preview).toHaveTextContent('images · images $0.25/image')
})

test.each(['missing schema', 'unsupported expression', 'unknown tier'])(
  'task pricing with %s shows an explicit unavailable summary',
  (scenario) => {
    if (scenario !== 'missing schema') {
      client.setQueryData(['pricing', 'anonymous'], {
        data: [
          {
            model_name: 'wan2.5-i2v-preview',
            billing_usage_schema: { clips: { type: 'number', unit: 'count' } },
          },
        ],
        vendors: [],
      })
    }
    const expression =
      scenario === 'unsupported expression'
        ? 'tier("music", max(u("clips"), 1) * 0.25)'
        : 'tier("music", u("clips") * 0.25)'
    const preview = renderPreview({
      is_task: true,
      billing_mode: 'tiered_expr',
      expr_b64: Buffer.from(expression).toString('base64'),
      matched_tier: scenario === 'unknown tier' ? 'old' : 'music',
    })
    expect(preview.textContent).toBe('Dynamic Pricing · No matching results')
  }
)
