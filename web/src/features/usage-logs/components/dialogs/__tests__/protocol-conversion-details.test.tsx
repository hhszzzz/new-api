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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test } from 'vitest'

import type { UsageLog } from '../../../data/schema'
import type { LogOtherData } from '../../../types'
import { DetailsDialog } from '../details-dialog'

const other: LogOtherData = {
  model_price: 0.25,
  request_path: '/v1/responses',
  diagnostics: { request_protocol: 'responses' },
  admin_info: {
    upstream_protocol: 'messages',
    protocol_converter: 'openai_responses_to_claude_messages',
    conversion_diagnostics: [
      {
        code: 'omitted_presentation_metadata',
        path: 'metadata',
        message: 'target protocol does not carry this display metadata',
        severity: 'warning',
        loss_class: 'presentation',
        from: 'openai-responses',
        to: 'claude',
      },
    ],
  },
}

const log: UsageLog = {
  id: 1,
  user_id: 1,
  created_at: 1,
  type: 5,
  content: '',
  username: 'user',
  token_name: 'token',
  model_name: 'gpt-test',
  quota: 0,
  prompt_tokens: 0,
  completion_tokens: 0,
  use_time: 0,
  is_stream: false,
  channel: 1,
  channel_name: 'channel',
  token_id: 1,
  group: 'default',
  ip: '',
  other: JSON.stringify(other),
  request_id: 'req-1',
  upstream_request_id: '',
}

test('failed requests keep conversion warnings inside the expanded field adjustments', async () => {
  const user = userEvent.setup()
  render(
    <DetailsDialog
      log={log}
      isAdminView
      canViewModelRoute={false}
      isRoot={false}
      open
      onOpenChange={() => undefined}
    />
  )

  const dialog = within(screen.getByRole('dialog'))
  expect(dialog.queryByText('Adjusted')).not.toBeInTheDocument()
  expect(dialog.queryByText('Warning')).not.toBeInTheDocument()

  const adjustments = dialog.getByRole('button', { name: /Field adjustments/ })
  expect(adjustments).toHaveAttribute('aria-expanded', 'false')
  await user.click(adjustments)
  await waitFor(() =>
    expect(
      within(screen.getByRole('dialog')).getByText('Warning')
    ).toBeVisible()
  )
})
