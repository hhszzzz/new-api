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
import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { Dialog } from '../dialog'

function TestDialog(props: { open: boolean; recordId: number }) {
  return (
    <Dialog
      open={props.open}
      onOpenChange={() => undefined}
      title='Log Details'
      scrollResetKey={props.recordId}
    >
      <button type='button'>Content action</button>
      <div data-slot='scroll-area-viewport'>Nested viewport</div>
    </Dialog>
  )
}

describe('Dialog scroll positioning', () => {
  test('focuses the header and resets retained scroll containers for every record and reopen', async () => {
    const view = render(<TestDialog open recordId={1} />)

    let dialog = screen.getByRole('dialog')
    let header = dialog.querySelector<HTMLElement>(
      '[data-slot="dialog-header"]'
    )
    let body = dialog.querySelector<HTMLElement>('[data-slot="dialog-body"]')
    let nested = dialog.querySelector<HTMLElement>(
      '[data-slot="scroll-area-viewport"]'
    )
    expect(header).not.toBeNull()
    expect(body).not.toBeNull()
    expect(nested).not.toBeNull()
    if (!header || !body || !nested) {
      throw new Error('Expected dialog scroll containers to be rendered')
    }

    await waitFor(() => expect(document.activeElement).toBe(header))

    body.scrollTop = 240
    nested.scrollTop = 120
    view.rerender(<TestDialog open recordId={2} />)

    expect(body).toHaveProperty('scrollTop', 0)
    expect(nested).toHaveProperty('scrollTop', 0)

    body.scrollTop = 320
    nested.scrollTop = 160
    view.rerender(<TestDialog open={false} recordId={2} />)
    view.rerender(<TestDialog open recordId={2} />)

    dialog = screen.getByRole('dialog')
    header = dialog.querySelector<HTMLElement>('[data-slot="dialog-header"]')
    body = dialog.querySelector<HTMLElement>('[data-slot="dialog-body"]')
    nested = dialog.querySelector<HTMLElement>(
      '[data-slot="scroll-area-viewport"]'
    )

    expect(body).toHaveProperty('scrollTop', 0)
    expect(nested).toHaveProperty('scrollTop', 0)
    await waitFor(() => expect(document.activeElement).toBe(header))
  })
})
