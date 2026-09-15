import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { parseProtocolPolicy } from '../policy'
import { ProtocolPolicyEditor } from '../protocol-policy-editor'
import { protocolCatalogFixture } from './fixtures'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

function renderEditor(
  props: { value?: string; disabled?: boolean } = {},
  cached = true
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  if (cached) client.setQueryData(['protocol-catalog'], protocolCatalogFixture)
  const onChange = vi.fn()
  render(
    <QueryClientProvider client={client}>
      <ProtocolPolicyEditor
        value={props.value ?? '{"version":1}'}
        onChange={onChange}
        disabled={props.disabled}
        inherit
      />
    </QueryClientProvider>
  )
  return onChange
}

beforeEach(() =>
  vi.mocked(api.get).mockResolvedValue({
    data: { success: true, data: protocolCatalogFixture },
  })
)

describe('protocol policy editor', () => {
  test('changes conversion while retaining ordered model rules and storage limits', async () => {
    const user = userEvent.setup()
    const rules = [{ model_pattern: '^private-', conversion: 'native_only' }]
    const onChange = renderEditor({
      value: JSON.stringify({ version: 1, rules, max_state_turns: 9 }),
    })
    await user.click(
      screen.getByRole('combobox', { name: 'Conversion policy' })
    )
    await user.click(
      screen.getByRole('option', { name: 'Lossless conversion' })
    )
    expect(JSON.parse(onChange.mock.lastCall?.[0])).toEqual({
      version: 1,
      rules,
      max_state_turns: 9,
      conversion: 'lossless',
    })
  })

  test('locks selection and JSON editing when the user cannot change sensitive configuration', () => {
    renderEditor({ disabled: true })
    for (const control of screen.getAllByRole('combobox')) {
      expect(control).toBeDisabled()
    }
    for (const control of screen.getAllByRole('checkbox')) {
      expect(control).toHaveAttribute('aria-disabled', 'true')
    }
    expect(
      screen.getByRole('textbox', { name: 'Protocol rules and limits (JSON)' })
    ).toBeDisabled()
  })

  test('recovers a failed catalog request through Retry and displays its protocols', async () => {
    vi.mocked(api.get).mockRejectedValueOnce(new Error('offline'))
    const user = userEvent.setup()
    renderEditor({}, false)
    expect(screen.getByText('Loading...')).toBeInTheDocument()
    await user.click(await screen.findByRole('button', { name: 'Retry' }))
    expect(
      await screen.findByRole('checkbox', { name: 'Anthropic Messages' })
    ).toBeInTheDocument()
    await waitFor(() =>
      expect(
        screen.queryByRole('button', { name: 'Retry' })
      ).not.toBeInTheDocument()
    )
  })

  test.each([
    '{"version":2}',
    '{"version":1,"conversion":"drop_tools"}',
    '{"version":1,"upstream_protocols":"chat"}',
    '{"version":1,"max_state_turns":-1}',
    '{"version":1,"rules":[{"deny":"false"}]}',
    '{"version":1,"max_state_bytes":1}',
  ])('rejects invalid policy %s before saving', (value) => {
    expect(parseProtocolPolicy(value)).toBeNull()
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
