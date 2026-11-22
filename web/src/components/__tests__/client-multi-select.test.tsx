import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { ClientMultiSelect } from '../client-multi-select'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('@/components/multi-select', () => ({
  MultiSelect: (props: {
    options: Array<{ label: string; value: string }>
    selected: string[]
    onChange: (values: string[]) => void
  }) => (
    <div data-selected={props.selected.join(',')}>
      {props.options.map((option) => (
        <button
          key={option.value}
          type='button'
          onClick={() => props.onChange([...props.selected, option.value])}
        >
          {option.label}
        </button>
      ))}
    </div>
  ),
}))

describe('client multi-select', () => {
  test('offers built-in client families and canonicalizes Codex aliases', () => {
    const onChange = vi.fn()
    render(
      <ClientMultiSelect
        selected={['codex_vscode']}
        availableClients={['codex_vscode', 'my_custom_client']}
        onChange={onChange}
      />
    )

    expect(screen.getByText('Codex').parentElement).toHaveAttribute(
      'data-selected',
      'codex'
    )
    for (const client of [
      'Antigravity',
      'Cline',
      'OpenCode',
      'Kimi Code',
      'ZCode',
    ]) {
      expect(screen.getByRole('button', { name: client })).toBeInTheDocument()
    }
    expect(
      screen.queryByRole('button', { name: 'Gemini CLI' })
    ).not.toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Codex' })).toHaveLength(1)

    fireEvent.click(screen.getByRole('button', { name: 'my_custom_client' }))
    expect(onChange).toHaveBeenCalledWith(['codex', 'my_custom_client'])
  })
})
