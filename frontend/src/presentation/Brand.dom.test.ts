// @vitest-environment happy-dom
import { readFileSync, existsSync } from 'node:fs'
import { join } from 'node:path'
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchGhostlineSession } from '../application/fetchGhostlineSession'
import App from './App.vue'

vi.mock('../application/fetchGhostlineSession', () => ({ fetchGhostlineSession: vi.fn() }))

// Vitest runs from the frontend package directory.
const frontend = process.cwd()
const read = (path: string) => readFileSync(join(frontend, path), 'utf8')

let app: ReturnType<typeof createApp> | undefined
afterEach(() => { app?.unmount(); app = undefined; document.body.innerHTML = '' })

describe('Ghostline brand', () => {
  it('declares the SVG favicon, its PNG fallback and the touch icon, all served from public/', () => {
    const head = new DOMParser().parseFromString(read('index.html'), 'text/html').head
    const links = [...head.querySelectorAll('link[rel~="icon"], link[rel="apple-touch-icon"]')]
      .map((link) => [link.getAttribute('rel'), link.getAttribute('href'), link.getAttribute('type')])
    expect(links).toEqual([
      ['icon', '/favicon.svg', 'image/svg+xml'],
      ['icon', '/favicon-32.png', 'image/png'],
      ['apple-touch-icon', '/apple-touch-icon.png', null],
    ])
    for (const [, href] of links) expect(existsSync(join(frontend, 'public', href ?? ''))).toBe(true)
  })

  it('keeps the logo files self-contained, so the img-src self CSP renders them', () => {
    for (const file of ['public/favicon.svg', 'public/logo.svg']) {
      const svg = read(file)
      expect(svg).toMatch(/^<svg xmlns="http:\/\/www\.w3\.org\/2000\/svg"/)
      expect(svg).not.toMatch(/<script|href=|url\(/i)
    }
  })

  it('shows the logo mark beside the GHOSTLINE wordmark, as decoration the wordmark names', async () => {
    vi.mocked(fetchGhostlineSession).mockResolvedValue({ authenticated: false, authRequired: false } as never)
    const host = document.createElement('div')
    document.body.append(host)
    app = createApp(App)
    app.mount(host)
    await nextTick()
    const mark = host.querySelector('.brand img.brand-mark')
    // Vite may inline the small public SVG as a data: URL; the CSP allows both forms.
    expect(mark?.getAttribute('src')).toMatch(/^(\/logo\.svg|data:image\/svg\+xml,)/)
    expect(decodeURIComponent(mark?.getAttribute('src') ?? '')).not.toContain('<script')
    expect(mark?.getAttribute('alt')).toBe('')
    expect(host.querySelector('.brand')?.textContent).toContain('GHOSTLINE')
  })
})
