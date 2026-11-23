import { describe, expect, it } from 'vitest'
import { releaseLabel } from './release'

describe('releaseLabel', () => {
  it.each([
    ['v0.1.0-rc.1', 'v0.1.0-rc.1'],
    ['v1.0.0', 'v1.0.0'],
    ['v12.3.45-beta.2', 'v12.3.45-beta.2'],
  ])('names the release %s', (raw, label) => {
    expect(releaseLabel(raw)).toBe(label)
  })

  it.each([undefined, '', 'dev', '1.0.0', 'v1.0', 'main', 'v1.0.0 <b>', ' v1.0.0'])(
    'labels %j as a development build rather than inventing a version',
    (raw) => {
      expect(releaseLabel(raw)).toBe('dev')
    },
  )
})
