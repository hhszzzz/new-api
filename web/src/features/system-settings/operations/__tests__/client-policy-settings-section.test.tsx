/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

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
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { ClientPolicySettingsSection } from '../client-policy-settings-section'

const { updateClientPolicyOptionsMock, toastErrorMock, toastSuccessMock } =
  vi.hoisted(() => ({
    updateClientPolicyOptionsMock: vi.fn(),
    toastErrorMock: vi.fn(),
    toastSuccessMock: vi.fn(),
  }))

vi.mock('../../api', () => ({
  updateClientPolicyOptions: updateClientPolicyOptionsMock,
}))

vi.mock('sonner', () => ({
  toast: { error: toastErrorMock, success: toastSuccessMock },
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => `translated:${key}`,
  }),
}))

vi.mock('@/features/users/api', () => ({
  getGroups: vi.fn().mockResolvedValue({
    success: true,
    data: ['vip', 'default'],
  }),
}))

vi.mock('@/components/client-multi-select', () => ({
  ClientMultiSelect: (props: {
    id?: string
    selected: string[]
    disabled?: boolean
  }) => (
    <div
      data-testid={props.id}
      data-selected={props.selected.join(',')}
      data-disabled={String(Boolean(props.disabled))}
    />
  ),
}))

vi.mock('@/components/multi-select', () => ({
  MultiSelect: (props: {
    options: Array<{ label: string; value: string }>
    selected: string[]
    onChange: (values: string[]) => void
  }) => (
    <div>
      {props.options.map((option) => (
        <button
          key={option.value}
          type='button'
          aria-label={`pick:${option.value}`}
          onClick={() => props.onChange([...props.selected, option.value])}
        >
          {option.label}
        </button>
      ))}
      <button
        type='button'
        aria-label='create:desktop_agent'
        onClick={() => props.onChange([...props.selected, 'desktop_agent'])}
      >
        desktop_agent
      </button>
    </div>
  ),
}))

vi.mock('@/components/ui/combobox', () => ({
  Combobox: () => null,
}))

vi.mock('@/components/ui/select', () => ({
  Select: (props: { children: ReactNode; value?: string }) => (
    <div data-value={props.value}>{props.children}</div>
  ),
  SelectContent: (props: { children: ReactNode }) => (
    <div>{props.children}</div>
  ),
  SelectGroup: (props: { children: ReactNode }) => <div>{props.children}</div>,
  SelectItem: (props: { children: ReactNode }) => <div>{props.children}</div>,
  SelectTrigger: (props: { children: ReactNode }) => (
    <div>{props.children}</div>
  ),
  SelectValue: (props: { children?: ReactNode }) => (
    <span data-testid='select-value'>{props.children}</span>
  ),
}))

