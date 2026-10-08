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
import { render, screen, waitFor, within } from '@testing-library/react'
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

  test('keeps the full-prompt copy beside its header instead of on its own row', async () => {
    const user = userEvent.setup()
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

    const fullPrompt = await screen.findByRole('button', {
      name: 'Full prompt',
    })
    const copy = screen.getByRole('button', { name: 'Copy · Full prompt' })
    expect(fullPrompt.parentElement).toContainElement(copy)
    await user.click(copy)
    expect(await navigator.clipboard.readText()).toBe('raw-secret-prompt')
    // Copying reads the header control, so it must not also open the section.
    expect(fullPrompt).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('raw-secret-prompt')).not.toBeInTheDocument()
  })

  test.each([
    {
      inspection: undefined,
      shown: 'Audit model',
      hidden: 'Inspection method',
    },
    {
      inspection: 'wordlist',
      shown: 'Inspection method',
      hidden: 'Audit model',
    },
  ])(
    'names the detector once in the result, not as a header badge ($inspection)',
    async ({ inspection, shown, hidden }) => {
      getPromptAuditMock.mockResolvedValue({
        success: true,
        data: { ...EVENT, inspection_type: inspection },
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

      const result = await screen.findByRole('region', { name: 'Result' })
      expect(within(result).getByText(shown)).toBeVisible()
      expect(within(result).queryByText(hidden)).not.toBeInTheDocument()
      expect(screen.queryByText('Model audit')).not.toBeInTheDocument()
    }
  )

  test('keeps the record and request ids in the technical details, not the title', async () => {
    const user = userEvent.setup()
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

    await screen.findByText('Text source')
    // The title says what the record is, not which row it is.
    expect(screen.queryByText(`#${EVENT.id}`)).not.toBeInTheDocument()
    expect(screen.queryByText('Record ID')).not.toBeInTheDocument()
    expect(screen.queryByText('Request ID')).not.toBeInTheDocument()
    const result = screen.getByRole('region', { name: 'Result' })
    expect(within(result).queryByText('Endpoint')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Technical details' }))
    expect(await screen.findByText('Record ID')).toBeVisible()
    expect(screen.getByText(String(EVENT.id))).toBeVisible()
    expect(screen.getByText('Request ID')).toBeVisible()
    expect(screen.getByText(EVENT.request_id)).toBeVisible()
    expect(screen.getByText('Endpoint')).toBeVisible()
  })

  test('names the protocol in the interface language', async () => {
    const user = userEvent.setup()
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

    await user.click(
      await screen.findByRole('button', { name: 'Technical details' })
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
    // Prompt text is read as content, in the fixed-width face.
    expect(screen.getByText('Extracted user question')).toHaveClass('font-mono')
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
    expect(screen.getAllByRole('tab', { name: 'User messages' })).toHaveLength(
      1
    )
    const panel = await screen.findByRole('tabpanel')
    expect(panel).toHaveTextContent('<command-name>/model</command-name>')
    expect(panel).toHaveTextContent('Latest question')
    // The blocks are shown apart, so where one ends and the next begins stays
    // visible instead of reading as one run of text.
    expect(within(panel).getAllByRole('separator')).toHaveLength(1)
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
    expect(tablist).toHaveAttribute('data-variant', 'default')
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

  test('reads a stored record as a snapshot, not an inspection', async () => {
    getPromptAuditMock.mockResolvedValue({
      success: true,
      data: {
        ...EVENT,
        status: 'stored',
        decision: '',
        action: '',
        inspection_type: 'stored',
        latency_ms: 0,
        direction: 'output',
        delivery_status: 'delivered',
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

    expect(
      await screen.findByRole('region', { name: 'Content snapshot' })
    ).toBeVisible()
    expect(
      screen.queryByRole('region', { name: 'Inspected content' })
    ).not.toBeInTheDocument()
    const result = screen.getByRole('region', { name: 'Result' })
    // Nothing resembles a verdict that never happened: no action, no source
    // list, no latency; the direction's own delivery is what an output has.
    expect(
      within(result).queryByText('Inspection method')
    ).not.toBeInTheDocument()
    expect(within(result).queryByText('Audit model')).not.toBeInTheDocument()
    expect(within(result).queryByText('Safety')).not.toBeInTheDocument()
    expect(
      within(result).queryByText('Suggested action')
    ).not.toBeInTheDocument()
    expect(within(result).getByText('Delivery status')).toBeVisible()
    expect(within(result).queryByText('Actual action')).not.toBeInTheDocument()
    expect(within(result).queryByText('Text source')).not.toBeInTheDocument()
    expect(within(result).queryByText('Latency')).not.toBeInTheDocument()
  })

  test('reads the reply of an output record as its own source, never as history', async () => {
    getPromptAuditMock.mockResolvedValue({
      success: true,
      data: {
        ...EVENT,
        direction: 'output',
        scan_payload: JSON.stringify({
          segments: [{ scope: 'user', text: 'Extracted user question' }],
          output: 'The generated reply',
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

    expect(await screen.findByText('The generated reply')).toBeVisible()
    expect(
      screen.getByRole('tab', { name: 'Generated output' })
    ).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tab', { name: 'User messages' })).toBeVisible()
    expect(
      screen.queryByRole('tab', { name: 'Historical assistant messages' })
    ).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('tab', { name: 'User messages' }))
    expect(screen.getByText('Extracted user question')).toBeVisible()
  })

  test.each([
    {
      payload: undefined,
      truncated: false,
      message: 'No content snapshot retained',
    },
    {
      payload: JSON.stringify({ output: 'Retained reply' }),
      truncated: true,
      message: 'Content snapshot was truncated to the retention limit.',
    },
  ])(
    'describes a stored snapshot without claiming it was inspected ($truncated)',
    async ({ payload, truncated, message }) => {
      getPromptAuditMock.mockResolvedValue({
        success: true,
        data: {
          ...EVENT,
          status: 'stored',
          inspection_type: 'stored',
          scan_payload: payload,
          scan_payload_truncated: truncated,
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
      expect(await screen.findByText(message)).toBeVisible()
      expect(
        screen.queryByText('No inspected content retained')
      ).not.toBeInTheDocument()
      expect(
        screen.queryByText(
          'Inspected content was truncated to the retention limit.'
        )
      ).not.toBeInTheDocument()
      await userEvent.click(screen.getByRole('button', { name: 'Full prompt' }))
      expect(
        screen.getByText(
          'This is the retained full text, shown without source separation.'
        )
      ).toBeVisible()
      expect(
        screen.queryByText(
          'This is the whole request as sent, while Inspected content lists only the parts the audit submitted.'
        )
      ).not.toBeInTheDocument()
    }
  )

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
  // The score list is the record of what the audit actually measured, so it has
  // to read strongest-first and round the same way every time. A list that
  // disagreed with the verdict beside it would be worse than no list at all.
  test('lists audit model scores strongest first with two decimals', async () => {
    getPromptAuditMock.mockResolvedValue({
      success: true,
      data: {
        ...EVENT,
        scores: { violent: 0.9, pii: 0.25, probe: 0.5 },
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

    const block = (await screen.findByText('Audit model scores'))
      .parentElement as HTMLElement
    expect(
      within(block)
        .getAllByText(/^\d\.\d\d$/)
        .map((node) => node.textContent)
    ).toEqual(['0.90', '0.50', '0.25'])
    // Each value reads against its own name: a probe score is not a category.
    expect(
      within(block)
        .getAllByText(/^(violent|pii|Liveness probe)$/)
        .map((node) => node.textContent)
    ).toEqual(['violent', 'Liveness probe', 'pii'])
  })

  test('renders no score list for a record that carries none', async () => {
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
    expect(screen.queryByText('Audit model scores')).not.toBeInTheDocument()
  })

  test('frames the caller as one card instead of repeating identity rows', async () => {
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

    await screen.findByText('Text source')
    const caller = screen.getByRole('region', { name: 'Caller' })
    expect(caller).toHaveClass('border', 'bg-background')
    expect(
      within(caller).getByRole('heading', { name: 'Caller' })
    ).toBeVisible()
    expect(within(caller).getByText('audit-user')).toBeVisible()
    expect(within(caller).getByText('production')).toBeVisible()
    expect(within(caller).getByText(String(EVENT.user_id))).toBeVisible()
    expect(within(caller).getByText('default')).toBeVisible()
    // The identity labels appear exactly once, inside the framed card.
    expect(screen.getAllByText('Username')).toHaveLength(1)
    expect(screen.getAllByText('User ID')).toHaveLength(1)
    expect(screen.getAllByText('Token')).toHaveLength(1)
    expect(screen.getAllByText('Group')).toHaveLength(1)
    expect(screen.queryByText('Request ID')).not.toBeInTheDocument()
  })

  test('carries the creation time in the header and the requested model in the context', async () => {
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

    await screen.findByText('Text source')
    const meta = screen.getByText(/\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}/)
    expect(meta).not.toHaveTextContent('gpt-test')
    // The requested model is named once, with the other request context.
    const context = screen.getByRole('region', { name: 'Context' })
    expect(within(context).getByText('Model')).toBeVisible()
    expect(within(context).getByText('gpt-test')).toBeVisible()
    expect(screen.queryByText('Created at')).not.toBeInTheDocument()
  })

  test('keeps the context to the request and the mechanics in the technical details', async () => {
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

    await screen.findByText('Text source')
    const context = screen.getByRole('region', { name: 'Context' })
    expect(within(context).getByText('Model')).toBeVisible()
    expect(within(context).getByText('Audit stage')).toBeVisible()
    // What travelled over the wire and what happened to the block are mechanics,
    // not context, so they stay collapsed with the rest of them.
    expect(within(context).queryByText('Protocol')).not.toBeInTheDocument()
    expect(
      within(context).queryByText('Delivery status')
    ).not.toBeInTheDocument()
  })

  test.each([
    { coverage: true, expected: 'hidden' },
    { coverage: false, expected: 'shown' },
  ])(
    'shows the coverage caveat only for a partial scan ($expected)',
    async ({ coverage }) => {
      getPromptAuditMock.mockResolvedValue({
        success: true,
        data: { ...EVENT, coverage_complete: coverage },
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

      const result = await screen.findByRole('region', { name: 'Result' })
      // A complete scan says nothing beyond "the audit ran", so it is not a row.
      if (coverage) {
        expect(within(result).queryByText('Coverage')).not.toBeInTheDocument()
        expect(within(result).queryByText('Complete')).not.toBeInTheDocument()
      } else {
        expect(within(result).getByText('Coverage')).toBeVisible()
        expect(within(result).getByText('Incomplete')).toBeVisible()
      }
    }
  )

  test('omits empty result fields and a suggestion identical to the actual action', async () => {
    getPromptAuditMock.mockResolvedValue({
      success: true,
      data: {
        ...EVENT,
        action: 'unavailable',
        would_action: 'unavailable',
        safety: '',
        refusal: '',
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
    const result = await screen.findByRole('region', { name: 'Result' })
    expect(within(result).getByText('Actual action')).toBeVisible()
    for (const label of [
      'Suggested action',
      'Safety',
      'Refusal',
      'No categories',
    ]) {
      expect(within(result).queryByText(label)).not.toBeInTheDocument()
    }
  })

  test('copies the complete request id from the technical details', async () => {
    const user = userEvent.setup()
    const requestID = 'request-with-long-identifier-'.repeat(10)
    getPromptAuditMock.mockResolvedValue({
      success: true,
      data: { ...EVENT, request_id: requestID },
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
    await user.click(
      await screen.findByRole('button', { name: 'Technical details' })
    )
    expect(await screen.findByText(requestID)).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Copy · Request ID' }))
    expect(await navigator.clipboard.readText()).toBe(requestID)
    // The record id is the other identifier the operator copies out of here.
    await user.click(screen.getByRole('button', { name: 'Copy · Record ID' }))
    expect(await navigator.clipboard.readText()).toBe(String(EVENT.id))
  })

  test('reads the outcome before the evidence and the context', async () => {
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

    await screen.findByText('Text source')
    const result = screen.getByRole('region', { name: 'Result' })
    const inspected = screen.getByRole('region', { name: 'Inspected content' })
    const context = screen.getByRole('region', { name: 'Context' })
    expect(
      result.compareDocumentPosition(inspected) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(
      inspected.compareDocumentPosition(context) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(
      within(result).getByRole('heading', { name: 'Result' })
    ).toBeVisible()
  })

  test('keeps the client and technical sections collapsed until asked', async () => {
    const user = userEvent.setup()
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

    await screen.findByText('Text source')
    const client = screen.getByRole('button', { name: 'Client' })
    const technical = screen.getByRole('button', { name: 'Technical details' })
    expect(client).toHaveAttribute('aria-expanded', 'false')
    expect(technical).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('192.0.2.1')).not.toBeInTheDocument()
    await user.click(client)
    expect(await screen.findByText('192.0.2.1')).toBeVisible()
    expect(client).toHaveAttribute('aria-expanded', 'true')
  })

  test('offers a retry when the record fails to load', async () => {
    const user = userEvent.setup()
    getPromptAuditMock.mockRejectedValue(new Error('network down'))
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

    const retry = await screen.findByRole('button', { name: 'Retry' })
    expect(screen.getByText('network down')).toBeVisible()
    getPromptAuditMock.mockResolvedValue({ success: true, data: EVENT })
    await user.click(retry)
    expect(await screen.findByText('Text source')).toBeVisible()
  })
})
