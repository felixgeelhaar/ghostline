import { z } from 'zod'

export const MIN_SEARCH_LENGTH = 3
export const MAX_SEARCH_LENGTH = 256

const SHORTHAND = /^([A-Za-z0-9][A-Za-z0-9-]{0,38})\/([A-Za-z0-9._-]{1,100})#([1-9]\d*)$/

export type ImportInput =
  | { kind: 'pull-request'; url: string }
  | { kind: 'search'; query: string }
  | { kind: 'too-short' }
  | { kind: 'empty' }

// The import field takes a pull request URL, owner/repo#number, or text to search
// the signed-in user's pull requests with.
export function classifyImportInput(raw: string): ImportInput {
  const text = raw.trim()
  if (!text) return { kind: 'empty' }
  const shorthand = SHORTHAND.exec(text)
  if (shorthand && shorthand[2] !== '.' && shorthand[2] !== '..') {
    return { kind: 'pull-request', url: `https://github.com/${shorthand[1]}/${shorthand[2]}/pull/${shorthand[3]}` }
  }
  if (/^https?:\/\//i.test(text) || /^(www\.)?github\.com\//i.test(text)) return { kind: 'pull-request', url: text }
  const query = text.split(/\s+/).join(' ').slice(0, MAX_SEARCH_LENGTH)
  if (query.length < MIN_SEARCH_LENGTH) return { kind: 'too-short' }
  return { kind: 'search', query }
}

export const SuggestionStateSchema = z.enum(['open', 'draft', 'merged', 'closed'])
export type SuggestionState = z.infer<typeof SuggestionStateSchema>

export const PullRequestSuggestionSchema = z.object({
  repository: z.string(), owner: z.string().min(1), repo: z.string().min(1), number: z.number().int().positive(),
  title: z.string(), state: SuggestionStateSchema,
  // When GitHub last changed the pull request; not when it was pushed or reviewed.
  updatedAt: z.string().refine((value) => !Number.isNaN(Date.parse(value)), 'updatedAt must be a timestamp'),
  url: z.string().url(),
}).refine((item) => item.repository === `${item.owner}/${item.repo}`
  && item.url === `https://github.com/${item.owner}/${item.repo}/pull/${item.number}`, 'A suggestion must link to the pull request it names.')

export type PullRequestSuggestion = z.infer<typeof PullRequestSuggestionSchema>

export const PullRequestSearchResultSchema = z.object({
  items: z.array(PullRequestSuggestionSchema),
  partial: z.boolean(),
})

export type PullRequestSearchResult = z.infer<typeof PullRequestSearchResultSchema>

export function suggestionStateLabel(state: SuggestionState): string {
  return state.toUpperCase()
}
