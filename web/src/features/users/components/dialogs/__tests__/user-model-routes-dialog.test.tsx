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
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import {
  createUserModelRoute,
  deleteUserModelRoute,
  getUserModelRouteCandidates,
  getUserModelRoutes,
  updateUserModelRoute,
} from '../../../api'
import type { UserModelRoute, UserModelRouteCandidates } from '../../../types'
import { UserModelRoutesDialog } from '../user-model-routes-dialog'

const { debounceControl } = vi.hoisted(() => ({
  debounceControl: { hold: false },
}))

vi.mock('../../../api', () => ({
  createUserModelRoute: vi.fn(),
  deleteUserModelRoute: vi.fn(),
  getUserModelRouteCandidates: vi.fn(),
  getUserModelRoutes: vi.fn(),
  updateUserModelRoute: vi.fn(),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) =>
      Object.entries(values ?? {}).reduce(
        (copy, [name, value]) => copy.replace(`{{${name}}}`, String(value)),
        key
      ),
  }),
}))

vi.mock('@/hooks', async () => {
  const { useRef } = await import('react')
  return {
    useDebounce: <T,>(value: T) => {
      const settledValue = useRef(value)
      if (!debounceControl.hold) settledValue.current = value
      return settledValue.current
    },
  }
})

vi.mock('@/components/dialog', () => ({
  Dialog: (props: {
    open: boolean
    title: string
    children: ReactNode
    footer?: ReactNode
  }) =>
    props.open ? (
      <div role='dialog' aria-label={props.title}>
        {props.children}
        {props.footer}
      </div>
    ) : null,
}))

vi.mock('@/components/confirm-dialog', () => ({
  ConfirmDialog: () => null,
}))

vi.mock('@/components/error-state', () => ({
  ErrorState: (props: { title?: string; onRetry?: () => void }) => (
    <div role='alert'>
      <span>{props.title}</span>
      {props.onRetry ? (
        <button type='button' onClick={props.onRetry}>
          Retry
        </button>
      ) : null}
    </div>
  ),
}))

vi.mock('@/components/empty-state', () => ({
  EmptyState: (props: { title?: string }) => <div>{props.title}</div>,
}))

vi.mock('@/components/status-badge', () => ({
  StatusBadge: (props: { label: string }) => <span>{props.label}</span>,
}))

vi.mock('@/components/ui/combobox-input', () => ({
  ComboboxInput: (props: {
    id?: string
    value: string
    onValueChange: (value: string) => void
    placeholder?: string
    disabled?: boolean
  }) => (
    <input
      id={props.id}
      value={props.value}
      onChange={(event) => props.onValueChange(event.currentTarget.value)}
      placeholder={props.placeholder}
      disabled={props.disabled}
    />
  ),
}))

