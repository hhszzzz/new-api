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
import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, test, vi } from 'vitest'

import { useLatestFormLoad } from '../use-latest-form-load'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function LoadHarness(props: {
  load: (id: number) => Promise<string>
  targetId: number | null
}) {
  const [value, setValue] = useState('')
  const { isLoading, reload } = useLatestFormLoad({
    enabled: props.targetId !== null,
    load: props.load,
    onLoad: setValue,
    target: props.targetId,
  })
  return (
    <div>
      <label htmlFor='value'>Value</label>
      <input
        id='value'
        disabled={isLoading}
        value={value}
        onChange={(event) => setValue(event.target.value)}
      />
      <button type='button' onClick={() => void reload()}>
        Reload
      </button>
    </div>
  )
}

describe('latest form load lifecycle', () => {
  test('disables editing until the active target has loaded', async () => {
    const pending = deferred<string>()
    const view = render(
      <LoadHarness load={() => pending.promise} targetId={1} />
    )
    expect(screen.getByLabelText('Value')).toBeDisabled()

    await act(async () => {
      pending.resolve('loaded')
      await pending.promise
    })

    expect(screen.getByLabelText('Value')).toBeEnabled()
    expect(screen.getByLabelText('Value')).toHaveValue('loaded')
    view.unmount()
  })

  test('ignores an older target response after switching records', async () => {
    const first = deferred<string>()
    const second = deferred<string>()
    const load = vi.fn((id: number) =>
      id === 1 ? first.promise : second.promise
    )
    const view = render(<LoadHarness load={load} targetId={1} />)
    view.rerender(<LoadHarness load={load} targetId={2} />)

    await act(async () => {
      second.resolve('second record')
      await second.promise
    })
    expect(screen.getByLabelText('Value')).toHaveValue('second record')

    await act(async () => {
      first.resolve('stale first record')
      await first.promise
    })
    expect(screen.getByLabelText('Value')).toHaveValue('second record')
  })

  test('ignores an obsolete refresh after the target changes', async () => {
    const user = userEvent.setup()
    const initial = deferred<string>()
    const refresh = deferred<string>()
    const second = deferred<string>()
    const load = vi
      .fn<(id: number) => Promise<string>>()
      .mockImplementationOnce(() => initial.promise)
      .mockImplementationOnce(() => refresh.promise)
      .mockImplementationOnce(() => second.promise)
    const view = render(<LoadHarness load={load} targetId={1} />)
    await act(async () => {
      initial.resolve('first record')
      await initial.promise
    })
    await user.click(screen.getByRole('button', { name: 'Reload' }))
    expect(screen.getByLabelText('Value')).toBeDisabled()

    view.rerender(<LoadHarness load={load} targetId={2} />)
    await act(async () => {
      second.resolve('second record')
      await second.promise
    })
    await act(async () => {
      refresh.resolve('stale refresh')
      await refresh.promise
    })

    expect(screen.getByLabelText('Value')).toHaveValue('second record')
    expect(screen.getByLabelText('Value')).toBeEnabled()
  })
})
