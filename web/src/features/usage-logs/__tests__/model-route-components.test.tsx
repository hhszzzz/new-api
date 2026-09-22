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
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useEffect } from 'react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { useCommonLogsColumns } from '../components/columns/common-logs-columns'
import { DetailsDialog } from '../components/dialogs/details-dialog'
import {
  UsageLogsProvider,
  useLogsViewScope,
  type LogsViewScope,
} from '../components/usage-logs-provider'
import type { UsageLog } from '../data/schema'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: { protocol?: string }) =>
      key.replace('{{protocol}}', values?.protocol ?? '{{protocol}}'),
  }),
}))

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: () => null,
}))

vi.mock('@/features/pricing/hooks/use-pricing-data', () => ({
  usePricingData: () => ({ models: [] }),
}))

const REQUESTED_MODEL = 'requested-model'
const NESTED_ACTUAL_MODEL = 'nested-actual-model'
const LEGACY_ACTUAL_MODEL = 'legacy-actual-model'
const NESTED_PARAM_OVERRIDE = '$.temperature = 0.5'
const LEGACY_PARAM_OVERRIDE = '$.temperature = 0.9'

const routedLog: UsageLog = {
  id: 1,
  user_id: 1,
  created_at: 1_800_000_000,
  type: 2,
  content: '',
  username: 'tester',
  token_name: 'test-token',
  model_name: REQUESTED_MODEL,
  quota: 0,
  prompt_tokens: 0,
  completion_tokens: 13,
  use_time: 0,
  is_stream: false,
  channel: 7,
  channel_name: 'admin-only-channel',
  token_id: 1,
  group: 'default',
  ip: '',
  other: JSON.stringify({
    transport: 'websocket',
    duration_ms: 900,
    frt: 250,
    admin_info: {
      is_model_mapped: true,
      upstream_model_name: NESTED_ACTUAL_MODEL,
      po: [`set ${NESTED_PARAM_OVERRIDE}`],
      request_headers: {
        'user-agent': 'codex-cli/1.0',
        'x-codex-thread-id': 'sha256:test-thread',
      },
      upstream_protocol: 'chat',
      protocol_converter: 'responses_to_chat',
      protocol_state_mode: 'replay',
      request_policy: [
        {
          attempt: 1,
          channel_id: 7,
          group: 'default',
          rule: 'Existing session rule',
          elapsed_ms: 12,
          decision: {
            action: 'attempt',
            reason: 'channel_selected',
            source: 'routing',
          },
        },
      ],
    },
    diagnostics: {
      method: 'POST',
      path: '/v1/responses',
      ip: '203.0.113.10',
      client: 'codex',
      node: 'gateway-node-a',
      status_code: 200,
      request_size: 1536,
      upstream_request_size: 2048,
      response_size: 3072,
      request_protocol: 'responses',
      route_pool_name: '测试路由转换',
      route_rule_id: 3,
    },
    request_conversion: ['responses', 'chat'],
    reasoning_effort: 'high',
    is_system_prompt_overwritten: true,
    is_model_mapped: true,
    upstream_model_name: LEGACY_ACTUAL_MODEL,
    po: [`set ${LEGACY_PARAM_OVERRIDE}`],
  }),
  request_id: '',
  upstream_request_id: '',
}

interface RoutePresentationProps {
  scope: LogsViewScope
  log?: UsageLog
}

/** Routed log whose admin_info carries additional conversion diagnostics. */
function routedLogWithDiagnostics(
  diagnostics: Record<string, unknown>[]
): UsageLog {
  const other = JSON.parse(routedLog.other as string) as {
    admin_info: Record<string, unknown>
  }
  return {
    ...routedLog,
    other: JSON.stringify({
      ...other,
      admin_info: { ...other.admin_info, conversion_diagnostics: diagnostics },
    }),
  }
}

function useRequestedScope(scope: LogsViewScope) {
  const permissions = useLogsViewScope()
  const setViewScope = permissions.setViewScope

  useEffect(() => {
    setViewScope(scope)
  }, [setViewScope, scope])

  return permissions
}

