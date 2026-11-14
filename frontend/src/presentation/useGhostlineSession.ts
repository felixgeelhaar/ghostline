import { ref } from 'vue'
import { fetchGhostlineSession } from '../application/fetchGhostlineSession'
import type { GhostlineSession } from '../domain/auth'

export function useGhostlineSession() {
  const ghostlineSession = ref<GhostlineSession>()
  const sessionLoading = ref(true)
  const sessionError = ref('')
  let latestRequest = 0

  async function refreshGhostlineSession() {
    const request = ++latestRequest
    sessionLoading.value = true
    sessionError.value = ''
    try {
      const session = await fetchGhostlineSession()
      if (request === latestRequest) ghostlineSession.value = session
    } catch (error) {
      if (request === latestRequest) {
        sessionError.value = error instanceof Error ? error.message : 'Could not verify Ghostline sign-in status.'
      }
    } finally {
      if (request === latestRequest) sessionLoading.value = false
    }
  }

  return { ghostlineSession, sessionLoading, sessionError, refreshGhostlineSession }
}
