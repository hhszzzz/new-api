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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { PromptAuditDeleteDialog } from '../components/prompt-audit-delete-dialog'
import { PromptAuditDetailSheet } from '../components/prompt-audit-detail-sheet'
import type { PromptAuditEvent } from '../types'

const {
  deletePromptAuditsMock,
  getPromptAuditMock,
  previewPromptAuditDeleteMock,
  retryPromptAuditMock,
} = vi.hoisted(() => ({
  deletePromptAuditsMock: vi.fn(),
  getPromptAuditMock: vi.fn(),
  previewPromptAuditDeleteMock: vi.fn(),
  retryPromptAuditMock: vi.fn(),
}))

vi.mock('../api', () => ({
  deletePromptAudits: deletePromptAuditsMock,
  getPromptAudit: getPromptAuditMock,
  previewPromptAuditDelete: previewPromptAuditDeleteMock,
  retryPromptAudit: retryPromptAuditMock,
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string | number>) =>
      Object.entries(values || {}).reduce(
        (result, [name, value]) => result.replace(`{{${name}}}`, String(value)),
        key
      ),
  }),
}))

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

const EVENT: PromptAuditEvent = {
  id: 17,
  request_id: 'req-prompt-audit',
  user_id: 9,
  token_id: 3,
  token_name: 'production',
  group: 'default',
  protocol: 'openai_responses',
  model: 'gpt-test',
  stage: 'responses_websocket',
  config_version: 'config-v1',
  execution_mode: 'blocking',
  status: 'done',
  prompt_hash: 'a'.repeat(64),
  prompt_length: 21,
  segment_count: 1,
  chunk_count: 1,
  full_prompt: 'raw-secret-prompt',
  full_prompt_available: true,
  full_prompt_truncated: false,
  redacted_preview: 'redacted-preview',
  safety: 'Safe',
  decision: 'pass',
  would_action: 'pass',
  categories: [],
  unknown_categories: [],
  endpoint_id: 'guard-primary',
  latency_ms: 8,
  attempts: 1,
  max_attempts: 4,
  next_attempt_at: 0,
  error_code: '',
  created_at: 1_785_000_000,
  updated_at: 1_785_000_000,
  completed_at: 1_785_000_001,
}

function renderWithQueryClient(children: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  return render(
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

describe('prompt audit management components', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getPromptAuditMock.mockResolvedValue({ success: true, data: EVENT })
    retryPromptAuditMock.mockResolvedValue({ success: true })
    deletePromptAuditsMock.mockResolvedValue({
      success: true,
      data: { deleted_count: 2 },
    })
  })

  test('never renders an API-provided full prompt without the full-prompt permission', async () => {
    renderWithQueryClient(
      <PromptAuditDetailSheet
        eventID={EVENT.id}
        canViewFullPrompt={false}
        canManage={false}
        canDelete={false}
        onOpenChange={vi.fn()}
        onDelete={vi.fn()}
      />
    )

    expect(await screen.findByText('redacted-preview')).toBeVisible()
    expect(screen.queryByText('raw-secret-prompt')).not.toBeInTheDocument()
    expect(
      screen.getByText('Your permission only allows the redacted preview.')
    ).toBeVisible()
  })

  test('blocks deletion when the preview contains active tasks', async () => {
    previewPromptAuditDeleteMock.mockResolvedValue({
      success: true,
      data: { eligible_count: 2, active_count: 1, max_id: 42 },
    })

    renderWithQueryClient(
      <PromptAuditDeleteDialog
        open
        filter={{ status: 'done' }}
        onOpenChange={vi.fn()}
        onDeleted={vi.fn()}
      />
    )

    expect(
      await screen.findByText(
        'Active tasks cannot be deleted. Narrow the filters and preview again.'
      )
    ).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Delete permanently' })
    ).toBeDisabled()
    expect(deletePromptAuditsMock).not.toHaveBeenCalled()
  })

  test('deletes only with the exact preview confirmation', async () => {
    const user = userEvent.setup()
    const preview = { eligible_count: 2, active_count: 0, max_id: 42 }
    previewPromptAuditDeleteMock.mockResolvedValue({
      success: true,
      data: preview,
    })
    const onDeleted = vi.fn()

    renderWithQueryClient(
      <PromptAuditDeleteDialog
        open
        filter={{ status: 'done' }}
        onOpenChange={vi.fn()}
        onDeleted={onDeleted}
      />
    )

    const confirm = await screen.findByRole('button', {
      name: 'Delete permanently',
    })
    await waitFor(() => expect(confirm).toBeEnabled())
    await user.click(confirm)

    await waitFor(() => {
      expect(deletePromptAuditsMock).toHaveBeenCalledWith(
        { status: 'done' },
        preview
      )
    })
    expect(onDeleted).toHaveBeenCalledOnce()
  })
})