function ModelColumnHarness(props: RoutePresentationProps) {
  const permissions = useRequestedScope(props.scope)
  const columns = useCommonLogsColumns(
    permissions.isAdminView,
    permissions.isRootView,
    permissions.canViewModelRoute
  )
  // eslint-disable-next-line react/incompatible-library -- This test exercises TanStack Table without compiler memoization.
  const table = useReactTable({
    data: [props.log ?? routedLog],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })
  const modelCell = table
    .getRowModel()
    .rows[0]?.getVisibleCells()
    .find((cell) => cell.column.id === 'model_name')

  if (!modelCell) {
    throw new Error('Expected the common usage-log model column to be present')
  }

  return (
    <div
      data-testid='model-column'
      data-view-scope={permissions.viewScope}
      data-admin-view={String(permissions.isAdminView)}
      data-can-view-route={String(permissions.canViewModelRoute)}
    >
      {flexRender(modelCell.column.columnDef.cell, modelCell.getContext())}
    </div>
  )
}

function DetailsDialogHarness(props: RoutePresentationProps) {
  const permissions = useRequestedScope(props.scope)

  return (
    <div
      data-testid='details-permissions'
      data-view-scope={permissions.viewScope}
      data-admin-view={String(permissions.isAdminView)}
      data-can-view-route={String(permissions.canViewModelRoute)}
    >
      <DetailsDialog
        log={props.log ?? routedLog}
        isAdminView={permissions.isAdminView}
        isRoot={permissions.isRootView}
        canViewModelRoute={permissions.canViewModelRoute}
        open
        onOpenChange={() => undefined}
      />
    </div>
  )
}

function setAuthenticatedRole(role: number) {
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'tester',
    role,
  })
}

function renderModelColumn(role: number, scope: LogsViewScope, log?: UsageLog) {
  setAuthenticatedRole(role)
  return render(
    <UsageLogsProvider>
      <ModelColumnHarness scope={scope} log={log} />
    </UsageLogsProvider>
  )
}

function renderDetailsDialog(
  role: number,
  scope: LogsViewScope,
  log?: UsageLog
) {
  setAuthenticatedRole(role)
  return render(
    <UsageLogsProvider>
      <DetailsDialogHarness scope={scope} log={log} />
    </UsageLogsProvider>
  )
}

afterEach(() => {
  useAuthStore.getState().auth.setUser(null)
})

