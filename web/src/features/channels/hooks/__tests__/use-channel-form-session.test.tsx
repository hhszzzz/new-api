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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useCallback } from 'react'
import { useForm } from 'react-hook-form'
import { describe, expect, test, vi } from 'vitest'

import { useChannelFormSession } from '../use-channel-form-session'

type FormValues = { name: string }
type RecordValue = { id: number; name: string }

const DEFAULT_VALUES: FormValues = { name: '' }

function FormHarness(props: {
  open: boolean
  record: RecordValue | null
  onReset?: () => void
}) {
  const form = useForm<FormValues>({ defaultValues: DEFAULT_VALUES })
  const transform = useCallback(
    (record: RecordValue): FormValues => ({ name: record.name }),
    []
  )
  const { onReset: handleReset } = props
  const onReset = useCallback(() => handleReset?.(), [handleReset])
  const { resetSession } = useChannelFormSession({
    defaultValues: DEFAULT_VALUES,
    form,
    isDirty: form.formState.isDirty,
    isEditing: props.record !== null,
    open: props.open,
    onReset,
    record: props.record,
    recordId: props.record?.id ?? null,
    transform,
  })

  if (!props.open) return null
  return (
    <form>
      <label htmlFor='name'>Name</label>
      <input id='name' {...form.register('name')} />
      <button type='button' onClick={resetSession}>
        Complete save
      </button>
    </form>
  )
}

describe('channel form session lifecycle', () => {
  test('resets create values when a successful save closes the session', async () => {
    const user = userEvent.setup()
    const onReset = vi.fn()
    const view = render(<FormHarness open record={null} onReset={onReset} />)
    const input = screen.getByLabelText('Name')
    await user.type(input, 'first channel')
    expect(input).toHaveValue('first channel')

    await user.click(screen.getByRole('button', { name: 'Complete save' }))
    view.rerender(<FormHarness open={false} record={null} onReset={onReset} />)
    view.rerender(<FormHarness open record={null} onReset={onReset} />)

    expect(screen.getByLabelText('Name')).toHaveValue('')
    expect(onReset).toHaveBeenCalled()
  })

  test('resets create values when the controlled drawer closes externally', async () => {
    const user = userEvent.setup()
    const view = render(<FormHarness open record={null} />)
    await user.type(screen.getByLabelText('Name'), 'unsaved channel')

    view.rerender(<FormHarness open={false} record={null} />)
    view.rerender(<FormHarness open record={null} />)

    expect(screen.getByLabelText('Name')).toHaveValue('')
  })

  test('does not replace dirty values when the same channel refetches', async () => {
    const user = userEvent.setup()
    const view = render(
      <FormHarness open record={{ id: 1, name: 'server name' }} />
    )
    const input = await screen.findByLabelText('Name')
    expect(input).toHaveValue('server name')
    await user.clear(input)
    await user.type(input, 'unsaved name')

    view.rerender(
      <FormHarness open record={{ id: 1, name: 'refetched server name' }} />
    )

    expect(screen.getByLabelText('Name')).toHaveValue('unsaved name')
  })

  test('hydrates a different channel even when the previous form was dirty', async () => {
    const user = userEvent.setup()
    const view = render(<FormHarness open record={{ id: 1, name: 'first' }} />)
    const input = await screen.findByLabelText('Name')
    await user.clear(input)
    await user.type(input, 'unsaved first')

    view.rerender(<FormHarness open record={{ id: 2, name: 'second' }} />)

    expect(screen.getByLabelText('Name')).toHaveValue('second')
  })
})
