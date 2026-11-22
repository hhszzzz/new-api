/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { GroupRatioForm } from '../group-ratio-form'

const { api } = await import('@/lib/api')

type ApiMethod = (url: string, data?: unknown) => Promise<{ data: unknown }>
type MockableApi = {
  get: ApiMethod
  post: ApiMethod
  put: ApiMethod
  delete: ApiMethod
}

const apiClient = api as unknown as MockableApi
const originalMethods = {
  get: apiClient.get,
  post: apiClient.post,
  put: apiClient.put,
  delete: apiClient.delete,
}

const defaults = {
  GroupRatio: '{"default":1,"vip":2}',
  TopupGroupRatio: '{}',
  UserUsableGroups: '{"default":"Default","vip":"VIP"}',
  GroupGroupRatio: '{}',
  AutoGroups: '[]',
  MaxTokenAutoGroups: 5,
  DefaultUseAutoGroup: false,
  GroupSpecialUsableGroup: '{}',
}

type GroupEntry = {
  name: string
  description: string
  ratio: number
  sort_order: number
}

let registry: GroupEntry[]
let options: Array<{ key: string; value: string }>
let createPayloads: Array<Record<string, unknown>>

function installApiFixtures() {
  apiClient.get = async (url) => {
    if (url === '/api/group/manage') {
      return { data: { success: true, data: registry } }
    }
    if (url === '/api/option/') {
      return { data: { success: true, data: options } }
    }
    throw new Error(`Unexpected GET ${url}`)
  }
  apiClient.post = async (url, payload) => {
    expect(url).toBe('/api/group/manage')
    const group = payload as GroupEntry
    createPayloads.push(group)
    registry = [
      ...registry,
      {
        name: group.name,
        description: group.description,
        ratio: group.ratio,
        sort_order: group.sort_order,
      },
    ]
    const ratios = Object.fromEntries(
      registry.map(({ name, ratio }) => [name, ratio])
    )
    const descriptions = Object.fromEntries(
      registry.map(({ name, description }) => [name, description])
    )
    options = [
      { key: 'GroupRatio', value: JSON.stringify(ratios) },
      { key: 'UserUsableGroups', value: JSON.stringify(descriptions) },
      { key: 'TopupGroupRatio', value: '{}' },
      { key: 'GroupGroupRatio', value: '{}' },
      { key: 'AutoGroups', value: '[]' },
      { key: 'MaxTokenAutoGroups', value: '5' },
      { key: 'DefaultUseAutoGroup', value: 'false' },
      { key: 'group_ratio_setting.group_special_usable_group', value: '{}' },
    ]
    return { data: { success: true, data: null } }
  }
  apiClient.put = async () => ({ data: { success: true, data: null } })
  apiClient.delete = async () => ({ data: { success: true, data: null } })
}

function Fixture(props: {
  onSave?: (values: typeof defaults) => Promise<void>
  isSaving?: boolean
}) {
  const [actions, setActions] = useState<HTMLDivElement | null>(null)
  const form = useForm({ defaultValues: defaults })
  return (
    <SettingsPageProvider actionsContainer={actions}>
      <div ref={setActions} />
      <GroupRatioForm
        form={form}
        onSave={props.onSave ?? (async () => {})}
        isSaving={props.isSaving ?? false}
      />
    </SettingsPageProvider>
  )
}

function renderSettings() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <Fixture />
    </QueryClientProvider>
  )
}

describe('group settings workspace', () => {
  beforeEach(() => {
    registry = [
      {
        name: 'default',
        description: 'Default access',
        ratio: 1,
        sort_order: 0,
      },
      { name: 'vip', description: 'Priority access', ratio: 2, sort_order: 1 },
    ]
    options = Object.entries(defaults).map(([key, value]) => ({
      key,
      value: String(value),
    }))
    options.push({
      key: 'group_ratio_setting.group_special_usable_group',
      value: '{}',
    })
    createPayloads = []
    installApiFixtures()
  })

  afterEach(() => {
    apiClient.get = originalMethods.get
    apiClient.post = originalMethods.post
    apiClient.put = originalMethods.put
    apiClient.delete = originalMethods.delete
    vi.restoreAllMocks()
  })

  it('organizes registry, primary-group access, and billing override settings', async () => {
    renderSettings()
    expect(await screen.findByText('default')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Delete default' })
    ).toBeDisabled()
    expect(screen.getByRole('tab', { name: 'Groups' })).toBeInTheDocument()
    expect(
      screen.getByRole('tab', { name: 'Primary-group access' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('tab', { name: 'Billing overrides' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('switch', { name: 'Default new tokens to Auto' })
    ).toBeInTheDocument()
  })

  it('creates a group with a decimal billing ratio through the registry', async () => {
    const user = userEvent.setup()
    renderSettings()

    await screen.findByText('default')
    await user.click(screen.getByRole('button', { name: 'Add group' }))
    await user.type(screen.getByRole('textbox', { name: 'Group name' }), 'team')
    await user.type(screen.getByLabelText('Description'), 'Team access')
    const ratio = screen.getByLabelText('Billing ratio')
    await user.clear(ratio)
    await user.type(ratio, '0.04')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(createPayloads).toHaveLength(1))
    expect(createPayloads[0]).toEqual({
      old_name: '',
      name: 'team',
      description: 'Team access',
      ratio: 0.04,
      sort_order: 2,
    })
    expect(await screen.findByText('team')).toBeInTheDocument()
  })
})