describe('usage-log model route component visibility', () => {
  test('puts a short translation label below the model without enlarging its content height', async () => {
    renderModelColumn(ROLE.ADMIN, 'all')
    const cell = screen.getByTestId('model-column')
    const label = await within(cell).findByText('chat translation')
    const badge = label.closest('[data-slot="status-badge"]')
    expect(badge).toHaveClass('h-3', 'leading-3')
    expect(badge?.parentElement).toHaveClass('flex-col', 'items-start')
    expect(badge?.previousElementSibling).toHaveTextContent(REQUESTED_MODEL)
    expect(cell).not.toHaveTextContent('→')
  })

  test('marks a translation that adjusted fields with an inline warning icon', async () => {
    renderModelColumn(
      ROLE.ADMIN,
      'all',
      routedLogWithDiagnostics([
        {
          code: 'omitted_presentation_metadata',
          severity: 'warning',
          loss_class: 'presentation',
          path: 'metadata',
          message: 'target protocol does not carry this display metadata',
        },
      ])
    )
    const cell = screen.getByTestId('model-column')
    const label = await within(cell).findByText('chat translation')
    const badge = label.closest('[data-slot="status-badge"]')
    expect(badge).toHaveClass('text-info')
    const icon = cell.querySelector<HTMLElement>(
      '[data-protocol-field-adjustment]'
    )
    expect(icon).not.toBeNull()
    expect(icon).toHaveClass('text-amber-600', 'dark:text-amber-400')
    expect(badge).toContainElement(icon)
    expect(badge?.lastElementChild).toBe(icon)
    expect(icon).toHaveAttribute(
      'title',
      'Fields were adjusted during conversion'
    )
  })

  test('keeps a translation without field adjustments unmarked', async () => {
    renderModelColumn(ROLE.ADMIN, 'all')
    const cell = screen.getByTestId('model-column')
    const label = await within(cell).findByText('chat translation')
    const badge = label.closest('[data-slot="status-badge"]')
    expect(badge).toHaveClass('text-info')
    expect(
      cell.querySelector('[data-protocol-field-adjustment]')
    ).not.toBeInTheDocument()
  })

  test('marks a translation that dropped execution tuning with the warning icon', async () => {
    renderModelColumn(
      ROLE.ADMIN,
      'all',
      routedLogWithDiagnostics([
        {
          code: 'unsupported_search_limit',
          severity: 'warning',
          loss_class: 'tuning',
          path: 'tools[0].max_uses',
          message: 'Chat Completions cannot preserve Claude max_uses',
        },
      ])
    )
    const cell = screen.getByTestId('model-column')
    const label = await within(cell).findByText('chat translation')
    const badge = label.closest('[data-slot="status-badge"]')
    const icon = cell.querySelector<HTMLElement>(
      '[data-protocol-field-adjustment]'
    )
    expect(icon).not.toBeNull()
    expect(badge).toContainElement(icon)
    expect(icon).toHaveAttribute(
      'title',
      'Fields were adjusted during conversion'
    )
  })

  test.each([
    [ROLE.USER, 'all'],
    [ROLE.ADMIN, 'self'],
  ] as const)(
    'hides the translation label for role %s in scope %s',
    async (role, scope) => {
      renderModelColumn(role, scope)
      const cell = screen.getByTestId('model-column')
      await waitFor(() =>
        expect(cell).toHaveAttribute('data-admin-view', 'false')
      )
      expect(
        within(cell).queryByText('chat translation')
      ).not.toBeInTheDocument()
    }
  )

  test.each([
    { type: 2, other: JSON.stringify({ request_conversion: ['responses'] }) },
    { type: 5, other: routedLog.other },
  ])('does not label native or failed requests as translated', (partialLog) => {
    renderModelColumn(ROLE.ADMIN, 'all', { ...routedLog, ...partialLog })
    expect(screen.getByTestId('model-column')).not.toHaveTextContent(
      'translation'
    )
  })

  test('hides an unexpected model route payload from a regular-user list cell', async () => {
    renderModelColumn(ROLE.USER, 'all')

    await waitFor(() => {
      expect(screen.getByTestId('model-column')).toHaveAttribute(
        'data-can-view-route',
        'false'
      )
    })

    expect(screen.getByText(REQUESTED_MODEL)).toBeVisible()
    expect(screen.queryByText(NESTED_ACTUAL_MODEL)).not.toBeInTheDocument()
    expect(screen.queryByText(LEGACY_ACTUAL_MODEL)).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: `Model: ${REQUESTED_MODEL}` })
    ).not.toBeInTheDocument()
  })

  test('hides an unexpected model route payload from a regular-user details dialog', async () => {
    renderDetailsDialog(ROLE.USER, 'all')

    await waitFor(() => {
      expect(screen.getByTestId('details-permissions')).toHaveAttribute(
        'data-can-view-route',
        'false'
      )
    })

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).queryByText('Model Mapping')).not.toBeInTheDocument()
    expect(
      within(dialog).queryByText(NESTED_ACTUAL_MODEL)
    ).not.toBeInTheDocument()
    expect(
      within(dialog).queryByText(LEGACY_ACTUAL_MODEL)
    ).not.toBeInTheDocument()
    expect(
      within(dialog).queryByText(NESTED_PARAM_OVERRIDE)
    ).not.toBeInTheDocument()
    expect(
      within(dialog).queryByText(LEGACY_PARAM_OVERRIDE)
    ).not.toBeInTheDocument()
    expect(
      within(dialog).queryByText('Request Diagnostics')
    ).not.toBeInTheDocument()
    expect(
      within(dialog).queryByText('Safe Request Headers')
    ).not.toBeInTheDocument()
    expect(
      within(dialog).queryByText('Request Conversion')
    ).not.toBeInTheDocument()
  })

  test.each([
    ['administrator all scope', 'all', true],
    ['administrator self scope', 'self', false],
  ] as const)(
    'limits request internals in the %s details dialog',
    async (_label, scope, shouldShow) => {
      renderDetailsDialog(ROLE.ADMIN, scope)

      await waitFor(() => {
        expect(screen.getByTestId('details-permissions')).toHaveAttribute(
          'data-admin-view',
          String(shouldShow)
        )
      })

      const dialog = screen.getByRole('dialog')
      expect(
        within(dialog).queryByText('Request Diagnostics')
      ).not.toBeInTheDocument()
      if (shouldShow) {
        expect(within(dialog).getByText('Payload Size')).toBeVisible()
        expect(within(dialog).getByText(/1\.5 KiB/)).toBeVisible()
        expect(within(dialog).getByText('Safe Request Headers')).toBeVisible()
        expect(within(dialog).getByText('Request Conversion')).toBeVisible()
        expect(
          within(dialog).getByRole('button', {
            name: /Request policy decisions/,
          })
        ).toBeVisible()
      } else {
        expect(
          within(dialog).queryByText('Payload Size')
        ).not.toBeInTheDocument()
        expect(
          within(dialog).queryByText('Safe Request Headers')
        ).not.toBeInTheDocument()
        expect(
          within(dialog).queryByText('Request Conversion')
        ).not.toBeInTheDocument()
        expect(
          within(dialog).queryByRole('button', {
            name: /Request policy decisions/,
          })
        ).not.toBeInTheDocument()
      }
    }
  )

  test('keeps policy decisions and safe request headers collapsed while showing payload size inline', async () => {
    renderDetailsDialog(ROLE.ADMIN, 'all')

    await waitFor(() => {
      expect(screen.getByTestId('details-permissions')).toHaveAttribute(
        'data-admin-view',
        'true'
      )
    })

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('Payload Size')).toBeVisible()
    expect(within(dialog).getByText(/2\.0 KiB/)).toBeVisible()
    expect(within(dialog).getByText(/3\.0 KiB/)).toBeVisible()
    expect(within(dialog).queryByText('Request Diagnostics')).toBeNull()
    expect(within(dialog).queryByText('gateway-node-a')).toBeNull()
    expect(within(dialog).queryByText('Route Pool')).toBeNull()
    const policyTrigger = within(dialog).getByRole('button', {
      name: /Request policy decisions/,
    })
    expect(policyTrigger).toHaveAttribute('aria-expanded', 'false')
    expect(within(dialog).queryByText('Channel selected')).toBeNull()
    const headerTrigger = within(dialog).getByRole('button', {
      name: /Safe Request Headers/,
    })
    expect(headerTrigger).toHaveAttribute('aria-expanded', 'false')
    expect(within(dialog).getByText('user-agent')).toBeVisible()
    expect(within(dialog).getByText('codex-cli/1.0')).toBeVisible()
    expect(
      within(dialog).queryByText('x-codex-thread-id')
    ).not.toBeInTheDocument()
    expect(
      within(dialog).queryByText('sha256:test-thread')
    ).not.toBeInTheDocument()
  })

  test('expands remaining safe request headers without duplicating user-agent', async () => {
    const user = userEvent.setup()
    renderDetailsDialog(ROLE.ADMIN, 'all')

    const dialog = screen.getByRole('dialog')
    const headerTrigger = within(dialog).getByRole('button', {
      name: /Safe Request Headers/,
    })

    await user.click(headerTrigger)

    expect(headerTrigger).toHaveAttribute('aria-expanded', 'true')
    expect(within(dialog).getByText('x-codex-thread-id')).toBeVisible()
    expect(within(dialog).getByText('sha256:test-thread')).toBeVisible()
    expect(within(dialog).getAllByText('user-agent')).toHaveLength(1)
  })

  test('shows WebSocket timing and aligns colored reasoning effort with overview rows', async () => {
    renderDetailsDialog(ROLE.ADMIN, 'all')

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('Connection Type')).toBeVisible()
    expect(within(dialog).getByText('WebSocket')).toBeVisible()

    const responseTimeRow = within(dialog)
      .getByText('Response Time')
      .closest('.grid')
    expect(responseTimeRow).toHaveTextContent('0.9s')
    expect(responseTimeRow).toHaveTextContent('(First token: 0.3s)')

    const reasoningValue = within(dialog).getByText('high')
    const reasoningBadge = reasoningValue.closest('[data-slot="status-badge"]')
    expect(reasoningBadge).toHaveClass('text-warning')
    expect(reasoningBadge).not.toHaveClass('rounded-4xl')

    const connectionRow = within(dialog)
      .getByText('Connection Type')
      .closest('.grid')
    const reasoningRow = within(dialog)
      .getByText('Reasoning Effort')
      .closest('.grid')
    expect(reasoningRow?.parentElement).toBe(connectionRow?.parentElement)

    const systemPromptRow = within(dialog)
      .getByText('System Prompt')
      .closest('.grid')
    expect(systemPromptRow?.parentElement).toBe(connectionRow?.parentElement)
    const systemPromptValue = within(dialog).getByText('Overwritten')
    const systemPromptBadge = systemPromptValue.closest(
      '[data-slot="status-badge"]'
    )
    expect(systemPromptBadge).toHaveClass('font-mono', 'text-muted-foreground')
    expect(systemPromptBadge).not.toHaveClass('rounded-4xl')
  })

  test('explains a radar-replaced reasoning tier beside the applied tier', () => {
    const autoAppliedLog: UsageLog = {
      ...routedLog,
      other: JSON.stringify({
        reasoning_effort: 'xhigh',
        reasoning_effort_auto: {
          applied: true,
          policy: 'highest_iq',
          iq: 128.5,
          from: 'low',
          to: 'xhigh',
        },
      }),
    }

    renderDetailsDialog(ROLE.USER, 'all', autoAppliedLog)

    const dialog = screen.getByRole('dialog')
    const autoRow = within(dialog)
      .getByText('Automatic reasoning effort')
      .closest('.grid')
    expect(autoRow).toHaveTextContent('low')
    expect(autoRow).toHaveTextContent('xhigh')
    expect(autoRow).toHaveTextContent('Highest IQ')
    expect(autoRow).toHaveTextContent('IQ 128.5')
  })

  test('omits the automatic tier row when the radar kept the requested tier', () => {
    const autoSkippedLog: UsageLog = {
      ...routedLog,
      other: JSON.stringify({
        reasoning_effort: 'high',
        reasoning_effort_auto: { applied: false, reason: 'client_default' },
      }),
    }

    renderDetailsDialog(ROLE.USER, 'all', autoSkippedLog)

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('Reasoning Effort')).toBeVisible()
    expect(within(dialog).queryByText('Automatic reasoning effort')).toBeNull()
  })

  test('explains a skip the reader cannot infer, such as stale radar data', () => {
    const staleLog: UsageLog = {
      ...routedLog,
      other: JSON.stringify({
        reasoning_effort: 'high',
        reasoning_effort_auto: {
          applied: false,
          reason: 'stale',
          policy: 'highest_iq',
          iq: 121.88,
        },
      }),
    }

    renderDetailsDialog(ROLE.USER, 'all', staleLog)

    const dialog = screen.getByRole('dialog')
    const autoRow = within(dialog)
      .getByText('Automatic reasoning effort')
      .closest('.grid')
    expect(autoRow).toHaveTextContent('Not applied')
    expect(autoRow).toHaveTextContent('The radar data is stale.')
    expect(autoRow).not.toHaveTextContent('IQ 121.88')
  })

  test('shows WebSocket stream status to the log owner', async () => {
    const streamStatusLog: UsageLog = {
      ...routedLog,
      is_stream: true,
      other: JSON.stringify({
        transport: 'websocket',
        stream_status: {
          status: 'error',
          end_reason: 'timeout',
          error_count: 1,
          end_error: 'upstream timed out',
          errors: ['recoverable frame error'],
        },
      }),
    }

    renderDetailsDialog(ROLE.USER, 'all', streamStatusLog)

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('Connection Type')).toBeVisible()
    expect(within(dialog).getByText('WebSocket')).toBeVisible()
    expect(within(dialog).getByText('Stream Status')).toBeVisible()
    expect(within(dialog).getByText('timeout')).toBeVisible()
    expect(within(dialog).getByText('upstream timed out')).toBeVisible()
    expect(within(dialog).getByText('recoverable frame error')).toBeVisible()
  })

  test('places payload size and policy decisions before request conversion', async () => {
    renderDetailsDialog(ROLE.ADMIN, 'all')

    await waitFor(() => {
      expect(screen.getByTestId('details-permissions')).toHaveAttribute(
        'data-admin-view',
        'true'
      )
    })

    const dialog = screen.getByRole('dialog')
    const payloadLabel = within(dialog).getByText('Payload Size')
    const policyHeading = within(dialog).getByRole('button', {
      name: /Request policy decisions/,
    })
    const conversionHeading = within(dialog).getByText('Request Conversion')

    expect(
      payloadLabel.compareDocumentPosition(policyHeading) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).not.toBe(0)
    expect(
      policyHeading.compareDocumentPosition(conversionHeading) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).not.toBe(0)
  })

  test('reads upstream protocol metadata from administrator-only log data', async () => {
    renderDetailsDialog(ROLE.ADMIN, 'all')

    await waitFor(() => {
      expect(screen.getByTestId('details-permissions')).toHaveAttribute(
        'data-admin-view',
        'true'
      )
    })

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('Upstream request')).toBeVisible()
    expect(within(dialog).getByText('OpenAI Chat Completions')).toBeVisible()
    // Technical details are always visible in the conversion section.
    expect(within(dialog).getByText('Protocol Converter')).toBeVisible()
    expect(within(dialog).getByText('Protocol State Mode')).toBeVisible()
    expect(
      within(dialog).getByText('Resend full conversation history')
    ).toBeVisible()
    expect(within(dialog).queryByText('Route Pool')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('测试路由转换')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('Route Rule')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('#3')).not.toBeInTheDocument()
    expect(within(dialog).getByText('responses_to_chat')).toBeVisible()
    expect(
      within(dialog)
        .getByText('Request Conversion')
        .closest('label')
        ?.querySelector('svg')
    ).toBeNull()
    const flowRows = within(dialog).getAllByText(
      /Client request|Upstream request|Request path/
    )
    expect(flowRows.length).toBeGreaterThanOrEqual(3)
  })

  test.each([
    ['administrator all scope', ROLE.ADMIN, 'all', true],
    ['administrator self scope', ROLE.ADMIN, 'self', false],
    ['super administrator all scope', ROLE.SUPER_ADMIN, 'all', true],
    ['super administrator self scope', ROLE.SUPER_ADMIN, 'self', false],
  ] as const)(
    'shows model routing in the %s list cell',
    async (_label, role, scope, isAdminView) => {
      const user = userEvent.setup()
      renderModelColumn(role, scope)
      const presentation = screen.getByTestId('model-column')

      await waitFor(() => {
        expect(presentation).toHaveAttribute('data-view-scope', scope)
        expect(presentation).toHaveAttribute(
          'data-admin-view',
          String(isAdminView)
        )
        expect(presentation).toHaveAttribute('data-can-view-route', 'true')
      })

      const trigger = screen.getByRole('button', {
        name: `Model: ${REQUESTED_MODEL}`,
      })

      await user.click(trigger)

      expect(await screen.findByText(NESTED_ACTUAL_MODEL)).toBeVisible()
      expect(screen.queryByText(LEGACY_ACTUAL_MODEL)).not.toBeInTheDocument()
    }
  )

  test.each([
    ['administrator all scope', ROLE.ADMIN, 'all', true],
    ['administrator self scope', ROLE.ADMIN, 'self', false],
    ['super administrator all scope', ROLE.SUPER_ADMIN, 'all', true],
    ['super administrator self scope', ROLE.SUPER_ADMIN, 'self', false],
  ] as const)(
    'keeps model mapping out of the %s details dialog',
    async (_label, role, scope, isAdminView) => {
      renderDetailsDialog(role, scope)
      const permissions = screen.getByTestId('details-permissions')

      await waitFor(() => {
        expect(permissions).toHaveAttribute('data-view-scope', scope)
        expect(permissions).toHaveAttribute(
          'data-admin-view',
          String(isAdminView)
        )
        expect(permissions).toHaveAttribute('data-can-view-route', 'true')
      })

      const dialog = screen.getByRole('dialog')
      expect(
        within(dialog).queryByText('Model Mapping')
      ).not.toBeInTheDocument()
      expect(
        within(dialog).queryByText(NESTED_ACTUAL_MODEL)
      ).not.toBeInTheDocument()
      expect(
        within(dialog).queryByText(LEGACY_ACTUAL_MODEL)
      ).not.toBeInTheDocument()
      expect(within(dialog).getByText(NESTED_PARAM_OVERRIDE)).toBeVisible()
      expect(
        within(dialog).queryByText(LEGACY_PARAM_OVERRIDE)
      ).not.toBeInTheDocument()
    }
  )

  test('shows a historical top-level param override when nested audit data is absent', async () => {
    const legacyOverrideLog = {
      ...routedLog,
      other: JSON.stringify({
        po: [`set ${LEGACY_PARAM_OVERRIDE}`],
      }),
    }

    renderDetailsDialog(ROLE.ADMIN, 'all', legacyOverrideLog)

    await waitFor(() => {
      expect(screen.getByTestId('details-permissions')).toHaveAttribute(
        'data-can-view-route',
        'true'
      )
    })

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText(LEGACY_PARAM_OVERRIDE)).toBeVisible()
  })
})
