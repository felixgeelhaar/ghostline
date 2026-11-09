import { describe, expect, it, vi } from 'vitest'
import { copyRevisionSha, type ClipboardWriter } from './copyRevisionSha'

const fullSha = 'a1b2c3d4e5f60718293a4b5c6d7e8f9012345678'

describe('copyRevisionSha', () => {
  it('writes the full SHA and confirms with the short SHA', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    const result = await copyRevisionSha(fullSha, { writeText })
    expect(writeText).toHaveBeenCalledWith(fullSha)
    expect(result).toEqual({ ok: true, message: 'Copied full SHA for revision a1b2c3d.' })
  })

  it('reports that the clipboard is unavailable', async () => {
    const result = await copyRevisionSha(fullSha, undefined)
    expect(result.ok).toBe(false)
    expect(result.message).toContain('Clipboard access is unavailable')
    expect(result.message).toContain(fullSha)
  })

  it('reports a denied clipboard write with the full SHA for manual copy', async () => {
    const clipboard: ClipboardWriter = { writeText: vi.fn().mockRejectedValue(new DOMException('denied', 'NotAllowedError')) }
    const result = await copyRevisionSha(fullSha, clipboard)
    expect(result.ok).toBe(false)
    expect(result.message).toContain('Clipboard access was denied')
    expect(result.message).toContain(fullSha)
  })

  it('reports a synchronous clipboard failure', async () => {
    const clipboard: ClipboardWriter = { writeText: vi.fn(() => { throw new Error('boom') }) }
    const result = await copyRevisionSha(fullSha, clipboard)
    expect(result.ok).toBe(false)
    expect(result.message).toContain('Could not copy')
  })
})