vi.mock('@/components/multi-select', () => ({
  MultiSelect: (props: {
    id?: string
    options: Array<{ value: string; label: string }>
    selected: string[]
    onChange: (values: string[]) => void
    emptyText?: string
    disabled?: boolean
  }) => (
    <div>
      <select
        id={props.id}
        multiple
        value={props.selected}
        disabled={props.disabled}
        onChange={(event) =>
          props.onChange(
            Array.from(
              event.currentTarget.selectedOptions,
              (option) => option.value
            )
          )
        }
      >
        {props.options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
      {props.options.length === 0 ? <span>{props.emptyText}</span> : null}
    </div>
  ),
}))

vi.mock('@/components/ui/select', () => ({
  Select: (props: {
    items?: Array<{ value: string; label: string; disabled?: boolean }>
    value: string | null
    onValueChange: (value: string | null) => void
    disabled?: boolean
  }) => (
    <select
      aria-label='Execution group'
      value={props.value ?? ''}
      disabled={props.disabled}
      onChange={(event) => props.onValueChange(event.currentTarget.value)}
    >
      {(props.items ?? []).map((item) => (
        <option key={item.value} value={item.value} disabled={item.disabled}>
          {item.label}
        </option>
      ))}
    </select>
  ),
  SelectContent: () => null,
  SelectGroup: () => null,
  SelectItem: () => null,
  SelectTrigger: () => null,
  SelectValue: () => null,
}))

vi.mock('@/components/ui/switch', () => ({
  Switch: (props: {
    id?: string
    checked: boolean
    onCheckedChange: (checked: boolean) => void
    'aria-label'?: string
  }) => (
    <input
      id={props.id}
      type='checkbox'
      checked={props.checked}
      aria-label={props['aria-label']}
      onChange={(event) => props.onCheckedChange(event.currentTarget.checked)}
    />
  ),
}))

const mockedCreateRoute = vi.mocked(createUserModelRoute)
const mockedDeleteRoute = vi.mocked(deleteUserModelRoute)
const mockedGetCandidates = vi.mocked(getUserModelRouteCandidates)
const mockedGetRoutes = vi.mocked(getUserModelRoutes)
const mockedUpdateRoute = vi.mocked(updateUserModelRoute)

const BASE_CANDIDATES: UserModelRouteCandidates = {
  source_models: ['gpt-5.4'],
  target_models: ['target-a', 'target-b'],
  applicable_groups: ['default', 'vip'],
  execution_groups: ['default'],
  execution_group_channel_counts: {},
  recommended_execution_group: '',
  channels: [],
}

function candidateResponse(channels: UserModelRouteCandidates['channels']) {
  return {
    success: true,
    data: { ...BASE_CANDIDATES, channels },
  }
}

function renderDialog() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
    },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <UserModelRoutesDialog
        open
        onOpenChange={() => undefined}
        userId={1}
        username='route-user'
      />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  debounceControl.hold = false
  mockedCreateRoute.mockReset()
  mockedDeleteRoute.mockReset()
  mockedGetCandidates.mockReset()
  mockedGetRoutes.mockReset()
  mockedUpdateRoute.mockReset()

  mockedGetRoutes.mockResolvedValue({ success: true, data: [] })
  mockedGetCandidates.mockImplementation(async (_userId, params = {}) => {
    if (!params.target_model) return candidateResponse([])
    if (params.target_model === 'target-a') {
      return candidateResponse([
        { id: 11, name: 'Channel A', type: 1, priority: 10, weight: 100 },
      ])
    }
    if (params.target_model === 'target-b') {
      return candidateResponse([
        { id: 22, name: 'Channel B', type: 1, priority: 5, weight: 50 },
      ])
    }
    return candidateResponse([])
  })
})

