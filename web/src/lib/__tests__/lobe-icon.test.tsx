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
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { runInNewContext } from 'node:vm'

import { act, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { getLobeIcon, getLobeIconNames } from '../lobe-icon'

vi.mock('@lobehub/icons/es/LmStudio/components/Mono.js', () => ({
  default: (props: { size?: number }) => (
    <svg aria-label='LM Studio' height={props.size} width={props.size} />
  ),
}))

vi.mock('@lobehub/icons/es/Mistral/components/Text.js', () => {
  throw new Error('Icon chunk unavailable')
})

describe('getLobeIcon', () => {
  test('renders common base and color variants without loading the icon barrel', () => {
    const { container, rerender } = render(getLobeIcon('OpenAI', 24))

    expect(container.querySelector('svg')).not.toBeNull()

    rerender(getLobeIcon('Claude.Color', 24))

    expect(container.querySelector('svg')).not.toBeNull()
  })

  test('keeps the branded avatar shape and icon scale for common icons', () => {
    const { container } = render(
      getLobeIcon('Claude.Avatar.type={"platform"}', 32)
    )

    expect(screen.getByLabelText('Claude')).toHaveStyle({
      background: '#D97757',
      borderRadius: '50%',
      height: '32px',
      width: '32px',
    })
    expect(container.querySelector('svg')).toHaveStyle({
      transform: 'scale(0.75)',
    })
  })

  test('preserves OpenAI avatar type backgrounds', () => {
    render(getLobeIcon('OpenAI.Avatar.type={"platform"}', 20))

    expect(screen.getByLabelText('OpenAI')).toHaveStyle({
      background: '#0000FE',
    })
  })

  test('keeps OpenAI.Color on the monochrome model-square icon', () => {
    const { container } = render(getLobeIcon('OpenAI.Color', 20))

    const icon = container.querySelector('svg')
    expect(icon).toHaveAttribute('fill', 'currentColor')
    expect(icon).toHaveAttribute('height', '20')
    expect(icon).toHaveAttribute('width', '20')
    expect(container.querySelector('[aria-label="OpenAI"]')).toBeNull()
  })

  test('keeps avatar requests on the original model-square renderer', () => {
    const { container } = render(
      getLobeIcon("Gemini.Avatar.type={'platform'}", 20)
    )

    expect(screen.getByLabelText('Gemini')).toHaveStyle({ background: '#fff' })
    expect(container.querySelector('svg')).toHaveAttribute(
      'fill',
      'currentColor'
    )
    expect(container.querySelector('path[fill="#3186FF"]')).toBeNull()
  })

  test('renders a fixed-size fallback when no icon is configured', () => {
    render(getLobeIcon(null, 18))

    expect(screen.getByText('?')).toHaveStyle({ width: '18px', height: '18px' })
  })

  test('loads uncommon icons from their individual chunk', async () => {
    const { container } = render(getLobeIcon('LmStudio', 24))

    await waitFor(() => expect(container.querySelector('svg')).not.toBeNull())
    expect(screen.getByLabelText('LM Studio')).toHaveAttribute('width', '24')
  })

  test('renders the Sub2API custom icon at the requested size', () => {
    const { container } = render(getLobeIcon('Sub2API', 22))

    expect(container.querySelector('svg')).toHaveAttribute('width', '22')
    expect(container.querySelector('svg')).toHaveAttribute('height', '22')
  })

  test.each([
    '/build/web/node_modules/@lobehub/icons/es/',
    'D:\\a\\new-api\\web\\node_modules\\@lobehub\\icons\\es\\',
  ])('includes icon variants when bundling from %s', (root) => {
    // Vitest does not apply Rspack's webpackInclude filter.
    const source = readFileSync(
      resolve(import.meta.dirname, '../lobe-icon.tsx'),
      'utf8'
    )
    const comment = source.match(/\/\*\s*(webpackInclude:[\s\S]*?)\*\//)
    expect(comment).not.toBeNull()
    const { webpackInclude } = runInNewContext(
      `({${comment?.[1]}})`,
      {},
      { timeout: 1000 }
    ) as { webpackInclude: RegExp }
    const separator = root.includes('\\') ? '\\' : '/'
    const files = [
      'OpenAI/components/Mono.js',
      'Claude/components/Color.js',
      'Gemini/components/Color.js',
      'Gemma/components/Simple.js',
      'LobeHub/components/Morden.js',
      'OpenAI/index.js',
      'OpenAI/components/Mono.d.ts',
      'OpenAI/components/Unknown.js',
    ]
    expect(
      files.filter((file) =>
        webpackInclude.test(root + file.replaceAll('/', separator))
      )
    ).toEqual(files.slice(0, 5))
  })

  test('preserves configured sizes and accessibility props when changing icons', async () => {
    const { rerender } = render(
      getLobeIcon('Claude.Color.size={32}.role="img".aria-label="Claude icon"')
    )
    expect(
      await screen.findByRole('img', { name: 'Claude icon' })
    ).toHaveAttribute('width', '32')
    rerender(
      getLobeIcon('Gemini.Color.role="img".aria-label="Gemini icon"', 28)
    )
    expect(
      await screen.findByRole('img', { name: 'Gemini icon' })
    ).toHaveAttribute('width', '28')
    expect(
      screen.queryByRole('img', { name: 'Claude icon' })
    ).not.toBeInTheDocument()
  })

  test.each(['OpenAI.Unknown', 'Gemma.Simple', 'LobeHub.Morden'])(
    'loads %s with fallback for unsupported variants',
    async (name) => {
      render(getLobeIcon(`${name}.role="img".aria-label="Special icon"`, 24))
      expect(
        await screen.findByRole('img', { name: 'Special icon' })
      ).toHaveAttribute('height', '24')
    }
  )

  test('keeps a sized placeholder when a chunk fails and accepts later size changes', async () => {
    const { rerender } = render(getLobeIcon('Mistral.Text', 18))
    await act(async () => {
      await vi.dynamicImportSettled()
    })
    expect(screen.getByText('M')).toHaveStyle({ width: '18px', height: '18px' })
    rerender(getLobeIcon('Mistral.Text', 36))
    expect(screen.getByText('M')).toHaveStyle({ width: '36px', height: '36px' })
  })

  test('keeps placeholders for missing names and lists installed custom icons', () => {
    const { rerender } = render(getLobeIcon('NotAnInstalledIcon', 30))
    expect(screen.getByText('N')).toHaveStyle({ width: '30px', height: '30px' })
    rerender(getLobeIcon('  '))
    expect(screen.getByText('?')).toBeVisible()
    rerender(getLobeIcon('SGLang', 24))
    expect(screen.getByRole('presentation', { hidden: true })).toHaveAttribute(
      'width',
      '24'
    )
    expect(getLobeIconNames()).toEqual(
      expect.arrayContaining([
        'OpenAI',
        'Claude.Color',
        'SGLang',
        'Sub2API',
        'Wan',
      ])
    )
  })
})
