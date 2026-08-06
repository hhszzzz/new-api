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

import { ChatSettingsVisualEditor } from '../chat-settings-visual-editor'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('../chat-dialog', () => ({
  ChatDialog: (props: {
    open: boolean
    onSave: (data: { name: string; url: string; groups: string[] }) => void
  }) =>
    props.open ? (
      <button
        type='button'
        onClick={() =>
          props.onSave({
            name: 'VIP Chat',
            url: 'https://vip.example.com',
            groups: ['vip'],
          })
        }
      >
        Save restricted preset
      </button>
    ) : null,
}))

describe('chat preset visual editor', () => {
  test('shows whether a preset is public or limited to groups', () => {
    render(
      <ChatSettingsVisualEditor
        value={JSON.stringify([
          { Public: 'https://public.example.com' },
          {
            VIP: {
              url: 'https://vip.example.com',
              groups: ['vip'],
            },
          },
        ])}
        onChange={vi.fn()}
        groupOptions={[]}
      />
    )

    expect(screen.getByText('Public')).toBeInTheDocument()
    expect(screen.getByText('All user groups')).toBeInTheDocument()
    expect(screen.getByText('VIP')).toBeInTheDocument()
    expect(screen.getByText('vip')).toBeInTheDocument()
  })

  test('writes selected groups into the saved chat configuration', () => {
    const onChange = vi.fn()
    render(
      <ChatSettingsVisualEditor
        value='[]'
        onChange={onChange}
        groupOptions={[{ value: 'vip', label: 'vip' }]}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: 'Add chat preset' }))
    fireEvent.click(
      screen.getByRole('button', { name: 'Save restricted preset' })
    )

    expect(onChange).toHaveBeenCalledOnce()
    expect(JSON.parse(onChange.mock.calls[0]?.[0] as string)).toEqual([
      {
        'VIP Chat': {
          url: 'https://vip.example.com',
          groups: ['vip'],
        },
      },
    ])
  })
})
