export class GhostlineApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
    readonly code?: string,
  ) {
    super(message)
    this.name = 'GhostlineApiError'
  }
}

export function apiErrorMessage(response: Response, payload: unknown, fallback: string): string {
  if (response.status === 429) {
    const seconds = Number(response.headers.get('Retry-After'))
    if (Number.isInteger(seconds) && seconds > 0) {
      return `Too many requests. Try again in ${seconds} ${seconds === 1 ? 'second' : 'seconds'}.`
    }
  }
  if (typeof payload === 'object' && payload !== null && 'error' in payload) return String(payload.error)
  return fallback
}

export function ghostlineApiError(response: Response, payload: unknown, fallback: string): GhostlineApiError {
  const code = typeof payload === 'object' && payload !== null && 'code' in payload && typeof payload.code === 'string'
    ? payload.code
    : undefined
  return new GhostlineApiError(response.status, apiErrorMessage(response, payload, fallback), code)
}
