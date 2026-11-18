import { describe, expect, it } from 'vitest'
import { CheckAnnotationPageSchema } from './checkAnnotations'

const sha = 'a'.repeat(40)
const page = {
  revisionSha: sha, checkRunId: 314, checkRunUrl: 'https://github.com/acme/app/actions/runs/314',
  page: 1, pageSize: 100, hasMore: false,
  items: [{ path: 'backend/payments.go', startLine: 12, endLine: 14, level: 'warning', title: 'Possible replay', message: 'Check the transaction boundary.', sourceUrl: `https://github.com/acme/app/blob/${sha}/backend/payments.go#L12-L14` }],
}

describe('CheckAnnotationPageSchema', () => {
  it('accepts source-linked annotations for an exact check run revision', () => {
    expect(CheckAnnotationPageSchema.parse(page)).toEqual(page)
  })

  it('accepts annotations when GitHub does not provide a check-run URL', () => {
    const { checkRunUrl: _omitted, ...withoutRunUrl } = page
    expect(CheckAnnotationPageSchema.parse(withoutRunUrl)).toEqual(withoutRunUrl)
  })

  it('rejects non-web evidence links', () => {
    expect(CheckAnnotationPageSchema.safeParse({ ...page, items: [{ ...page.items[0], sourceUrl: 'javascript:alert(1)' }] }).success).toBe(false)
  })

  it('rejects malformed revision identities', () => {
    expect(CheckAnnotationPageSchema.safeParse({ ...page, revisionSha: 'abc' }).success).toBe(false)
  })
})
