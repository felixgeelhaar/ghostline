// A release tag as the release workflow validates it: vMAJOR.MINOR.PATCH with an
// optional prerelease suffix.
const releaseTag = /^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/

/**
 * The label naming the release this build came from. Anything that is not a release
 * tag, including a missing value, is a development build: never guess a version.
 */
export function releaseLabel(raw: string | undefined): string {
  return raw !== undefined && releaseTag.test(raw) ? raw : 'dev'
}
