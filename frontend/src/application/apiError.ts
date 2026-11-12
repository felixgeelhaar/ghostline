export class ApiResponseError extends Error {
  constructor(message: string, readonly status: number) {
    super(message)
    this.name = 'ApiResponseError'
  }
}

export function isAuthenticationRequired(error: unknown): error is ApiResponseError {
  return error instanceof ApiResponseError && error.status === 401
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
