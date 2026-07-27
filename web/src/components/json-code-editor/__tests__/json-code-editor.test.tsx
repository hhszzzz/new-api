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
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { JsonCodeEditor } from '../../json-code-editor'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

describe('JsonCodeEditor component', () => {
  test('forwards form attributes and lifecycle callbacks to the textarea', () => {
    const onBlur = vi.fn()
    const textareaRef = vi.fn()
    const rendered = render(
      <JsonCodeEditor
        value='{"model":"gpt"}'
        onChange={vi.fn()}
        id='json-input'
        name='model_config'
        placeholder='{"model":"gpt"}'
        ariaLabel='Model configuration'
        disabled
        aria-describedby='model-help'
        aria-invalid
        data-form-root='settings-form'
        onBlur={onBlur}
        textareaRef={textareaRef}
      />
    )
    const textarea = screen.getByRole('textbox', {
      name: 'Model configuration',
    })

    expect(textarea).toHaveAttribute('id', 'json-input')
    expect(textarea).toHaveAttribute('name', 'model_config')
    expect(textarea).toHaveAttribute('placeholder', '{"model":"gpt"}')
    expect(textarea).toBeDisabled()
    expect(textarea).toHaveAttribute('aria-describedby', 'model-help')
    expect(textarea).toHaveAttribute('aria-invalid', 'true')
    expect(textarea).toHaveAttribute('data-form-root', 'settings-form')

    fireEvent.blur(textarea)
    expect(onBlur).toHaveBeenCalledOnce()
    expect(textareaRef).toHaveBeenCalledWith(textarea)

    rendered.unmount()
    expect(textareaRef).toHaveBeenLastCalledWith(null)
  })

  test('emits user edits and synchronizes a controlled value', () => {
    const onChange = vi.fn()
    const rendered = render(
      <JsonCodeEditor
        value='{"count":1}'
        onChange={onChange}
        ariaLabel='JSON value'
      />
    )
    const textarea = screen.getByRole('textbox', { name: 'JSON value' })

    fireEvent.input(textarea, { target: { value: '{"count":2}' } })
    expect(onChange).toHaveBeenCalledWith('{"count":2}')

    rendered.rerender(
      <JsonCodeEditor
        value='{"count":3}'
        onChange={onChange}
        ariaLabel='JSON value'
      />
    )
    expect(textarea).toHaveValue('{"count":3}')
  })

  test('formats valid JSON through the public toolbar action', () => {
    const onChange = vi.fn()
    render(<JsonCodeEditor value='{"model":{"ratio":2}}' onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Format JSON' }))
    expect(onChange).toHaveBeenCalledWith(
      '{\n  "model": {\n    "ratio": 2\n  }\n}'
    )
  })
})