describe('user model routes dialog', () => {
  test('shows loading and empty states while keeping an incomplete route disabled', async () => {
    let resolveCandidates:
      | ((value: ReturnType<typeof candidateResponse>) => void)
      | undefined
    mockedGetCandidates.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveCandidates = resolve
        })
    )

    renderDialog()

    expect(screen.getByRole('button', { name: 'Add route' })).toBeDisabled()
    expect(screen.getAllByText('Loading...').length).toBeGreaterThan(0)

    resolveCandidates?.(candidateResponse([]))
    expect(await screen.findByText('No model routes')).toBeVisible()
    expect(await screen.findByLabelText('Source model')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Add route' })).toBeDisabled()
  })

  test('renders route and editor request failures instead of empty data', async () => {
    mockedGetRoutes.mockRejectedValue(new Error('route request failed'))
    mockedGetCandidates.mockResolvedValue({
      success: false,
      message: 'candidate request failed',
    })

    renderDialog()

    await waitFor(() => {
      expect(screen.getAllByRole('alert')).toHaveLength(2)
    })
    expect(screen.getAllByText('Failed to load')).toHaveLength(2)
    expect(screen.queryByText('No model routes')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add route' })).toBeDisabled()
  })

  test('offers every authorized request group returned by the route API', async () => {
    renderDialog()
    const user = userEvent.setup()

    await user.click(await screen.findByLabelText('All user groups'))

    const groupSelect = await screen.findByLabelText('Applicable groups')
    expect(groupSelect).toBeVisible()
    expect(
      within(groupSelect).getByRole('option', { name: 'default' })
    ).toBeVisible()
    expect(
      within(groupSelect).getByRole('option', { name: 'vip' })
    ).toBeVisible()
  })

  test('refreshes eligible channels when the target model changes', async () => {
    renderDialog()
    const user = userEvent.setup()

    const sourceInput = await screen.findByLabelText('Source model')
    const targetInput = screen.getByLabelText('Target model')
    fireEvent.change(sourceInput, { target: { value: 'gpt-5.4' } })
    fireEvent.change(targetInput, { target: { value: 'target-a' } })

    const channelSelect = await screen.findByLabelText('Channel pool')
    await waitFor(() => {
      expect(screen.getByRole('option', { name: /Channel A/ })).toBeVisible()
    })
    await user.selectOptions(channelSelect, '11')
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Add route' })).toBeEnabled()
    })

    fireEvent.change(targetInput, { target: { value: 'target-b' } })
    await waitFor(() => {
      expect(screen.getByRole('option', { name: /Channel B/ })).toBeVisible()
    })
    expect(screen.queryByRole('option', { name: /Channel A/ })).toBeNull()
    expect(screen.getByRole('button', { name: 'Add route' })).toBeDisabled()

    await user.selectOptions(channelSelect, '22')
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Add route' })).toBeEnabled()
    })
    expect(mockedGetCandidates).toHaveBeenCalledWith(1, {
      target_model: 'target-a',
      execution_group: 'default',
    })
    expect(mockedGetCandidates).toHaveBeenCalledWith(1, {
      target_model: 'target-b',
      execution_group: 'default',
    })
  })

  test('does not expose stale channels while a target change is debouncing', async () => {
    renderDialog()
    const user = userEvent.setup()

    fireEvent.change(await screen.findByLabelText('Source model'), {
      target: { value: 'gpt-5.4' },
    })
    const targetInput = screen.getByLabelText('Target model')
    fireEvent.change(targetInput, { target: { value: 'target-a' } })
    const channelSelect = await screen.findByLabelText('Channel pool')
    await waitFor(() => {
      expect(screen.getByRole('option', { name: /Channel A/ })).toBeVisible()
    })
    await user.selectOptions(channelSelect, '11')
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Add route' })).toBeEnabled()
    })

    debounceControl.hold = true
    fireEvent.change(targetInput, { target: { value: 'target-b' } })

    expect(channelSelect).toBeDisabled()
    expect(screen.queryByRole('option', { name: /Channel A/ })).toBeNull()
    expect(screen.getByRole('button', { name: 'Add route' })).toBeDisabled()
  })

  test('shows channel request failures and prevents saving', async () => {
    mockedGetCandidates.mockImplementation(async (_userId, params = {}) => {
      if (!params.target_model) return candidateResponse([])
      return { success: false, message: 'channel request failed' }
    })
    renderDialog()

    fireEvent.change(await screen.findByLabelText('Source model'), {
      target: { value: 'gpt-5.4' },
    })
    fireEvent.change(screen.getByLabelText('Target model'), {
      target: { value: 'target-a' },
    })

    expect(await screen.findByRole('alert')).toHaveTextContent('Failed to load')
    expect(screen.getByRole('button', { name: 'Add route' })).toBeDisabled()
  })

  test('keeps a retired channel visible while preventing an unsafe edit', async () => {
    const route: UserModelRoute = {
      id: 9,
      user_id: 1,
      source_model: 'gpt-5.4',
      target_model: 'retired-target',
      pool_name: 'Legacy pool',
      execution_group: 'default',
      all_groups: true,
      groups: [],
      channel_ids: [99],
      enabled: true,
    }
    mockedGetRoutes.mockResolvedValue({ success: true, data: [route] })
    renderDialog()
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'Edit' }))

    const saveButton = await screen.findByRole('button', { name: 'Save route' })
    await waitFor(() => expect(saveButton).toBeDisabled())
    expect(await screen.findByRole('option', { name: '#99' })).toBeVisible()
    expect(
      screen.getByText(/This route contains unavailable channels: 99/)
    ).toBeVisible()
  })

  test('chooses a group with enabled channels for a newly selected target', async () => {
    mockedGetCandidates.mockImplementation(async (_userId, params = {}) => {
      if (!params.target_model) {
        return {
          success: true,
          data: {
            ...BASE_CANDIDATES,
            execution_groups: ['empty', 'ready'],
          },
        }
      }
      const base = {
        ...BASE_CANDIDATES,
        execution_groups: ['empty', 'ready'],
        execution_group_channel_counts: { empty: 0, ready: 1 },
        recommended_execution_group: 'ready',
      }
      if (params.execution_group !== 'ready') {
        return { success: true, data: { ...base, channels: [] } }
      }
      return {
        success: true,
        data: {
          ...base,
          channels: [
            {
              id: 31,
              name: 'Ready Channel',
              type: 1,
              priority: 1,
              weight: 100,
              aggregate_id: 7,
              aggregate_name: 'Shared API',
              protocol_compatibility: {
                chat: 'native' as const,
                messages: 'convertible' as const,
              },
            },
          ],
        },
      }
    })

    renderDialog()
    fireEvent.change(await screen.findByLabelText('Target model'), {
      target: { value: 'target-a' },
    })

    await waitFor(() => {
      expect(screen.getByLabelText('Execution group')).toHaveValue('ready')
    })
    expect(
      await screen.findByRole('option', {
        name: /empty \(0 channels · No enabled channels for this model in this group\)/,
      })
    ).toBeDisabled()
    expect(
      await screen.findByRole('option', { name: /Shared API \/ Ready Channel/ })
    ).toBeVisible()
    expect(
      screen.getByRole('option', { name: /Aggregate channel/ })
    ).toBeVisible()
  })

  test('saves a named aggregate channel pool without an input error', async () => {
    mockedGetCandidates.mockImplementation(async (_userId, params = {}) => {
      if (!params.target_model) return candidateResponse([])
      return candidateResponse([
        {
          id: 31,
          name: 'Ready Channel',
          type: 1,
          priority: 1,
          weight: 100,
          aggregate_id: 7,
          aggregate_name: 'Shared API',
        },
      ])
    })
    mockedCreateRoute.mockResolvedValue({
      success: true,
      data: {
        id: 10,
        user_id: 1,
        source_model: 'gpt-5.4',
        target_model: 'target-a',
        pool_name: 'Primary Pool',
        execution_group: 'default',
        all_groups: true,
        groups: [],
        channel_ids: [31],
        enabled: true,
      },
    })
    renderDialog()
    const user = userEvent.setup()

    await user.type(await screen.findByLabelText('Source model'), 'gpt-5.4')
    await user.type(screen.getByLabelText('Target model'), 'target-a')
    await user.type(screen.getByLabelText('Channel pool name'), 'Primary Pool')
    const channelSelect = await screen.findByLabelText('Channel pool')
    await waitFor(() => {
      expect(
        screen.getByRole('option', { name: /Aggregate channel/ })
      ).toBeVisible()
    })
    await user.selectOptions(channelSelect, 'aggregate:7')
    await user.click(screen.getByRole('button', { name: 'Add route' }))

    await waitFor(() => {
      expect(mockedCreateRoute).toHaveBeenCalledWith(1, {
        source_model: 'gpt-5.4',
        target_model: 'target-a',
        pool_name: 'Primary Pool',
        inject_prompt: '',
        execution_group: 'default',
        all_groups: true,
        groups: [],
        channel_ids: [31],
        enabled: true,
      })
    })
  })

  test('limits injected prompts by Unicode characters', async () => {
    renderDialog()

    const prompt = '🚀'.repeat(8001)
    fireEvent.change(await screen.findByLabelText('Injected system prompt'), {
      target: { value: prompt },
    })

    expect(screen.getByLabelText('Injected system prompt')).toHaveValue(
      '🚀'.repeat(8000)
    )
    expect(screen.getByText('8000/8000')).toBeVisible()
  })
})
