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
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: () => null,
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
  completion_tokens: 0,
  use_time: 0,
  is_stream: false,
  channel: 7,
  channel_name: 'admin-only-channel',
  token_id: 1,
  group: 'default',
  ip: '',
  other: JSON.stringify({
    admin_info: {
      is_model_mapped: true,
      upstream_model_name: NESTED_ACTUAL_MODEL,
      po: [`set ${NESTED_PARAM_OVERRIDE}`],
      request_headers: {
        'user-agent': 'codex-cli/1.0',
      },
      upstream_protocol: 'chat',
      protocol_converter: 'responses_to_chat',
      protocol_state_mode: 'replay',
    },
    diagnostics: {
      method: 'POST',
      path: '/v1/responses',
      ip: '203.0.113.10',
      client: 'codex',
      request_protocol: 'responses',
    },
    request_conversion: ['responses', 'chat'],
    reasoning_effort: 'high',
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
    permissions.canViewModelRoute
  )
  const table = useReactTable({
    data: [routedLog],
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

function renderModelColumn(role: number, scope: LogsViewScope) {
  setAuthenticatedRole(role)
  return render(
    <UsageLogsProvider>
      <ModelColumnHarness scope={scope} />
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
      screen.queryByRole('button', { name: REQUESTED_MODEL })
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
      if (shouldShow) {
        expect(within(dialog).getByText('Request Diagnostics')).toBeVisible()
        expect(within(dialog).getByText('Safe Request Headers')).toBeVisible()
        expect(within(dialog).getByText('Request Conversion')).toBeVisible()
      } else {
        expect(
          within(dialog).queryByText('Request Diagnostics')
        ).not.toBeInTheDocument()
        expect(
          within(dialog).queryByText('Safe Request Headers')
        ).not.toBeInTheDocument()
        expect(
          within(dialog).queryByText('Request Conversion')
        ).not.toBeInTheDocument()
      }
    }
  )

  test('keeps diagnostic headings and the IP row text-only for administrators', async () => {
    renderDetailsDialog(ROLE.ADMIN, 'all')

    await waitFor(() => {
      expect(screen.getByTestId('details-permissions')).toHaveAttribute(
        'data-admin-view',
        'true'
      )
    })

    const dialog = screen.getByRole('dialog')
    const diagnosticHeading = within(dialog).getByText('Request Diagnostics')
    const headerHeading = within(dialog).getByText('Safe Request Headers')
    const ipLabel = within(dialog).getByText('IP Address')

    expect(diagnosticHeading.closest('label')?.querySelector('svg')).toBeNull()
    expect(headerHeading.closest('label')?.querySelector('svg')).toBeNull()
    expect(ipLabel.closest('div')?.querySelector('svg')).toBeNull()
  })

  test('places reasoning effort before request diagnostics', async () => {
    renderDetailsDialog(ROLE.ADMIN, 'all')

    await waitFor(() => {
      expect(screen.getByTestId('details-permissions')).toHaveAttribute(
        'data-admin-view',
        'true'
      )
    })

    const dialog = screen.getByRole('dialog')
    const reasoningLabel = within(dialog).getByText('Reasoning Effort')
    const diagnosticsHeading = within(dialog).getByText('Request Diagnostics')

    expect(
      reasoningLabel.compareDocumentPosition(diagnosticsHeading) &
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
    expect(within(dialog).getByText('Upstream Protocol')).toBeVisible()
    expect(within(dialog).getByText('Protocol Converter')).toBeVisible()
    expect(within(dialog).getByText('Protocol State Mode')).toBeVisible()
    expect(within(dialog).getByText('replay')).toBeVisible()
    expect(
      within(dialog).getByText('responses → responses_to_chat → chat')
    ).toBeVisible()
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

      const trigger = screen.getByRole('button', { name: REQUESTED_MODEL })

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
    'shows model routing in the %s details dialog',
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
      expect(within(dialog).getByText('Model Mapping')).toBeVisible()
      expect(within(dialog).getByText(NESTED_ACTUAL_MODEL)).toBeVisible()
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
