import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { ClientPolicyIndicator } from '../client-policy-indicator'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: (name: string) => <span data-client-icon={name} />,
}))

describe('client policy indicator', () => {
  test('keeps brand icons intact and outlines allowed clients in green', async () => {
    const user = userEvent.setup()
    render(
      <ClientPolicyIndicator
        policy={{ mode: 'allow', clients: ['codex', 'claude_code'] }}
      />
    )

    const indicator = screen.getByTestId('client-policy-indicator')
    expect(indicator).toHaveAttribute('data-policy-mode', 'allow')
    expect(indicator.querySelector('[data-slot="avatar"]')).toHaveClass(
      'after:!border-success'
    )
    expect(
      indicator.querySelector('[data-client-icon="Codex.Color"]')
    ).not.toBeNull()

    await user.hover(indicator)
    expect(
      await screen.findByText('Allow only selected clients: Codex, Claude Code')
    ).toBeVisible()
  })

  test('groups Codex surfaces and keeps identifiable client logos in the policy marker', async () => {
    const user = userEvent.setup()
    render(
      <ClientPolicyIndicator
        policy={{
          mode: 'allow',
          clients: [
            'codex_vscode',
            'kimi_code',
            'cline',
            'zcode',
            'antigravity',
          ],
        }}
      />
    )

    const indicator = screen.getByTestId('client-policy-indicator')
    expect(
      indicator.querySelector('[data-client-icon="Codex.Color"]')
    ).not.toBeNull()
    expect(
      indicator.querySelector('[data-client-icon="Moonshot"]')
    ).not.toBeNull()
    expect(indicator.querySelector('[data-client-icon="Cline"]')).not.toBeNull()

    await user.hover(indicator)
    expect(
      await screen.findByText(
        'Allow only selected clients: Codex, Kimi Code, Cline, ZCode, Antigravity'
      )
    ).toBeVisible()
  })

  test('outlines denied clients in red and shows custom client names', async () => {
    const user = userEvent.setup()
    render(
      <ClientPolicyIndicator
        policy={{ mode: 'deny', clients: ['custom_agent'] }}
      />
    )

    const indicator = screen.getByTestId('client-policy-indicator')
    expect(indicator).toHaveAttribute('data-policy-mode', 'deny')
    expect(indicator.querySelector('[data-slot="avatar"]')).toHaveClass(
      'after:!border-destructive'
    )
    expect(indicator.querySelector('svg')).not.toBeNull()

    await user.hover(indicator)
    expect(
      await screen.findByText('Block selected clients: custom_agent')
    ).toBeVisible()
  })

  test('marks an empty allow list as denying every client', async () => {
    const user = userEvent.setup()
    render(<ClientPolicyIndicator policy={{ mode: 'allow', clients: [] }} />)

    const indicator = screen.getByTestId('client-policy-indicator')
    expect(indicator).toHaveAttribute('data-policy-mode', 'allow')
    expect(indicator.querySelector('svg')).not.toBeNull()
    await user.hover(indicator)
    expect(
      await screen.findByText('Allow only selected clients: None')
    ).toBeVisible()
  })

  test.each([
    undefined,
    { mode: 'unrestricted', clients: [] },
    { mode: 'deny', clients: [] },
  ])('does not mark unrestricted policies: %s', (policy) => {
    const result = render(<ClientPolicyIndicator policy={policy} />)
    expect(result.queryByTestId('client-policy-indicator')).toBeNull()
  })
})