describe('client policy settings selected labels', () => {
  beforeEach(() => {
    updateClientPolicyOptionsMock.mockReset()
    updateClientPolicyOptionsMock.mockResolvedValue({ success: true })
    toastErrorMock.mockReset()
    toastSuccessMock.mockReset()
  })

  test('shows translated labels instead of raw stored enum values', () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <ClientPolicySettingsSection
          defaultValues={{
            'client_policy_setting.rules': JSON.stringify([
              {
                name: 'custom_client',
                matches: [
                  {
                    source: 'header',
                    header: 'x-app',
                    mode: 'prefix',
                    value: 'codex',
                  },
                ],
              },
            ]),
            'client_policy_setting.group_policies': '{}',
          }}
        />
      </QueryClientProvider>
    )

    const selectedLabels = screen
      .getAllByTestId('select-value')
      .map((element) => element.textContent)
      .filter(Boolean)

    expect(selectedLabels).toContain('translated:Safe header')
    expect(selectedLabels).toContain('translated:Prefix')
  })

  test('offers simplified client presets in the rule selector', async () => {
    const user = userEvent.setup()
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <ClientPolicySettingsSection
          defaultValues={{
            'client_policy_setting.rules': '[]',
            'client_policy_setting.group_policies': '{}',
          }}
        />
      </QueryClientProvider>
    )

    expect(
      screen.queryByText('translated:Ready-to-use client presets')
    ).not.toBeInTheDocument()
    expect(
      screen.queryByText('translated:Built-in clients')
    ).not.toBeInTheDocument()
    expect(
      screen.getByText('translated:Group client policies')
    ).toBeInTheDocument()
    const pythonOption = screen.getByRole('button', {
      name: 'pick:preset:stainless-python',
    })
    expect(pythonOption).toHaveTextContent('Python')
    expect(
      screen.getByRole('button', {
        name: 'pick:preset:stainless-javascript',
      })
    ).toHaveTextContent('JavaScript')
    expect(
      screen.getByRole('button', { name: 'pick:preset:stainless-java' })
    ).toHaveTextContent('Java')
    expect(
      screen.getByRole('button', { name: 'pick:preset:postman' })
    ).toHaveTextContent('Postman')
    expect(
      screen.getByRole('button', { name: 'pick:preset:httpie' })
    ).toHaveTextContent('HTTPie')
    expect(
      screen.getByRole('button', { name: 'pick:preset:insomnia' })
    ).toHaveTextContent('Insomnia')

    await user.click(pythonOption)

    expect(screen.getAllByDisplayValue('python')).toHaveLength(2)
    expect(screen.getAllByText('translated:Safe header')).toHaveLength(2)
    expect(screen.getAllByText('translated:Exact')).toHaveLength(2)
    expect(
      screen.queryByRole('button', { name: 'translated:Stainless Python SDK' })
    ).not.toBeInTheDocument()
  })

  test('creates an editable User-Agent fingerprint from a custom client name', async () => {
    const user = userEvent.setup()
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <ClientPolicySettingsSection
          defaultValues={{
            'client_policy_setting.rules': '[]',
            'client_policy_setting.group_policies': '{}',
          }}
        />
      </QueryClientProvider>
    )

    await user.click(
      screen.getByRole('button', { name: 'create:desktop_agent' })
    )

    expect(screen.getAllByDisplayValue('desktop_agent')).toHaveLength(2)
    expect(screen.getAllByText('translated:User-Agent')).toHaveLength(2)
    expect(screen.getAllByText('translated:Prefix')).toHaveLength(2)
  })

  test('preserves unsaved rules when system options refresh in the background', () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const initialValues = {
      'client_policy_setting.rules': JSON.stringify([
        {
          name: 'server_client',
          matches: [{ source: 'user_agent', mode: 'prefix', value: 'server/' }],
        },
      ]),
      'client_policy_setting.group_policies': '{}',
    }
    const { rerender } = render(
      <QueryClientProvider client={queryClient}>
        <ClientPolicySettingsSection defaultValues={initialValues} />
      </QueryClientProvider>
    )

    fireEvent.change(screen.getByDisplayValue('server_client'), {
      target: { value: 'local_draft' },
    })
    rerender(
      <QueryClientProvider client={queryClient}>
        <ClientPolicySettingsSection
          defaultValues={{
            ...initialValues,
            'client_policy_setting.rules': JSON.stringify([
              {
                name: 'refreshed_server_client',
                matches: [
                  {
                    source: 'user_agent',
                    mode: 'prefix',
                    value: 'refreshed/',
                  },
                ],
              },
            ]),
          }}
        />
      </QueryClientProvider>
    )

    expect(screen.getByDisplayValue('local_draft')).toBeVisible()
    expect(screen.queryByDisplayValue('refreshed_server_client')).toBeNull()
  })

  test('shows every current and previously configured group without add or remove controls', async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <ClientPolicySettingsSection
          defaultValues={{
            'client_policy_setting.rules': JSON.stringify([
              {
                name: 'desktop_agent',
                matches: [
                  {
                    source: 'user_agent',
                    mode: 'prefix',
                    value: 'desktop-agent/',
                  },
                ],
              },
            ]),
            'client_policy_setting.group_policies': JSON.stringify({
              vip: { mode: 'allow', clients: ['codex', 'desktop_agent'] },
              legacy: { mode: 'deny', clients: ['claude_code'] },
            }),
          }}
        />
      </QueryClientProvider>
    )

    const defaultSelector = await screen.findByTestId(
      'group-client-policy-default'
    )
    const vipSelector = screen.getByTestId('group-client-policy-vip')
    const legacySelector = screen.getByTestId('group-client-policy-legacy')

    expect(defaultSelector).toHaveAttribute('data-disabled', 'true')
    expect(vipSelector).toHaveAttribute('data-selected', 'codex,desktop_agent')
    expect(vipSelector).toHaveAttribute('data-disabled', 'false')
    expect(legacySelector).toHaveAttribute('data-selected', 'claude_code')

    const vipRow = vipSelector.parentElement
    if (!vipRow) throw new Error('VIP policy row was not rendered')
    expect(vipRow.querySelector('[data-value="allow"]')).not.toBeNull()
    expect(
      within(vipRow).queryByRole('button', { name: 'translated:Remove' })
    ).not.toBeInTheDocument()
    expect(screen.queryByText('translated:Add group')).not.toBeInTheDocument()
  })

  test.each([
    {
      name: 'empty match value',
      match: { source: 'user_agent', mode: 'prefix', value: '' },
    },
    {
      name: 'header match without a header',
      match: { source: 'header', mode: 'exact', value: 'codex' },
    },
  ])('rejects an incomplete rule with $name', async ({ match }) => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <ClientPolicySettingsSection
          defaultValues={{
            'client_policy_setting.rules': JSON.stringify([
              { name: 'incomplete', matches: [match] },
            ]),
            'client_policy_setting.group_policies': '{}',
          }}
        />
      </QueryClientProvider>
    )

    const form = container.querySelector('form')
    if (!form) throw new Error('settings form was not rendered')
    fireEvent.submit(form)

    await waitFor(() =>
      expect(toastErrorMock).toHaveBeenCalledWith(
        'translated:Complete every client rule match before saving.'
      )
    )
    expect(updateClientPolicyOptionsMock).not.toHaveBeenCalled()
  })

  test('submits rules and policies in one atomic mutation', async () => {
    updateClientPolicyOptionsMock.mockResolvedValueOnce({
      success: false,
      message: 'rejected',
    })
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <ClientPolicySettingsSection
          defaultValues={{
            'client_policy_setting.rules': JSON.stringify([
              {
                name: 'codex',
                matches: [
                  { source: 'user_agent', mode: 'prefix', value: 'codex/' },
                ],
              },
            ]),
            'client_policy_setting.group_policies': '{}',
          }}
        />
      </QueryClientProvider>
    )

    const form = container.querySelector('form')
    if (!form) throw new Error('settings form was not rendered')
    fireEvent.submit(form)

    await waitFor(() =>
      expect(updateClientPolicyOptionsMock).toHaveBeenCalledOnce()
    )
    expect(updateClientPolicyOptionsMock.mock.calls[0]?.[0]).toEqual({
      rules: [
        {
          name: 'codex',
          matches: [
            {
              source: 'user_agent',
              header: undefined,
              mode: 'prefix',
              value: 'codex/',
            },
          ],
        },
      ],
      group_policies: {},
    })
  })
})
