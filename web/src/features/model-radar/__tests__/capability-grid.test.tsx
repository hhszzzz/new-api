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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { CapabilityGrid } from '../components/capability-grid'
import { ModelBadge } from '../components/model-badge'
import { resolveRadarSettings } from '../lib/model-radar'
import { configurationFixture as fixture } from './fixtures'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    i18n: { language: 'en', resolvedLanguage: 'en' },
    t: (key: string, values?: Record<string, string | number>) =>
      key.replaceAll(/\{\{(\w+)\}\}/g, (_, name: string) =>
        String(values?.[name] ?? `{{${name}}}`)
      ),
  }),
}))
vi.mock('@/hooks', () => ({ useMediaQuery: () => true }))
vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: (iconName: string) => <svg data-icon-key={iconName} />,
}))

describe('model radar capability grid', () => {
  test('All shows vendor sections, resolves Kimi aliases, and preserves source identities in details', async () => {
    const user = userEvent.setup()
    const view = render(
      <CapabilityGrid
        history={[]}
        configurations={[{ ...fixture, model: 'k3' }, fixture]}
        groupByVendor
      />
    )
    expect(screen.getByRole('region', { name: 'Moonshot' })).toBeVisible()
    expect(screen.getByRole('region', { name: 'OpenAI' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'kimi-k3' })).toHaveAttribute(
      'title',
      'k3'
    )
    expect(
      view.container.querySelector('[data-icon-key="Moonshot.Color"]')
    ).not.toBeNull()
    await user.click(
      screen.getByRole('button', { name: 'View details for k3 medium' })
    )
    expect(await screen.findByRole('dialog')).toHaveAccessibleName(
      'kimi-k3 medium Moonshot'
    )
  })

  test('keeps different source models separate when their display names collide', () => {
    render(
      <CapabilityGrid
        history={[]}
        configurations={[
          { ...fixture, model: 'k3' },
          { ...fixture, model: 'kimi-k3' },
        ]}
        settings={resolveRadarSettings({})}
      />
    )
    expect(screen.getAllByRole('heading', { name: 'kimi-k3' })).toHaveLength(2)
    expect(
      screen.getByRole('button', { name: 'View details for k3 medium' })
    ).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'View details for kimi-k3 medium' })
    ).toBeVisible()
  })
  test('preserves source model order instead of crowning a two-sample configuration as the overall leader', () => {
    render(
      <CapabilityGrid
        history={[]}
        configurations={[
          fixture,
          {
            ...fixture,
            model: 'experimental',
            iq: 150,
            passed: 2,
            valid_tasks: 2,
          },
        ]}
      />
    )
    expect(
      screen
        .getAllByRole('heading', { level: 3 })
        .map((node) => node.textContent)
    ).toEqual(['gpt-radar', 'experimental'])
    expect(screen.queryByLabelText('IQ max')).toBeNull()
  })

  test('places stronger efforts first and pairs sample counts with duration without card prices', () => {
    render(
      <CapabilityGrid
        history={[]}
        configurations={[
          { ...fixture, effort: 'low' },
          { ...fixture, effort: 'ultra' },
        ]}
      />
    )
    const cards = screen.getAllByRole('button', { name: /View details/ })
    expect(cards[0]).toHaveAccessibleName('View details for gpt-radar ultra')
    expect(cards[1]).toHaveAccessibleName('View details for gpt-radar low')
    expect(within(cards[0]).queryByTitle('Average cost')).toBeNull()
    expect(within(cards[0]).getByTitle('Average duration')).toHaveTextContent(
      '4.5 min'
    )
    expect(
      within(cards[0]).getByTitle('Passed / valid samples')
    ).toHaveTextContent('7/10')
    expect(
      within(cards[0]).getByTitle('Passed / valid samples').parentElement
    ).toHaveClass('justify-between')
  })
  test('renders a complete vendor badge without clipping it', () => {
    const { container } = render(<ModelBadge color='#2563eb' model='gpt-5.4' />)
    const wrapper = container.firstElementChild

    expect(wrapper).toHaveClass('size-6')
    expect(wrapper).not.toHaveClass('overflow-hidden')
    expect(wrapper?.querySelector('svg')).not.toBeNull()
  })

  test('renders the vendor configured icon variant instead of the radar fallback', () => {
    const { container } = render(
      <CapabilityGrid
        history={[]}
        configurations={[{ ...fixture, model: 'dsh-deepseek-v4-pro' }]}
        iconRegistry={{
          modelIcons: new Map<string, string>(),
          providerIcons: new Map([['deepseek', 'DeepSeek.Color']]),
        }}
      />
    )

    expect(
      container.querySelector('[data-icon-key="DeepSeek.Color"]')
    ).not.toBeNull()
  })

  test('keeps compact cards large enough to tap and shows only summary metrics', () => {
    render(<CapabilityGrid history={[]} configurations={[fixture]} />)

    expect(screen.getAllByRole('heading', { name: 'gpt-radar' })).toHaveLength(
      1
    )
    const card = screen.getByRole('button', {
      name: 'View details for gpt-radar medium',
    })
    expect(within(card).getByText('93.8')).toBeVisible()
    expect(within(card).queryByText('$1.25')).toBeNull()
    expect(within(card).getByText('4.5 min')).toBeVisible()
    expect(within(card).getByText('7/10')).toBeVisible()
    expect(card).toHaveClass('min-h-11')
    expect(within(card).queryByLabelText(/runs in 24h/)).toBeNull()
    expect(screen.queryByText('Codex')).toBeNull()
  })

  test('aligns missing effort slots on desktop and hides them in the mobile two-column flow', () => {
    render(
      <CapabilityGrid
        history={[]}
        configurations={[
          fixture,
          { ...fixture, model: 'other', effort: 'high' },
        ]}
      />
    )
    const group = screen.getByRole('region', { name: 'gpt-radar' })
    const card = within(group).getByRole('button')
    const grid = card.parentElement
    expect(grid).toHaveClass('grid-cols-2')
    expect(grid?.style.getPropertyValue('--effort-count')).toBe('2')
    const placeholder = grid?.firstElementChild
    expect(placeholder).toHaveAttribute('aria-hidden', 'true')
    expect(placeholder).toHaveClass('hidden', 'md:block')
  })

  test('shows missing duration as a dash', () => {
    render(
      <CapabilityGrid
        history={[]}
        configurations={[
          {
            ...fixture,
            runs_24h: null,
            average_price_usd: null,
            average_minutes: null,
          },
        ]}
      />
    )
    const card = screen.getByRole('button')
    expect(within(card).getByText('—')).toBeVisible()
    expect(within(card).queryByLabelText(/runs in 24h/)).toBeNull()
  })

  test('opens complete metrics from the keyboard and restores focus after Escape', async () => {
    const user = userEvent.setup()
    render(<CapabilityGrid history={[]} configurations={[fixture]} />)
    const detailsButton = screen.getByRole('button', {
      name: 'View details for gpt-radar medium',
    })

    detailsButton.focus()
    await user.keyboard('{Enter}')

    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveAccessibleName('gpt-radar medium OpenAI')
    expect(within(dialog).getByText('Runs 24h / 48h / total')).toBeVisible()
    expect(within(dialog).getByText('3 / 6 / 12')).toBeVisible()
    expect(screen.getByText('Combined cost index')).toBeVisible()
    expect(screen.getByText('45')).toBeVisible()
    expect(
      screen.getByText(
        (_, element) => element?.textContent === 'Cost samples: 10'
      )
    ).toBeVisible()
    expect(
      screen.getByText(
        'Software engineering IQ uses up to the three latest valid samples per task, weighted equally, on a 150-point scale. The pass ratio counts samples, not distinct tasks.'
      )
    ).toBeVisible()

    await user.keyboard('{Escape}')

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(detailsButton).toHaveFocus()
  })

  test('keeps an open detail dialog synchronized with refreshed configuration data', async () => {
    const user = userEvent.setup()
    const view = render(
      <CapabilityGrid history={[]} configurations={[fixture]} />
    )

    await user.click(
      screen.getByRole('button', {
        name: 'View details for gpt-radar medium',
      })
    )
    expect(screen.getByRole('dialog')).toBeVisible()

    view.rerender(
      <CapabilityGrid
        history={[]}
        configurations={[{ ...fixture, iq: 81.25, combined_cost_index: 12 }]}
      />
    )

    expect(screen.getByRole('dialog')).toHaveTextContent('81.25')
    expect(screen.getByRole('dialog')).toHaveTextContent('12')
    expect(screen.getByRole('dialog')).not.toHaveTextContent('93.75')
  })

  test('closes an open detail dialog when its configuration disappears', async () => {
    const user = userEvent.setup()
    const view = render(
      <CapabilityGrid history={[]} configurations={[fixture]} />
    )

    await user.click(
      screen.getByRole('button', {
        name: 'View details for gpt-radar medium',
      })
    )
    expect(screen.getByRole('dialog')).toBeVisible()

    view.rerender(<CapabilityGrid history={[]} configurations={[]} />)

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  test('adds a column when the source introduces a new reasoning effort', () => {
    render(
      <CapabilityGrid
        history={[]}
        configurations={[fixture, { ...fixture, effort: 'turbo', iq: 95 }]}
      />
    )

    const card = screen.getByRole('button', {
      name: 'View details for gpt-radar turbo',
    })
    expect(card.parentElement?.style.getPropertyValue('--effort-count')).toBe(
      '2'
    )
    expect(within(card).getByText('turbo')).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'View details for gpt-radar turbo' })
    ).toBeVisible()
  })

  test('offers an automatic tier switch per model and reports toggles', async () => {
    const user = userEvent.setup()
    const onToggle = vi.fn()
    render(
      <CapabilityGrid
        history={[]}
        configurations={[fixture, { ...fixture, effort: 'high', iq: 120 }]}
        settings={resolveRadarSettings({
          models: { 'gpt-radar': { auto_effort: true } },
        })}
        autoEffort={{
          setting: { policy: 'highest_iq', min_iq_delta: 5, models: {} },
          isSaving: false,
          userModels: ['gpt-radar'],
          onToggle,
        }}
      />
    )

    const toggle = screen.getByRole('switch', {
      name: 'Automatically choose the reasoning tier for gpt-radar',
    })
    expect(toggle).toBeEnabled()
    expect(toggle).not.toBeChecked()
    expect(screen.queryByText('Current')).toBeNull()

    await user.hover(toggle)
    expect(
      await screen.findByText('Enable automatic reasoning tier')
    ).toBeVisible()

    await user.click(toggle)
    expect(onToggle).toHaveBeenCalledWith('gpt-radar', true)
  })

  test('hides the switch when the administrator did not allow tier adjustment', () => {
    render(
      <CapabilityGrid
        history={[]}
        configurations={[fixture]}
        settings={resolveRadarSettings({})}
        autoEffort={{
          setting: { policy: 'highest_iq', min_iq_delta: 5, models: {} },
          isSaving: false,
          userModels: ['gpt-radar'],
          onToggle: vi.fn(),
        }}
      />
    )
    expect(screen.queryByRole('switch')).toBeNull()
  })

  test('hides the switch when the user cannot call the model', () => {
    render(
      <CapabilityGrid
        history={[]}
        configurations={[fixture]}
        settings={resolveRadarSettings({
          models: { 'gpt-radar': { auto_effort: true } },
        })}
        autoEffort={{
          setting: { policy: 'highest_iq', min_iq_delta: 5, models: {} },
          isSaving: false,
          userModels: ['other-model'],
          onToggle: vi.fn(),
        }}
      />
    )
    expect(screen.queryByRole('switch')).toBeNull()
  })

  test('crowns the tier the radar would pick and marks it as automatic', () => {
    render(
      <CapabilityGrid
        history={[]}
        configurations={[fixture, { ...fixture, effort: 'high', iq: 120 }]}
        settings={resolveRadarSettings({
          models: { 'gpt-radar': { auto_effort: true } },
        })}
        autoEffort={{
          setting: {
            policy: 'highest_iq',
            min_iq_delta: 5,
            models: { 'gpt-radar': { enabled: true } },
          },
          isSaving: false,
          userModels: ['gpt-radar'],
          onToggle: vi.fn(),
        }}
      />
    )

    const high = screen.getByRole('button', {
      name: 'View details for gpt-radar high',
    })
    expect(high).toHaveClass('ring-primary/40')
    expect(within(high).getByText('Current')).toBeVisible()
    const medium = screen.getByRole('button', {
      name: 'View details for gpt-radar medium',
    })
    expect(medium).not.toHaveClass('ring-primary/40')
    expect(within(medium).queryByText('Current')).toBeNull()
    expect(
      screen.getByRole('switch', {
        name: 'Automatically choose the reasoning tier for gpt-radar',
      })
    ).toBeChecked()
  })

  test('treats an administrator alias as a reachable model for the automatic tier switch', async () => {
    const user = userEvent.setup()
    render(
      <CapabilityGrid
        history={[]}
        configurations={[fixture]}
        settings={resolveRadarSettings({
          models: { 'gpt-radar': { auto_effort: true, aliases: ['my-gpt'] } },
        })}
        autoEffort={{
          setting: {
            policy: 'highest_iq',
            min_iq_delta: 5,
            models: { 'gpt-radar': { enabled: true } },
          },
          isSaving: false,
          userModels: ['my-gpt'],
          onToggle: vi.fn(),
        }}
      />
    )

    const toggle = screen.getByRole('switch', {
      name: 'Automatically choose the reasoning tier for gpt-radar',
    })
    expect(toggle).toBeEnabled()
    expect(toggle).toBeChecked()
    expect(screen.queryByText('Disable automatic reasoning tier')).toBeNull()

    await user.hover(toggle)
    expect(
      await screen.findByText('Disable automatic reasoning tier')
    ).toBeVisible()
  })

  test('keeps the model name and switch on one line with a truncating label', () => {
    const { container } = render(
      <CapabilityGrid
        history={[]}
        configurations={[fixture]}
        settings={resolveRadarSettings({
          models: { 'gpt-radar': { auto_effort: true } },
        })}
        autoEffort={{
          setting: { policy: 'highest_iq', min_iq_delta: 5, models: {} },
          isSaving: false,
          userModels: ['gpt-radar'],
          onToggle: vi.fn(),
        }}
      />
    )
    const heading = screen.getByRole('heading', { name: 'gpt-radar' })
    expect(heading).toHaveClass('truncate', 'flex-1')
    expect(
      container.querySelector('section[aria-label="gpt-radar"]')
    ).toHaveClass('lg:grid-cols-[16rem_minmax(0,1fr)]')
  })
})
