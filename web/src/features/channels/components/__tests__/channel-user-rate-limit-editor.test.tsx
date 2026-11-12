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
import { useState, type ReactElement } from 'react'
import { beforeEach, expect, test, vi } from 'vitest'

import { searchUsers } from '@/features/users/api'
import type { User } from '@/features/users/types'

import type { ChannelUserRateLimit } from '../../types'
import { ChannelUserRateLimitEditor } from '../channel-user-rate-limit-editor'

vi.mock('@/features/users/api', () => ({ searchUsers: vi.fn() }))

function userFixture(id: number, username: string, displayName: string): User {
  return {
    id,
    username,
    display_name: displayName,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    group: 'default',
    status: 1,
    role: 1,
  }
}

function renderEditor(element: ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>{element}</QueryClientProvider>
  )
}

function StatefulEditor(props: {
  initial: ChannelUserRateLimit[]
  onChange: (value: ChannelUserRateLimit[]) => void
}) {
  const [value, setValue] = useState(props.initial)
  return (
    <ChannelUserRateLimitEditor
      value={value}
      onChange={(next) => {
        setValue(next)
        props.onChange(next)
      }}
    />
  )
}

beforeEach(() => {
  vi.mocked(searchUsers).mockResolvedValue({
    success: true,
    data: {
      items: [userFixture(7, 'alice', 'Alice'), userFixture(8, 'bob', 'Bob')],
      total: 2,
      page: 1,
      page_size: 20,
    },
  })
})

test('adds a searched user using the display name shown in the results', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  renderEditor(<ChannelUserRateLimitEditor value={[]} onChange={onChange} />)

  expect(screen.getByText('No per-user overrides.')).toBeVisible()

  await user.click(screen.getByRole('button', { name: 'Add user' }))
  await user.type(
    screen.getByPlaceholderText('Search by username, email, or ID'),
    'ali'
  )
  await waitFor(() =>
    expect(searchUsers).toHaveBeenCalledWith(
      expect.objectContaining({ keyword: 'ali' })
    )
  )
  await user.click(await screen.findByRole('option', { name: /Alice/ }))

  expect(onChange).toHaveBeenCalledWith([{ user_id: 7, username: 'Alice' }])
})

test('does not offer soft-deleted users from search results', async () => {
  const user = userEvent.setup()
  vi.mocked(searchUsers).mockResolvedValueOnce({
    success: true,
    data: {
      items: [
        userFixture(7, 'alice', 'Alice'),
        {
          ...userFixture(8, 'deleted', 'Deleted user'),
          DeletedAt: { Time: '2026-11-10T00:00:00Z', Valid: true },
        },
      ],
      total: 2,
      page: 1,
      page_size: 20,
    },
  })
  renderEditor(<ChannelUserRateLimitEditor value={[]} onChange={vi.fn()} />)

  await user.click(screen.getByRole('button', { name: 'Add user' }))
  await user.type(
    screen.getByPlaceholderText('Search by username, email, or ID'),
    'user'
  )

  await screen.findByRole('option', { name: /Alice/ })
  expect(
    screen.queryByRole('option', { name: /Deleted user/ })
  ).not.toBeInTheDocument()
})

test('does not offer a user who already has an override', async () => {
  const user = userEvent.setup()
  renderEditor(
    <ChannelUserRateLimitEditor
      value={[{ user_id: 7, username: 'Alice' }]}
      onChange={vi.fn()}
    />
  )

  await user.click(screen.getByRole('button', { name: 'Add user' }))
  await user.type(
    screen.getByPlaceholderText('Search by username, email, or ID'),
    'user'
  )

  await screen.findByRole('option', { name: /Bob/ })
  expect(
    screen.queryByRole('option', { name: /Alice/ })
  ).not.toBeInTheDocument()
})

test('sets an RPM for a user who has no limit yet', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  renderEditor(
    <StatefulEditor
      initial={[{ user_id: 7, username: 'Alice' }]}
      onChange={onChange}
    />
  )

  await user.type(screen.getByLabelText('Requests per minute'), '45')
  expect(onChange).toHaveBeenLastCalledWith([
    { user_id: 7, username: 'Alice', rpm_limit: 45 },
  ])
})

test('edits concurrency while keeping the configured RPM', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  renderEditor(
    <StatefulEditor
      initial={[{ user_id: 7, username: 'Alice', rpm_limit: 30 }]}
      onChange={onChange}
    />
  )

  await user.type(screen.getByLabelText('Concurrent requests'), '3')
  expect(onChange).toHaveBeenLastCalledWith([
    { user_id: 7, username: 'Alice', rpm_limit: 30, concurrency_limit: 3 },
  ])
})

test('sets a numeric priority for an override', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  renderEditor(
    <StatefulEditor
      initial={[{ user_id: 7, username: 'Alice' }]}
      onChange={onChange}
    />
  )

  const priority = screen.getByLabelText('Priority')
  await user.type(priority, '100')
  expect(onChange).toHaveBeenLastCalledWith([
    { user_id: 7, username: 'Alice', priority: 100 },
  ])

  await user.clear(priority)
  expect(onChange).toHaveBeenLastCalledWith([
    { user_id: 7, username: 'Alice', priority: undefined },
  ])
})

test('removes a configured override', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  renderEditor(
    <ChannelUserRateLimitEditor
      value={[
        { user_id: 7, username: 'Alice', rpm_limit: 30 },
        { user_id: 8, username: 'Bob', concurrency_limit: 2 },
      ]}
      onChange={onChange}
    />
  )

  await user.click(screen.getAllByRole('button', { name: 'Remove' })[0])
  expect(onChange).toHaveBeenLastCalledWith([
    { user_id: 8, username: 'Bob', concurrency_limit: 2 },
  ])
})
