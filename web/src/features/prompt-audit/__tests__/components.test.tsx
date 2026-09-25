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
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { PromptAuditDeleteDialog } from '../components/prompt-audit-delete-dialog'
import { PromptAuditDetailSheet } from '../components/prompt-audit-detail-sheet'
import type { PromptAuditEvent } from '../types'

const {
  deletePromptAuditsMock,
  getPromptAuditMock,
  previewPromptAuditDeleteMock,
  retryPromptAuditMock,
  reviewPromptAuditMock,
} = vi.hoisted(() => ({
  deletePromptAuditsMock: vi.fn(),
  getPromptAuditMock: vi.fn(),
  previewPromptAuditDeleteMock: vi.fn(),
  retryPromptAuditMock: vi.fn(),
  reviewPromptAuditMock: vi.fn(),
}))

vi.mock('../api', () => ({
  deletePromptAudits: deletePromptAuditsMock,
  getPromptAudit: getPromptAuditMock,
  previewPromptAuditDelete: previewPromptAuditDeleteMock,
  retryPromptAudit: retryPromptAuditMock,
  reviewPromptAudit: reviewPromptAuditMock,
}))

// The few translations a test needs; every other key reads as itself.
const translations = vi.hoisted(() => ({}) as Record<string, string>)

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string | number>) =>
      Object.entries(values || {}).reduce(
        (result, [name, value]) => result.replace(`{{${name}}}`, String(value)),
        translations[key] ?? key
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
  username: 'audit-user',
  group: 'default',
  protocol: 'openai_responses',
  model: 'gpt-test',
  stage: 'responses_websocket',
  direction: 'input',
  generation_id: '',
  delivery_status: 'not_applicable',
  coverage_complete: true,
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
  refusal: '',
  decision: 'pass',
  action: 'allow',
  would_action: 'pass',
  categories: [],
  unknown_categories: [],
  endpoint_id: 'guard-primary',
  endpoint_model: 'qwen3guard-gen-0.6b',
  review_status: '',
  review_decision: '',
  review_codes: [],
  review_reason: '',
  reviewer_endpoint_id: '',
  human_review: '',
  human_review_reason: '',
  reviewed_by: 0,
  reviewer_name: '',
  reviewed_at: 0,
  latency_ms: 8,
  attempts: 1,
  max_attempts: 4,
  next_attempt_at: 0,
  error_code: '',
  ip: '192.0.2.1',
  user_agent: 'claude-code/2.0.30',
  method: 'POST',
  request_path: '/v1/responses',
  origin: 'https://console.example.com',
  referer: 'https://console.example.com/keys',
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
  afterEach(() => {
    for (const key of Object.keys(translations)) delete translations[key]
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

  test('shows the retained full prompt to an administrator with permission', async () => {
    renderWithQueryClient(
      <PromptAuditDetailSheet
        eventID={EVENT.id}
        canViewFullPrompt
        canManage={false}
        canDelete={false}
        onOpenChange={vi.fn()}
        onDelete={vi.fn()}
      />
    )

    expect(
      await screen.findByRole('button', { name: 'Full prompt' })
    ).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('raw-secret-prompt')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Full prompt' }))
    expect(await screen.findByText('raw-secret-prompt')).toBeVisible()
    expect(screen.queryByText('redacted-preview')).not.toBeInTheDocument()
  })

  test('tells an administrator viewing the full prompt what the copy holds', async () => {
    renderWithQueryClient(
      <PromptAuditDetailSheet
        eventID={EVENT.id}
        canViewFullPrompt
        canManage={false}
        canDelete={false}
        onOpenChange={vi.fn()}
        onDelete={vi.fn()}
      />
    )

    await userEvent.click(
      await screen.findByRole('button', { name: 'Full prompt' })
    )
    expect(await screen.findByText('raw-secret-prompt')).toBeVisible()
    expect(
      screen.getByText(
        'This is the whole request as sent, while Inspected content lists only the parts the audit submitted.'
      )
    ).toBeVisible()
    expect(
      screen.queryByText('Your permission only allows the redacted preview.')
    ).not.toBeInTheDocument()
  })

  test('names the protocol in the interface language', async () => {
    translations['Async Task'] = 'Tâche asynchrone'
    getPromptAuditMock.mockResolvedValue({
      success: true,
      data: { ...EVENT, protocol: 'task' },
    })

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

    expect(await screen.findByText('Tâche asynchrone')).toBeVisible()
  })

  test('shows inspected text by source and keeps the unselected source hidden', async () => {
    getPromptAuditMock.mockResolvedValue({
      success: true,
      data: {
        ...EVENT,
        scan_payload_truncated: true,
        scan_payload: JSON.stringify({
          segments: [
            { scope: 'user', text: 'Extracted user question' },
            { scope: 'skill', text: 'Long skill body '.repeat(80) },
          ],
        }),
      },
    })
    renderWithQueryClient(
      <PromptAuditDetailSheet
        eventID={EVENT.id}
        canViewFullPrompt
        canManage={false}
        canDelete={false}
        onOpenChange={vi.fn()}
        onDelete={vi.fn()}
      />
    )
    expect(await screen.findByText('Extracted user question')).toBeVisible()
    expect(
      screen.queryByText('Long skill body '.repeat(80).trim())
    ).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('tab', { name: 'Skills' }))
    expect(screen.getByText('Long skill body '.repeat(80).trim())).toBeVisible()
    expect(
      screen.getByText(
        'Inspected content was truncated to the retention limit.'
      )
    ).toBeVisible()
  })

  test('never renders retained inspection text without content permission', async () => {
    getPromptAuditMock.mockResolvedValue({
      success: true,
      data: {
        ...EVENT,
        scan_payload: JSON.stringify({
          segments: [{ scope: 'mcp', text: 'MCP private content' }],
        }),
      },
    })
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
    expect(screen.queryByText('MCP private content')).not.toBeInTheDocument()
    expect(screen.queryByText('Inspected content')).not.toBeInTheDocument()
  })

  test('replaces the inspected scope list with one tab per inspected source', async () => {
    getPromptAuditMock.mockResolvedValue({
      success: true,
      data: {
        ...EVENT,
        inspected_scopes: ['user', 'tool_result'],
        scan_payload: JSON.stringify({
          segments: [
            { scope: 'user', text: 'Extracted user question' },
            { scope: 'tool_result', text: 'Extracted tool output' },
          ],
        }),
      },
    })

    renderWithQueryClient(
      <PromptAuditDetailSheet
        eventID={EVENT.id}
        canViewFullPrompt
        canManage={false}
        canDelete={false}
        onOpenChange={vi.fn()}
        onDelete={vi.fn()}
      />
    )

    // The scopes are the tabs, so the sources are named without a separate list
    // that only repeated them.
    expect(
      await screen.findByRole('tab', { name: 'User messages' })
    ).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tab', { name: 'Tool results' })).toHaveAttribute(
      'aria-selected',
      'false'
    )
    expect(screen.getByText('Extracted user question')).toBeVisible()
    expect(screen.queryByText('Extracted tool output')).not.toBeInTheDocument()
    expect(screen.queryByText('Inspected scope')).not.toBeInTheDocument()
  })

  test('keeps one tab for a source the client sent in more than one block', async () => {
    getPromptAuditMock.mockResolvedValue({
      success: true,
      data: {
        ...EVENT,
        scan_payload: JSON.stringify({
          segments: [
            { scope: 'user', text: '<command-name>/model</command-name>' },
            { scope: 'system', text: 'System instructions' },
            { scope: 'user', text: 'Latest question' },
          ],
        }),
      },
    })

    renderWithQueryClient(
      <PromptAuditDetailSheet
        eventID={EVENT.id}
        canViewFullPrompt
        canManage={false}
        canDelete={false}
        onOpenChange={vi.fn()}
        onDelete={vi.fn()}
      />
    )

    // Both blocks are one source, so they read as one tab holding both rather
    // than as two tabs under the same name.
    expect(
      await screen.findByRole('tab', { name: 'User messages' })
    ).toHaveAttribute('aria-selected', 'true')
    expect(screen.getAllByRole('tab', { name: 'User messages' })).toHaveLength(1)
    const panel = await screen.findByRole('tabpanel')
    expect(panel).toHaveTextContent('<command-name>/model</command-name>')
    expect(panel).toHaveTextContent('Latest question')
  })

  test('keeps the source tabs one row of tabs sized to their own names', async () => {
    getPromptAuditMock.mockResolvedValue({
      success: true,
      data: {
        ...EVENT,
        scan_payload: JSON.stringify({
          segments: [
            { scope: 'user', text: 'Extracted user question' },
            { scope: 'tool_result', text: 'Extracted tool output' },
          ],
        }),
      },
    })

    renderWithQueryClient(
      <PromptAuditDetailSheet
        eventID={EVENT.id}
        canViewFullPrompt
        canManage={false}
        canDelete={false}
        onOpenChange={vi.fn()}
        onDelete={vi.fn()}
      />
    )

    // Fixed size instead of a stretched one: the list is as wide as its tabs, and
    // the row it sits in scrolls sideways rather than growing taller.
    const tablist = await screen.findByRole('tablist')
    expect(tablist.className).toContain('min-w-max')
    expect(tablist.parentElement?.className).toContain('overflow-x-auto')
  })

  test('shows the selected source text when its tab is chosen', async () => {
    getPromptAuditMock.mockResolvedValue({
      success: true,
      data: {
        ...EVENT,
        scan_payload: JSON.stringify({
          segments: [
            { scope: 'user', text: 'Extracted user question' },
            { scope: 'tool_result', text: 'Extracted tool output' },
          ],
        }),
      },
    })

    renderWithQueryClient(
      <PromptAuditDetailSheet
        eventID={EVENT.id}
        canViewFullPrompt
        canManage={false}
        canDelete={false}
        onOpenChange={vi.fn()}
        onDelete={vi.fn()}
      />
    )

    await userEvent.click(
      await screen.findByRole('tab', { name: 'Tool results' })
    )
    expect(screen.getByText('Extracted tool output')).toBeVisible()
    expect(
      screen.queryByText('Extracted user question')
    ).not.toBeInTheDocument()
  })

  test('records no inspected scope for a wordlist-only row', async () => {
    getPromptAuditMock.mockResolvedValue({
      success: true,
      data: {
        ...EVENT,
        inspection_type: 'wordlist',
        matched_scope: 'user',
        inspected_scopes: [],
      },
    })

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

    // The hit is reported as the text source, and the inspected-content section
    // is absent because this viewer may not read retained content at all.
    expect(await screen.findByText('Text source')).toBeVisible()
    expect(screen.getByText('User messages')).toBeVisible()
    expect(screen.queryByText('Inspected content')).not.toBeInTheDocument()
  })

  test('hides the related log links for a request the audit rejected', async () => {
    getPromptAuditMock.mockResolvedValue({
      success: true,
      data: { ...EVENT, decision: 'block', action: 'block' },
    })

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

    expect(await screen.findByText('Text source')).toBeVisible()
    expect(
      screen.queryByRole('link', { name: /Related/ })
    ).not.toBeInTheDocument()
  })

  test('links the logs of generated output the audit blocked', async () => {
    // Output is judged after upstream already produced and billed it, so its
    // usage and error logs exist whatever the verdict.
    getPromptAuditMock.mockResolvedValue({
      success: true,
      data: {
        ...EVENT,
        direction: 'output',
        decision: 'block',
        action: 'block',
      },
    })

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

    expect(
      await screen.findByRole('link', { name: /Related usage logs/ })
    ).toBeVisible()
    expect(
      screen.getByRole('link', { name: /Related error logs/ })
    ).toBeVisible()
  })

  test('links the usage and error logs of a request that was let through', async () => {
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

    expect(
      await screen.findByRole('link', { name: /Related usage logs/ })
    ).toHaveAttribute('href', '/usage-logs/common?requestId=req-prompt-audit')
    expect(
      screen.getByRole('link', { name: /Related error logs/ })
    ).toHaveAttribute(
      'href',
      '/usage-logs/common?type=5&requestId=req-prompt-audit'
    )
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
