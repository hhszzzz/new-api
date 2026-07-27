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
import DOMPurify, { type Config } from 'dompurify'
import { useEffect, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

export type HtmlContentVariant = 'inline' | 'isolated'

interface HtmlContentProps {
  content: string
  className?: string
  variant?: HtmlContentVariant
}

// `allow-scripts` without `allow-same-origin` keeps the frame on an opaque
// origin: embedded players and dashboards can run their own code, but they
// cannot reach the application's DOM, storage, or cookies.
const isolatedContentSandbox =
  'allow-forms allow-popups allow-popups-to-escape-sandbox allow-presentation allow-scripts'

// Isolated content is rendered full-bleed, below a fixed public header. Without
// this offset the first rows of a pasted page sit underneath the header, and a
// short fragment disappears behind it entirely.
const isolatedContentOffset = 'pt-16'

// Admins paste either a fragment (`<p>About us…</p>`) or a whole page exported
// from a site builder. A whole page carries `html`/`body` rules and a `<title>`,
// which only behave correctly inside a real document.
const htmlDocumentPattern = /^\s*(?:<!doctype\s+html|<html[\s>]|<body[\s>])/i

function isHtmlDocument(content: string): boolean {
  return htmlDocumentPattern.test(content)
}

const isolatedContentBaseStyles = `
<style>
  :host {
    display: block;
    width: 100%;
    color: inherit;
    font: inherit;
  }

  *,
  *::before,
  *::after {
    box-sizing: border-box;
  }

  img,
  video,
  iframe {
    max-width: 100%;
  }

  iframe {
    border: 0;
  }
</style>
`

const isolatedSanitizeOptions = {
  ADD_ATTR: [
    'allowfullscreen',
    'autoplay',
    'class',
    'controls',
    'default',
    'id',
    'kind',
    'label',
    'loading',
    'loop',
    'muted',
    'playsinline',
    'poster',
    'preload',
    'referrerpolicy',
    'rel',
    'srclang',
    'style',
    'target',
  ],
  ADD_TAGS: ['audio', 'iframe', 'picture', 'source', 'style', 'track', 'video'],
  FORBID_ATTR: ['srcdoc'],
  FORBID_TAGS: ['base', 'embed', 'link', 'meta', 'object', 'script'],
  FORCE_BODY: true,
} satisfies Config

/**
 * A pasted page is rendered in a sandboxed frame, so it keeps far more of what
 * its author wrote than a fragment can.
 *
 * It keeps its own `html`/`head`/`body` structure, so the `body { … }` rules and
 * `<title>` it ships with still mean what the author intended. It also keeps its
 * stylesheets and scripts: pages exported from a site builder are usually a thin
 * shell around a CDN stylesheet plus an on-load script, and stripping those
 * leaves a blank page. The frame — opaque origin, no `allow-same-origin` — is
 * what makes that safe, so the document cannot reach the application's DOM,
 * storage, or cookies regardless of what it loads.
 */
const isolatedDocumentSanitizeOptions = {
  ADD_ATTR: [
    ...isolatedSanitizeOptions.ADD_ATTR,
    'async',
    'charset',
    'content',
    'crossorigin',
    'defer',
    'href',
    'integrity',
    'media',
    'name',
    'sizes',
    'type',
  ],
  ADD_TAGS: [
    ...isolatedSanitizeOptions.ADD_TAGS,
    'head',
    'html',
    'link',
    'meta',
    'noscript',
    'script',
    'title',
  ],
  FORCE_BODY: false,
  WHOLE_DOCUMENT: true,
} satisfies Config

function hardenIsolatedFragment(root: ParentNode): void {
  root.querySelectorAll('a[target="_blank"]').forEach((link) => {
    const rel = new Set(
      link.getAttribute('rel')?.split(/\s+/).filter(Boolean) ?? []
    )

    rel.add('noopener')
    rel.add('noreferrer')
    link.setAttribute('rel', [...rel].join(' '))
  })

  root.querySelectorAll('iframe').forEach((frame) => {
    frame.removeAttribute('srcdoc')
    frame.setAttribute('sandbox', isolatedContentSandbox)
    frame.setAttribute('referrerpolicy', 'no-referrer')

    if (!frame.hasAttribute('loading')) {
      frame.setAttribute('loading', 'lazy')
    }
  })
}

function hardenIsolatedHtml(html: string, isDocument: boolean): string {
  if (typeof document === 'undefined') {
    return html
  }

  // A document is hardened in place: routing it through a `<template>` would
  // discard the `html`/`head`/`body` structure the pasted page depends on.
  if (isDocument) {
    const parsed = new DOMParser().parseFromString(html, 'text/html')
    hardenIsolatedFragment(parsed)

    return parsed.documentElement.outerHTML
  }

  const template = document.createElement('template')
  template.innerHTML = html
  hardenIsolatedFragment(template.content)

  return template.innerHTML
}

function sanitizeIsolatedHtml(content: string): string {
  const isDocument = isHtmlDocument(content)
  const html = DOMPurify.sanitize(
    content,
    isDocument ? isolatedDocumentSanitizeOptions : isolatedSanitizeOptions
  )

  return hardenIsolatedHtml(html, isDocument)
}

function syncDarkClass(wrapper: HTMLElement): void {
  const isDark = document.documentElement.classList.contains('dark')
  wrapper.classList.toggle('dark', isDark)
}

/**
 * Render a pasted page inside a sandboxed frame.
 *
 * A shadow root cannot host `html`/`head`/`body`, so a whole document rendered
 * there loses every `body { … }` rule it ships with and leaks its `<title>` as
 * visible text. A frame gives it the real document it was written against,
 * matching how an admin-provided URL is already embedded.
 */
function IsolatedHtmlDocument(props: {
  className?: string
  html: string
}): React.ReactElement {
  const { t } = useTranslation()

  return (
    <div className={isolatedContentOffset}>
      <iframe
        // `allow-scripts` without `allow-same-origin` keeps the frame on an
        // opaque origin, so the pasted page cannot reach the application's DOM,
        // storage, or cookies.
        sandbox={isolatedContentSandbox}
        srcDoc={props.html}
        title={t('Embedded content')}
        className={cn('h-[calc(100svh-4rem)] w-full border-0', props.className)}
      />
    </div>
  )
}

function IsolatedHtmlContent(props: {
  className?: string
  html: string
}): React.ReactElement {
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const container = containerRef.current
    if (!container) {
      return
    }

    const shadowRoot =
      container.shadowRoot ?? container.attachShadow({ mode: 'open' })
    const applicationStyleNodes = [
      ...document.head.querySelectorAll<HTMLLinkElement | HTMLStyleElement>(
        'style, link[rel="stylesheet"]'
      ),
    ].map((node) => node.cloneNode(true))

    const wrapper = document.createElement('div')
    syncDarkClass(wrapper)
    wrapper.innerHTML = props.html

    const contentTemplate = document.createElement('template')
    contentTemplate.innerHTML = isolatedContentBaseStyles

    shadowRoot.replaceChildren(
      ...applicationStyleNodes,
      contentTemplate.content,
      wrapper
    )

    const observer = new MutationObserver(() => syncDarkClass(wrapper))
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class'],
    })

    return () => observer.disconnect()
  }, [props.html])

  return (
    <div
      ref={containerRef}
      className={cn('block w-full', isolatedContentOffset, props.className)}
    />
  )
}

export function HtmlContent(props: HtmlContentProps) {
  const variant = props.variant ?? 'inline'
  const isolated = variant === 'isolated'
  const html = useMemo(
    () =>
      isolated
        ? sanitizeIsolatedHtml(props.content)
        : DOMPurify.sanitize(props.content),
    [isolated, props.content]
  )

  if (isolated) {
    return isHtmlDocument(props.content) ? (
      <IsolatedHtmlDocument className={props.className} html={html} />
    ) : (
      <IsolatedHtmlContent className={props.className} html={html} />
    )
  }

  return (
    <div
      className={cn(
        'prose prose-neutral dark:prose-invert max-w-none',
        props.className
      )}
      // eslint-disable-next-line react/no-danger -- html is sanitized above
      dangerouslySetInnerHTML={{ __html: html }}
    />
  )
}
