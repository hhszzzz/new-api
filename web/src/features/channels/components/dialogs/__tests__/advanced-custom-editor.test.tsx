import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { protocolCatalogFixture } from '@/features/protocols/__tests__/fixtures'

import { AdvancedCustomEditorDialog } from '../advanced-custom-editor-dialog'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

function renderEditor(route: Record<string, unknown>) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['protocol-catalog'], protocolCatalogFixture)
  const onSave = vi.fn()
  render(
    <QueryClientProvider client={client}>
      <AdvancedCustomEditorDialog
        open
        value={JSON.stringify({ advanced_routes: [route] })}
        onSave={onSave}
        onOpenChange={vi.fn()}
      />
    </QueryClientProvider>
  )
  return onSave
}

describe('Advanced Custom target editor', () => {
  test('saves an imported legacy route as a target protocol without losing custom fields', async () => {
    const user = userEvent.setup()
    const onSave = renderEditor({
      incoming_path: '/v1/messages',
      upstream_path: '/proxy',
      converter: 'claude_messages_to_openai_responses',
      models: ['private-model'],
      auth: { type: 'header', name: 'X-Secret', value: '{api_key}' },
    })
    expect(
      screen.getByRole('combobox', { name: 'Target protocol' })
    ).toHaveTextContent('OpenAI Responses')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(JSON.parse(onSave.mock.lastCall?.[0]).advanced_routes).toEqual([
      {
        incoming_path: '/v1/messages',
        upstream_path: '/proxy',
        target_protocol: 'responses',
        models: ['private-model'],
        auth: { type: 'header', name: 'X-Secret', value: '{api_key}' },
      },
    ])
  })

  test('changes a target using the catalog while preserving a custom path and authentication', async () => {
    const user = userEvent.setup()
    const onSave = renderEditor({
      incoming_path: '/v1/messages',
      upstream_path: '/custom',
      target_protocol: 'native',
      auth: { type: 'header', name: 'X-Provider', value: '{api_key}' },
    })
    await user.click(screen.getByRole('combobox', { name: 'Target protocol' }))
    await user.click(screen.getByRole('option', { name: 'OpenAI Responses' }))
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(JSON.parse(onSave.mock.lastCall?.[0]).advanced_routes[0]).toEqual({
      incoming_path: '/v1/messages',
      upstream_path: '/custom',
      target_protocol: 'responses',
      auth: { type: 'header', name: 'X-Provider', value: '{api_key}' },
    })
  })

  test('keeps compact on its native operation and disables conversion selection', () => {
    renderEditor({
      incoming_path: '/v1/responses/compact',
      upstream_path: '/v1/responses/compact',
      target_protocol: 'native',
    })
    expect(
      screen.getByRole('combobox', { name: 'Target protocol' })
    ).toBeDisabled()
    expect(
      screen.getByRole('combobox', { name: 'Target protocol' })
    ).toHaveTextContent('Native forwarding')
  })
})
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
