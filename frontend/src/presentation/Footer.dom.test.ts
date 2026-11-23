// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchGhostlineSession } from '../application/fetchGhostlineSession'
import App from './App.vue'

vi.mock('../application/fetchGhostlineSession', () => ({ fetchGhostlineSession: vi.fn() }))

let app: ReturnType<typeof createApp> | undefined
afterEach(() => { app?.unmount(); app = undefined; document.body.innerHTML = ''; vi.unstubAllEnvs() })

async function footerText() {
  vi.mocked(fetchGhostlineSession).mockResolvedValue({ authenticated: false, authRequired: false } as never)
  const host = document.createElement('div')
  document.body.append(host)
  app = createApp(App)
  app.mount(host)
  await nextTick()
  return host.querySelector('footer')?.textContent ?? ''
}

describe('footer', () => {
  it('names the release the build was made from', async () => {
    vi.stubEnv('VITE_GHOSTLINE_VERSION', 'v0.1.0-rc.1')
    expect(await footerText()).toContain('GHOSTLINE // v0.1.0-rc.1')
  })

  it('marks a build without a release version as dev, not as a hackathon day', async () => {
    vi.stubEnv('VITE_GHOSTLINE_VERSION', '')
    const text = await footerText()
    expect(text).toContain('GHOSTLINE // dev')
    expect(text).not.toMatch(/DAY \d/)
  })
})
