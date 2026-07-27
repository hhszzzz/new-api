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
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { getModel } from '../../../api'
import { modelsQueryKeys, vendorsQueryKeys } from '../../../lib'
import type { Model } from '../../../types'
import { ModelMutateDrawer } from '../model-mutate-drawer'

vi.mock('../../../api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../../api')>()
  return { ...actual, getModel: vi.fn() }
})

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('@/components/ui/sheet', () => ({
  Sheet: (props: { open: boolean; children: ReactNode }) =>
    props.open ? <div>{props.children}</div> : null,
  SheetClose: (props: { children: ReactNode }) => (
    <button type='button'>{props.children}</button>
  ),
  SheetContent: (props: { children: ReactNode }) => <div>{props.children}</div>,
  SheetDescription: (props: { children: ReactNode }) => <p>{props.children}</p>,
  SheetFooter: (props: { children: ReactNode }) => <div>{props.children}</div>,
  SheetHeader: (props: { children: ReactNode }) => <div>{props.children}</div>,
  SheetTitle: (props: { children: ReactNode }) => <h2>{props.children}</h2>,
}))

const MODEL: Model = {
  id: 7,
  model_name: 'gpt-test',
  description: 'Server description',
  status: 1,
  sync_official: 1,
  created_time: 1,
  updated_time: 1,
  name_rule: 0,
}

const mockedGetModel = vi.mocked(getModel)

beforeEach(() => {
  mockedGetModel.mockReset()
  mockedGetModel.mockResolvedValue({ success: true, data: MODEL })
  vi.stubGlobal(
    'matchMedia',
    vi.fn((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }))
  )
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderDrawer() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
    },
  })
  queryClient.setQueryData(vendorsQueryKeys.list(), {
    success: true,
    data: { items: [], total: 0, page: 1, page_size: 1000 },
  })
  queryClient.setQueryData(['system-options'], {
    success: true,
    message: '',
    data: [],
  })

  const renderTree = () => (
    <QueryClientProvider client={queryClient}>
      <ModelMutateDrawer
        open
        onOpenChange={() => undefined}
        currentRow={MODEL}
      />
    </QueryClientProvider>
  )
  const rendered = render(renderTree())

  return {
    queryClient,
    rerender: () => rendered.rerender(renderTree()),
  }
}

describe('model mutate drawer', () => {
  test('keeps unsaved fields when the current model cache refreshes', async () => {
    const { queryClient, rerender } = renderDrawer()
    const user = userEvent.setup()
    const description = await screen.findByLabelText('Description')

    await waitFor(() => expect(description).toHaveValue('Server description'))
    await user.clear(description)
    await user.type(description, 'Unsaved description')

    mockedGetModel.mockResolvedValueOnce({
      success: true,
      data: { ...MODEL, description: 'Refreshed server description' },
    })
    await act(async () => {
      await queryClient.refetchQueries({
        queryKey: modelsQueryKeys.detail(MODEL.id),
      })
    })
    rerender()

    expect(description).toHaveValue('Unsaved description')
  })
})
