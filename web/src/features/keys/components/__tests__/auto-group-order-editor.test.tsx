/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { fireEvent, render, within } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, test } from 'vitest'

const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { AutoGroupOrderEditor } = await import('../auto-group-order-editor')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
})

const options = ['default', 'vip', 'team', 'partner'].map((value) => ({
  value,
  label: value,
  ratio: 1,
}))

function Harness(props: { initialGroups?: string[] }) {
  const [groups, setGroups] = useState(
    props.initialGroups ?? ['default', 'vip']
  )
  return (
    <I18nextProvider i18n={i18n}>
      <AutoGroupOrderEditor
        value={groups}
        options={options}
        onChange={setGroups}
      />
      <output data-testid='order'>{groups.join(',')}</output>
    </I18nextProvider>
  )
}

function findButton(container: HTMLElement, label: string): HTMLButtonElement {
  return within(container).getByRole('button', { name: label })
}

function getCommandItem(label: string): HTMLElement {
  const item = [
    ...document.querySelectorAll<HTMLElement>('[data-slot="command-item"]'),
  ].find((candidate) => candidate.textContent?.includes(label))
  if (!item) throw new Error(`Expected command item containing "${label}"`)
  return item
}

describe('per-token Auto group order', () => {
  test('keeps the saved order and exposes accessible reorder controls', () => {
    const { container } = render(<Harness />)

    expect(within(container).getByTestId('order')).toHaveTextContent(
      'default,vip'
    )
    expect(
      within(container).getByRole('group', { name: 'Auto group order' })
    ).toBeInTheDocument()
    fireEvent.click(findButton(container, 'Move vip up'))
    expect(within(container).getByTestId('order')).toHaveTextContent(
      'vip,default'
    )
  })

  test('adds and removes authorized candidates', () => {
    const { container } = render(<Harness />)
    fireEvent.click(findButton(container, 'Remove vip'))
    expect(within(container).getByTestId('order')).toHaveTextContent('default')

    const addButton = within(container).getByRole('combobox')
    fireEvent.click(addButton)
    fireEvent.click(getCommandItem('team'))
    expect(within(container).getByTestId('order')).toHaveTextContent(
      'default,team'
    )
  })

  test('allows lists longer than the former global limit', () => {
    const { container } = render(
      <Harness initialGroups={['default', 'vip', 'team']} />
    )
    const addButton = within(container).getByRole('combobox')
    fireEvent.click(addButton)
    fireEvent.click(getCommandItem('partner'))

    expect(within(container).getByTestId('order')).toHaveTextContent(
      'default,vip,team,partner'
    )
    expect(container).toHaveTextContent('4 groups selected')
  })
})
