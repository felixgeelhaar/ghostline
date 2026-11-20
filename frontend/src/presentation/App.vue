<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { importPullRequest } from '../application/importPullRequest'
import { fetchRevisionChecks } from '../application/fetchRevisionChecks'
import { fetchNoxReport } from '../application/fetchNoxReport'
import { createNoxReportLoader } from '../application/noxReportLoader'
import { fetchCheckAnnotations } from '../application/fetchCheckAnnotations'
import { copyRevisionSha, type CopyResult } from '../application/copyRevisionSha'
import { fetchQodoReviewSession } from '../application/fetchQodoReviewSession'
import { buildChangeActivity, countReviewsWithUnavailableDetails, describeCheckActivity } from '../domain/changeActivity'
import { checkOutcome, checkSummaryPreview, headCommitLabel, orderChecks, summarizeChecks, type Check, type CheckTone } from '../domain/checks'
import type { CheckAnnotationPage } from '../domain/checkAnnotations'
import { findRecurringFindings, qodoHeadCoverage, reviewCountSummary, reviewsForRevision } from '../domain/reviews'
import type { QodoStructuredReview } from '../domain/qodoReviewSession'
import type { NoxReport } from '../domain/noxReport'
import { noxReportCanShowZeroFindingNote, noxReportHeadline, noxReportNeedsAttention } from '../domain/noxReport'
import { buildFindingTrace } from '../domain/trace'
import { isSamePullRequest, validatePullRequestURL, type PullRequest } from '../domain/pullRequest'
import { GhostlineApiError } from '../application/apiError'
import ReviewCard from './ReviewCard.vue'
import DeepTrace from './DeepTrace.vue'
import StructuredQodoReview from './StructuredQodoReview.vue'
import { useGhostlineSession } from './useGhostlineSession'

const url = ref('')
const pr = ref<PullRequest>()
const error = ref('')
const showingPreviousSnapshot = ref(false)
const loading = ref(false)
const selectedRevision = ref('')
const focusedChecks = ref<PullRequest['checks']>()
const checksLoading = ref(false)
const checksError = ref('')
const focusedNoxReport = ref<NoxReport>()
const noxLoading = ref(false)
const noxError = ref('')
const selectedTraceId = ref('')
const copyResult = ref<CopyResult>()
const annotationPages = ref<Record<string, CheckAnnotationPage>>({})
const annotationOpen = ref('')
const annotationLoading = ref<Record<string, boolean>>({})
const annotationErrors = ref<Record<string, string>>({})
const annotationPageNumbers = ref<Record<string, number>>({})
const qodoApiKey = ref('')
const qodoStructuredReview = ref<QodoStructuredReview>()
const qodoReviewError = ref('')
const qodoReviewLoading = ref(false)
const qodoKeyTransportSecure = computed(() => globalThis.isSecureContext === true)
const { ghostlineSession, sessionLoading, sessionError, refreshGhostlineSession, handleSessionRejected, endSession } = useGhostlineSession()
let traceTrigger: HTMLButtonElement | undefined
let authChannel: BroadcastChannel | undefined
let revisionRequest = 0
let importRequest = 0
let annotationGeneration = 0
const annotationRequests = new Map<string, number>()
let qodoRequest = 0
const activeChecks = computed(() => selectedRevision.value ? focusedChecks.value : pr.value?.checks)
const orderedChecks = computed(() => orderChecks(activeChecks.value?.availability === 'available' ? activeChecks.value.items : []))
const checksSummary = computed(() => summarizeChecks(orderedChecks.value))
const recurringFindings = computed(() => findRecurringFindings(pr.value?.qodoReviews.items ?? []))
const changeActivity = computed(() => buildChangeActivity(pr.value?.commits ?? [], pr.value?.qodoReviews.items ?? [], activeChecks.value))
const reviewsWithUnavailableDetails = computed(() => countReviewsWithUnavailableDetails(pr.value?.qodoReviews.items ?? []))
const checkActivityNotice = computed(() => describeCheckActivity({
  checks: activeChecks.value,
  events: changeActivity.value,
  loading: checksLoading.value,
  error: checksError.value || undefined,
  revisionSha: selectedRevision.value,
}))
const headCoverage = computed(() => pr.value ? qodoHeadCoverage(pr.value.qodoReviews, pr.value.checks.revisionSha || undefined, pr.value.commits) : undefined)
const visibleCommits = computed(() => pr.value?.commits.filter((commit) => !selectedRevision.value || commit.sha === selectedRevision.value) ?? [])
const selectedReviews = computed(() => reviewsForRevision(pr.value?.qodoReviews.items ?? [], selectedRevision.value || undefined))
const reviewCount = computed(() => reviewCountSummary(pr.value?.qodoReviews.items ?? [], selectedRevision.value || undefined))
const activeTrace = computed(() => {
  const finding = recurringFindings.value.find((item) => item.id === selectedTraceId.value)
  return finding ? buildFindingTrace(finding, pr.value?.commits ?? []) : undefined
})
const glyph: Record<CheckTone, string> = { pass: '✓', fail: '✕', running: '●', neutral: '–' }

onMounted(() => {
  if (typeof BroadcastChannel !== 'undefined') {
    authChannel = new BroadcastChannel('ghostline-session')
    authChannel.onmessage = (event: MessageEvent) => {
      if (event.data?.type !== 'session-changed') return
      clearWorkspace()
      void handleSessionRejected()
    }
  }
  const currentURL = new URL(window.location.href)
  const signInCompleted = currentURL.searchParams.get('ghostline_auth') === 'complete'
  if (signInCompleted) {
    currentURL.searchParams.delete('ghostline_auth')
    window.history.replaceState(window.history.state, '', `${currentURL.pathname}${currentURL.search}${currentURL.hash}`)
    authChannel?.postMessage({ type: 'session-changed' })
  }
  void refreshGhostlineSession()
})

onBeforeUnmount(() => authChannel?.close())

function handleUnauthorized(error: unknown) {
  if (ghostlineSession.value?.authenticated && error instanceof GhostlineApiError && error.code === 'AUTH_REQUIRED') {
    void handleSessionRejected()
  }
}
const noxReportLoader = createNoxReportLoader({
  fetchReport: fetchNoxReport,
  isCurrent: (pullRequestURL, revisionSHA) => {
    const currentPR = pr.value
    return currentPR?.url === pullRequestURL
      && (selectedRevision.value || currentPR.checks.revisionSha) === revisionSHA
  },
  publish: (update) => {
    noxLoading.value = update.loading
    if ('error' in update) noxError.value = update.error ?? ''
    if ('report' in update) focusedNoxReport.value = update.report
  },
  onError: handleUnauthorized,
})

async function logout() {
  try {
    const response = await fetch('/auth/logout', { method: 'POST', credentials: 'same-origin' })
    if (!response.ok) throw new Error()
    clearWorkspace()
    endSession()
    authChannel?.postMessage({ type: 'session-changed' })
  } catch {
    sessionError.value = 'Ghostline could not end this session. Try again.'
  }
}

function clearWorkspace() {
  importRequest++
  revisionRequest++
  qodoRequest++
  noxReportLoader.invalidate()
  copyRequest++
  resetAnnotations()
  url.value = ''
  pr.value = undefined
  error.value = ''
  loading.value = false
  selectedRevision.value = ''
  focusedChecks.value = undefined
  checksLoading.value = false
  checksError.value = ''
  focusedNoxReport.value = undefined
  noxLoading.value = false
  noxError.value = ''
  selectedTraceId.value = ''
  traceTrigger = undefined
  copyResult.value = undefined
  qodoApiKey.value = ''
  qodoStructuredReview.value = undefined
  qodoReviewError.value = ''
  qodoReviewLoading.value = false
}

async function loadPR() {
  const request = ++importRequest
  const requestedURL = url.value
  error.value = validatePullRequestURL(requestedURL) ?? ''
  if (error.value) {
    loading.value = false
    return
  }
  const keepCurrentChange = isSamePullRequest(pr.value, requestedURL)
  loading.value = true
  noxReportLoader.invalidate()
  if (keepCurrentChange) {
    focusedNoxReport.value = undefined
    noxError.value = ''
    noxLoading.value = true
  }
  if (!keepCurrentChange) {
    pr.value = undefined
    selectedRevision.value = ''
    selectedTraceId.value = ''
    traceTrigger = undefined
    focusedChecks.value = undefined
    checksError.value = ''
    checksLoading.value = false
    focusedNoxReport.value = undefined
    noxError.value = ''
    noxLoading.value = false
    copyResult.value = undefined
    qodoStructuredReview.value = undefined
    qodoReviewError.value = ''
  }
  copyRequest++
  resetAnnotations()
  if (!keepCurrentChange) {
    revisionRequest++
  }
  qodoApiKey.value = ''
  if (!keepCurrentChange) {
    qodoRequest++
    qodoReviewLoading.value = false
  }
  try {
    const importedPR = await importPullRequest(requestedURL)
    if (request === importRequest) {
      const focusedRevisionStillExists = importedPR.commits.some((commit) => commit.sha === selectedRevision.value)
      const nextNoxRevision = selectedRevision.value || importedPR.checks.revisionSha
      if (focusedNoxReport.value && focusedNoxReport.value.revisionSha !== nextNoxRevision) {
        noxReportLoader.invalidate()
        focusedNoxReport.value = undefined
        noxError.value = ''
        noxLoading.value = false
      }
      if (selectedRevision.value && !focusedRevisionStillExists) {
        revisionRequest++
        selectedRevision.value = ''
        focusedChecks.value = undefined
        checksError.value = ''
        checksLoading.value = false
        noxReportLoader.invalidate()
        focusedNoxReport.value = undefined
        noxError.value = ''
        noxLoading.value = false
      } else if (selectedRevision.value && selectedRevision.value === importedPR.checks.revisionSha) {
        revisionRequest++
        focusedChecks.value = importedPR.checks
        checksError.value = ''
        checksLoading.value = false
      }
      resetAnnotations()
      pr.value = importedPR
      if (selectedRevision.value) void loadNoxReport(importedPR, selectedRevision.value)
      else if (importedPR.checks.revisionSha) void loadNoxReport(importedPR, importedPR.checks.revisionSha)
      else {
        noxLoading.value = false
        noxError.value = 'GitHub did not provide a pull request revision, so Ghostline cannot load its security report.'
      }
      showingPreviousSnapshot.value = false
    }
  } catch (e) {
    handleUnauthorized(e)
    if (request === importRequest) {
      error.value = e instanceof Error ? e.message : 'Could not load the pull request.'
      showingPreviousSnapshot.value = keepCurrentChange && pr.value !== undefined
      if (keepCurrentChange) {
        // A report requested while the refresh was pending describes an
        // unconfirmed snapshot; it must not replace this warning.
        noxReportLoader.invalidate()
        focusedNoxReport.value = undefined
        noxLoading.value = false
        noxError.value = 'Could not confirm the latest pull request revision; re-import before relying on its security report.'
      }
    }
  } finally {
    if (request === importRequest) loading.value = false
  }
}
// Annotation state is reset when an import starts and again when its result is
// applied: a refresh can return new check runs, and a request made against the
// replaced pull-request object must not leave an open panel without a page.
function resetAnnotations() {
  annotationGeneration++
  annotationRequests.clear()
  annotationPages.value = {}
  annotationOpen.value = ''
  annotationLoading.value = {}
  annotationErrors.value = {}
  annotationPageNumbers.value = {}
}
// A page is cached under its revision, check run and page number, and the panel
// reads only its own key, so a response can never appear under another item.
// It is kept unless a newer import or a newer request for the same key replaced it.
function isLatestAnnotationRequest(key: string, request: number, generation: number) {
  return generation === annotationGeneration && annotationRequests.get(key) === request
}
function annotationKey(sha: string, checkRunId: number, page: number) { return `${sha}:${checkRunId}:${page}` }
function annotationPanelKey(sha: string, checkRunId: number) { return `${sha}:${checkRunId}` }
function currentAnnotationPage(sha: string, checkRunId: number) { return annotationPageNumbers.value[annotationPanelKey(sha, checkRunId)] ?? 1 }
function annotationsFor(sha: string, checkRunId: number) { return annotationPages.value[annotationKey(sha, checkRunId, currentAnnotationPage(sha, checkRunId))] }
function annotationLoadingFor(sha: string, checkRunId: number) { return annotationLoading.value[annotationKey(sha, checkRunId, currentAnnotationPage(sha, checkRunId))] }
function annotationErrorFor(sha: string, checkRunId: number) { return annotationErrors.value[annotationKey(sha, checkRunId, currentAnnotationPage(sha, checkRunId))] }
async function loadAnnotations(check: Check, sha: string, page: number) {
  if (check.kind !== 'check_run') return
  const importedPR = pr.value
  if (!importedPR) return
  const key = annotationKey(sha, check.id, page)
  const generation = annotationGeneration
  const request = (annotationRequests.get(key) ?? 0) + 1
  annotationRequests.set(key, request)
  annotationLoading.value = { ...annotationLoading.value, [key]: true }
  annotationErrors.value = { ...annotationErrors.value, [key]: '' }
  try {
    const result = await fetchCheckAnnotations(importedPR.url, sha, check.id, page)
    if (isLatestAnnotationRequest(key, request, generation)) {
      annotationPages.value = { ...annotationPages.value, [key]: result }
    }
  } catch (e) {
    // A request from a reset workspace can't speak for the current session.
    if (!isLatestAnnotationRequest(key, request, generation)) return
    handleUnauthorized(e)
    annotationErrors.value = { ...annotationErrors.value, [key]: e instanceof Error ? e.message : 'Could not load GitHub annotations for this check.' }
  } finally {
    if (isLatestAnnotationRequest(key, request, generation)) annotationLoading.value = { ...annotationLoading.value, [key]: false }
  }
}
function toggleAnnotations(check: Check) {
  if (check.kind !== 'check_run') return
  const sha = activeChecks.value?.revisionSha
  if (!sha) return
  const panelKey = `${sha}:${check.id}`
  if (annotationOpen.value === panelKey) {
    annotationOpen.value = ''
    return
  }
  annotationOpen.value = panelKey
  annotationPageNumbers.value = { ...annotationPageNumbers.value, [panelKey]: 1 }
  const key = annotationKey(sha, check.id, 1)
  if (!annotationPages.value[key] && !annotationLoading.value[key]) void loadAnnotations(check, sha, 1)
}
function showAnnotationPage(check: Check, page: number) {
  const sha = activeChecks.value?.revisionSha
  if (!sha || check.kind !== 'check_run') return
  annotationPageNumbers.value = { ...annotationPageNumbers.value, [annotationPanelKey(sha, check.id)]: page }
  const key = annotationKey(sha, check.id, page)
  if (annotationPages.value[key]) {
    annotationErrors.value = { ...annotationErrors.value, [key]: '' }
    return
  }
  if (!annotationLoading.value[key]) void loadAnnotations(check, sha, page)
}
async function loadStructuredQodoReview() {
  const importedPR = pr.value
  const apiKey = qodoApiKey.value
  if (!importedPR || !apiKey || !qodoKeyTransportSecure.value) return
  const request = ++qodoRequest
  const requestedPRURL = importedPR.url
  qodoApiKey.value = ''
  qodoReviewError.value = ''
  qodoReviewLoading.value = true
  try {
    const result = await fetchQodoReviewSession(requestedPRURL, apiKey)
    if (request === qodoRequest && pr.value?.url === requestedPRURL) qodoStructuredReview.value = result
  } catch (e) {
    handleUnauthorized(e)
    if (request === qodoRequest && pr.value?.url === requestedPRURL) {
      qodoReviewError.value = e instanceof Error ? e.message : 'Could not load structured Qodo review data.'
    }
  } finally {
    if (request === qodoRequest) qodoReviewLoading.value = false
  }
}
async function focusRevision(sha: string) {
  const importedPR = pr.value
  if (!importedPR) return
  selectedRevision.value = sha
  focusedNoxReport.value = undefined
  noxError.value = ''
  noxLoading.value = false
  focusedChecks.value = undefined
  checksError.value = ''
  checksLoading.value = false
  const request = ++revisionRequest
  void noxReportLoader.loadWhenSettled(importedPR.url, sha)
  if (sha === importedPR.checks.revisionSha) {
    focusedChecks.value = importedPR.checks
    return
  }
  checksLoading.value = true
  try {
    const checks = await fetchRevisionChecks(importedPR.url, sha)
    if (request === revisionRequest && selectedRevision.value === sha) focusedChecks.value = checks
  } catch (e) {
    handleUnauthorized(e)
    if (request === revisionRequest && selectedRevision.value === sha) {
      checksError.value = e instanceof Error ? e.message : 'Could not load checks for this revision.'
    }
  } finally {
    if (request === revisionRequest) checksLoading.value = false
  }
}
function loadNoxReport(importedPR: PullRequest, sha: string) {
  void noxReportLoader.load(importedPR.url, sha)
}
let copyRequest = 0
async function copySha(sha: string) {
  const request = ++copyRequest
  copyResult.value = undefined
  const result = await copyRevisionSha(sha, navigator.clipboard)
  if (request === copyRequest) copyResult.value = result
}
function clearFocus() {
  revisionRequest++
  noxReportLoader.invalidate()
  selectedRevision.value = ''
  focusedChecks.value = undefined
  checksLoading.value = false
  checksError.value = ''
  focusedNoxReport.value = undefined
  noxLoading.value = false
  noxError.value = ''
  if (pr.value?.checks.revisionSha) {
    void loadNoxReport(pr.value, pr.value.checks.revisionSha)
  } else {
    noxError.value = 'GitHub did not provide a pull request revision, so Ghostline cannot load its security report.'
  }
}
async function toggleTrace(id: string, event: MouseEvent) {
  if (selectedTraceId.value === id) {
    selectedTraceId.value = ''
    await nextTick()
    traceTrigger?.focus()
    traceTrigger = undefined
    return
  }
  traceTrigger = event.currentTarget as HTMLButtonElement
  selectedTraceId.value = id
  await nextTick()
  document.getElementById('deep-trace-title')?.focus()
}
async function clearTrace() {
  selectedTraceId.value = ''
  await nextTick()
  traceTrigger?.focus()
  traceTrigger = undefined
}
function date(value: string) { return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
function optionalDate(value?: string) { return value ? date(value) : 'not reported' }
</script>

<template>
  <div class="shell">
    <a class="skip-link" href="#main-content">Skip to main content</a>
    <header class="topbar"><a class="brand" href="#"><span class="brand-mark">G</span><span>GHOSTLINE<small>ENGINEERING CONTROL ROOM</small></span></a><span v-if="ghostlineSession?.authenticated && ghostlineSession.login" class="account"><span>{{ ghostlineSession.login }}</span><button type="button" @click="logout">SIGN OUT</button></span><span v-else class="live"><i></i> SYSTEM READY</span></header>
    <main id="main-content" tabindex="-1">
    <section v-if="sessionLoading && !ghostlineSession" class="auth-message" role="status"><p class="eyebrow">ACCESS CHECK</p><h1>Checking sign-in…</h1></section>
    <section v-else-if="sessionError && !ghostlineSession" class="auth-message" role="alert"><p class="eyebrow">CONNECTION REQUIRED</p><h1>Can’t verify access.</h1><p>{{ sessionError }}</p><button type="button" @click="refreshGhostlineSession">TRY AGAIN</button></section>
    <section v-else-if="ghostlineSession?.authRequired && !ghostlineSession.authenticated" class="auth-message"><p class="eyebrow">GHOSTLINE ACCESS</p><h1>Sign in to your control room.</h1><p>Use the GitHub account invited to this Ghostline instance. Only your account name is kept in the session; the OAuth access token is discarded after sign-in.</p><form action="/auth/login" method="post"><button class="github-sign-in" type="submit">SIGN IN WITH GITHUB <span aria-hidden="true">↗</span></button></form></section>
    <div v-else-if="ghostlineSession && (!ghostlineSession.authRequired || ghostlineSession.authenticated)" class="workspace-content">
    <p v-if="sessionError && ghostlineSession.authenticated" class="session-error" role="alert">{{ sessionError }}</p>
    <section class="hero"><p class="eyebrow">CHANGE INTELLIGENCE / 001</p><h1>See the work.<br><em>Trace the decisions.</em></h1><p class="lede">Import a pull request to inspect its revision history. Commit timestamps show when commits were authored, not when they were pushed.</p></section>
    <section class="import-panel"><label for="pr-url">GITHUB PULL REQUEST</label><div class="input-row"><span class="prefix">↗</span><input id="pr-url" v-model="url" placeholder="https://github.com/owner/repo/pull/123" @keyup.enter="loadPR"><button :disabled="loading" @click="loadPR">{{ loading ? 'FETCHING…' : 'IMPORT CHANGE' }} <span>↗</span></button></div><p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="showingPreviousSnapshot && pr" class="checks-note warn" role="status">GitHub refresh failed. Showing the last imported snapshot for {{ pr.owner }}/{{ pr.repo }}#{{ pr.number }}.</p></section>
    <section v-if="pr" class="change-card">
      <div class="change-heading"><div><span class="eyebrow">{{ pr.repository }} / PULL REQUEST #{{ pr.number }}</span><h2>{{ pr.title }}</h2></div><span class="state" :class="pr.state">{{ pr.state }}</span></div>
      <div class="meta"><span>{{ pr.commits.length }} COMMITS</span><span>OPENED {{ date(pr.createdAt) }}</span><a :href="pr.url" target="_blank" rel="noreferrer">VIEW ON GITHUB ↗</a></div>

      <section v-if="pr.referencedIssues?.items.length || pr.referencedIssues?.availability === 'unavailable'" class="referenced-issues" aria-labelledby="referenced-issues-title">
        <h3 id="referenced-issues-title" class="section-title">REFERENCED ISSUES · GITHUB STATE</h3>
        <p v-if="pr.referencedIssues?.availability === 'unavailable'" class="checks-note warn" role="status">{{ pr.referencedIssues.reason || 'GitHub could not confirm the state of every referenced issue.' }}</p>
        <ul>
          <li v-for="issue in pr.referencedIssues?.items ?? []" :key="issue.number">
            <div v-if="pr.merged && issue.state === 'open'" class="referenced-issue-warning" role="status">
              <strong>Referenced issue #{{ issue.number }} is still open.</strong>
              <div><a :href="pr.url" target="_blank" rel="noreferrer">VIEW MERGED PR #{{ pr.number }} ↗</a><a :href="issue.url" target="_blank" rel="noreferrer">VIEW ISSUE #{{ issue.number }} ↗</a></div>
              <p>GitHub shows the issue as open; Ghostline does not decide whether the work is delivered.</p>
            </div>
            <div v-else class="referenced-issue-row">
              <a :href="issue.url" target="_blank" rel="noreferrer">ISSUE #{{ issue.number }} ↗</a>
              <span>{{ issue.state === 'unavailable' ? 'ISSUE STATE UNAVAILABLE' : issue.state.toUpperCase() }}</span>
            </div>
          </li>
        </ul>
      </section>

      <section class="checks" aria-labelledby="checks-title">
        <h3 id="checks-title" class="section-title"><span>CHECKS · {{ selectedRevision ? 'FOCUSED REVISION' : 'PR HEAD' }}</span><span v-if="activeChecks?.revisionSha"><a :href="activeChecks.revisionUrl" target="_blank" rel="noreferrer" :aria-label="`Revision ${activeChecks.revisionShortSha} on GitHub`">{{ activeChecks.revisionShortSha }} ↗</a></span></h3>
        <p v-if="checksLoading" class="checks-note" role="status">Loading GitHub checks for revision {{ selectedRevision.slice(0, 7) }}…</p>
        <p v-else-if="checksError" class="checks-note warn" role="status"><strong>Checks unavailable.</strong> {{ checksError }}</p>
        <template v-else-if="activeChecks">
          <p v-if="activeChecks.availability === 'unavailable'" class="checks-note warn" role="status"><strong>Checks unavailable.</strong> {{ activeChecks.reason }}</p>
          <p v-else-if="activeChecks.items.length === 0" class="checks-note" role="status"><strong>No checks reported.</strong> GitHub returned no check runs or commit statuses for revision {{ activeChecks.revisionShortSha }}.</p>
          <template v-else><p class="checks-summary" :class="checksSummary.tone" role="status"><span aria-hidden="true">{{ glyph[checksSummary.tone] }}</span>{{ checksSummary.label }}</p><ul class="check-list"><li v-for="(check, i) in orderedChecks" :key="`${check.kind}:${check.name}:${i}`"><span class="check-state" :class="checkOutcome(check).tone"><span aria-hidden="true">{{ glyph[checkOutcome(check).tone] }}</span>{{ checkOutcome(check).label }}</span><div class="check-name"><a v-if="check.url" :href="check.url" target="_blank" rel="noreferrer">{{ check.name }} <span aria-hidden="true">↗</span><span class="sr-only">(opens on GitHub)</span></a><span v-else>{{ check.name }}</span><small>{{ [check.source, check.url ? '' : 'no GitHub link reported'].filter(Boolean).join(' · ') }}</small><small v-if="check.outputTitle || check.outputSummary || check.outputSummaryTruncated" class="check-output"><strong v-if="check.outputTitle">{{ check.outputTitle }}</strong><span v-if="check.outputSummary">{{ checkSummaryPreview(check.outputSummary).text }}</span><span v-if="check.outputSummaryTruncated || checkSummaryPreview(check.outputSummary || '').truncated" class="check-output-note">Preview shortened · open the linked check for full output.</span></small><button v-if="check.kind === 'check_run' && check.id > 0" type="button" class="annotation-toggle" :aria-expanded="annotationOpen === `${activeChecks?.revisionSha}:${check.id}`" @click="toggleAnnotations(check)">INSPECT GITHUB ANNOTATIONS</button><section v-if="check.kind === 'check_run' && annotationOpen === `${activeChecks?.revisionSha}:${check.id}`" class="annotation-panel" :aria-label="`GitHub annotations for ${check.name} on revision ${activeChecks?.revisionShortSha}`"><p class="annotation-boundary">GitHub check annotations · revision {{ activeChecks?.revisionShortSha }} · <a v-if="check.url" :href="check.url" target="_blank" rel="noreferrer">check run source ↗</a></p><p v-if="annotationLoadingFor(activeChecks?.revisionSha ?? '', check.id)" role="status">Loading annotations from GitHub…</p><p v-else-if="annotationErrorFor(activeChecks?.revisionSha ?? '', check.id)" class="checks-note warn" role="status">{{ annotationErrorFor(activeChecks?.revisionSha ?? '', check.id) }}</p><p v-else-if="annotationsFor(activeChecks?.revisionSha ?? '', check.id)?.items.length === 0" role="status">GitHub reported no line annotations for this check run and revision.</p><ul v-else class="annotation-list"><li v-for="(annotation, j) in annotationsFor(activeChecks?.revisionSha ?? '', check.id)?.items" :key="`${annotation.path}:${annotation.startLine}:${j}`"><strong>{{ annotation.level.toUpperCase() }}<template v-if="annotation.title"> · {{ annotation.title }}</template></strong><p>{{ annotation.message }}</p><details v-if="annotation.details" class="annotation-details"><summary>RAW DETAILS</summary><pre>{{ annotation.details }}</pre></details><small>{{ annotation.path }}:{{ annotation.startLine }}<template v-if="annotation.endLine !== annotation.startLine">–{{ annotation.endLine }}</template></small><a v-if="annotation.sourceUrl" :href="annotation.sourceUrl" target="_blank" rel="noreferrer">VIEW SOURCE LINES ↗</a></li></ul><div v-if="annotationsFor(activeChecks?.revisionSha ?? '', check.id)?.hasMore" class="annotation-pagination"><button type="button" :disabled="annotationLoadingFor(activeChecks?.revisionSha ?? '', check.id)" @click="showAnnotationPage(check, currentAnnotationPage(activeChecks?.revisionSha ?? '', check.id) + 1)">LOAD NEXT PAGE</button></div><div v-if="currentAnnotationPage(activeChecks?.revisionSha ?? '', check.id) > 1" class="annotation-pagination"><button type="button" @click="showAnnotationPage(check, currentAnnotationPage(activeChecks?.revisionSha ?? '', check.id) - 1)">PREVIOUS PAGE</button><span>PAGE {{ currentAnnotationPage(activeChecks?.revisionSha ?? '', check.id) }}</span></div></section></div></li></ul></template>
        </template>
        <p class="footnote">GitHub reported these checks for revision {{ activeChecks?.revisionShortSha || selectedRevision.slice(0, 7) }} at fetch time. Results apply only to that revision. Re-import to refresh PR head checks.</p>
      </section>

      <section class="nox-report" aria-labelledby="nox-report-title">
        <h3 id="nox-report-title" class="section-title"><span>SECURITY SCAN · {{ selectedRevision ? 'FOCUSED REVISION' : 'PR HEAD' }}</span><span v-if="focusedNoxReport?.revisionSha"><a :href="`https://github.com/${pr.owner}/${pr.repo}/commit/${focusedNoxReport.revisionSha}`" target="_blank" rel="noreferrer" :aria-label="`Scanned revision ${focusedNoxReport.revisionSha.slice(0, 7)} on GitHub`">{{ focusedNoxReport.revisionSha.slice(0, 7) }} ↗</a></span></h3>
        <p v-if="noxLoading" class="checks-note" role="status">Loading the published security report for revision {{ (selectedRevision || pr.checks.revisionSha).slice(0, 7) }}…</p>
        <p v-else-if="noxError" class="checks-note warn" role="status"><strong>Security report unavailable.</strong> {{ noxError }}</p>
        <template v-else-if="focusedNoxReport?.availability === 'unavailable'">
          <p class="checks-note warn" role="status"><strong>Security report unavailable for revision {{ focusedNoxReport.revisionSha.slice(0, 7) || 'unknown' }}.</strong> {{ focusedNoxReport.reason }}</p>
          <p v-if="focusedNoxReport.runUrl" class="checks-note"><a :href="focusedNoxReport.runUrl" target="_blank" rel="noreferrer">VIEW SOURCE WORKFLOW RUN ↗</a></p>
        </template>
        <template v-else-if="focusedNoxReport?.availability === 'available'">
          <p class="nox-summary" :class="{ warn: noxReportNeedsAttention(focusedNoxReport) }" role="status">
            <strong>{{ noxReportHeadline(focusedNoxReport) }}</strong>
            <span>Workflow {{ focusedNoxReport.runConclusion || focusedNoxReport.runStatus?.replaceAll('_', ' ') || 'status unavailable' }} · {{ focusedNoxReport.runName || 'Nox scan' }} · report from {{ optionalDate(focusedNoxReport.createdAt) }} · retained until {{ optionalDate(focusedNoxReport.expiresAt) }}</span>
          </p>
          <p v-if="noxReportCanShowZeroFindingNote(focusedNoxReport)" class="checks-note">This is the published Nox report for this exact revision. It is not an independent guarantee that the code is secure.</p>
          <ol v-else class="nox-findings"><li v-for="(finding, index) in focusedNoxReport.findings" :key="`${finding.fingerprint || finding.ruleId}:${index}`" :class="{ suppressed: finding.suppressed }">
            <div class="nox-finding-heading"><strong>{{ finding.title }}</strong><span v-if="finding.suppressed" class="suppression-label">SUPPRESSED BY SOURCE</span><span v-else :class="`severity-${finding.severity}`">{{ finding.severity }}</span></div>
            <p>{{ finding.message }}</p>
            <p v-if="finding.path" class="nox-location">{{ finding.path }}<span v-if="finding.line">:{{ finding.line }}</span></p>
            <p v-if="finding.suppressed" class="nox-suppression">The source marks this result as suppressed; that does not mean it was fixed or verified.</p>
            <p v-if="finding.suppressionKind || finding.suppressionJustification" class="nox-suppression">Source detail<span v-if="finding.suppressionKind"> · {{ finding.suppressionKind }}</span><span v-if="finding.suppressionJustification"> · {{ finding.suppressionJustification }}</span></p>
            <ul v-if="finding.suppressions.length" class="nox-suppression-details"><li v-for="(suppression, suppressionIndex) in finding.suppressions" :key="suppressionIndex">Suppression status: {{ suppression.status || 'unspecified' }}<span v-if="suppression.kind"> · {{ suppression.kind }}</span><span v-if="suppression.justification"> · {{ suppression.justification }}</span></li></ul>
            <p v-if="finding.suppressionsTruncated" class="checks-note warn">Some suppression records were omitted at the report parsing limit.</p>
          </li></ol>
          <p v-if="focusedNoxReport.truncated" class="checks-note warn" role="status">Showing {{ focusedNoxReport.findings.length }} of {{ focusedNoxReport.totalCount }} report results ({{ focusedNoxReport.activeCount }} active, {{ focusedNoxReport.suppressedCount }} suppressed). Open the source workflow for the full report.</p>
          <p class="nox-source"><a v-if="focusedNoxReport.runUrl" :href="focusedNoxReport.runUrl" target="_blank" rel="noreferrer">OPEN SOURCE WORKFLOW RUN ↗</a><span v-else>Source workflow link not reported</span><span> · Report and findings apply to revision {{ focusedNoxReport.revisionSha.slice(0, 7) }}.</span></p>
        </template>
      </section>

      <section class="activity-index" aria-labelledby="activity-index-title">
        <h3 id="activity-index-title" class="section-title"><span>CHANGE ACTIVITY · OBSERVED SOURCES</span><span>{{ changeActivity.length }} EVENTS</span></h3>
        <p class="footnote">Events are ordered by their source timestamps. Check entries are GitHub’s latest records for the focused revision at fetch time, not a complete run history. Commit, check, and Qodo timestamps describe different source fields; sequence alone does not establish causation.</p>
        <p v-if="pr.qodoReviews.availability === 'unavailable'" class="checks-note warn" role="status"><strong>Qodo activity unavailable.</strong> {{ pr.qodoReviews.reason }} This index may omit Qodo review events.</p>
        <p v-else-if="pr.qodoReviews.unrecognizedCommentCount > 0" class="checks-note warn" role="status"><strong>{{ pr.qodoReviews.unrecognizedCommentCount }} additional Qodo comments could not be parsed.</strong> The activity index may omit review events. <a :href="pr.url" target="_blank" rel="noreferrer">Inspect all comments on GitHub ↗</a></p>
        <p v-if="pr.qodoReviews.availability === 'available' && reviewsWithUnavailableDetails > 0" class="checks-note warn" role="status"><strong>Finding details unavailable or incomplete for {{ reviewsWithUnavailableDetails }} recognized Qodo review comment{{ reviewsWithUnavailableDetails === 1 ? '' : 's' }}.</strong> The activity index may omit finding details. <a :href="pr.url" target="_blank" rel="noreferrer">Inspect the source review on GitHub ↗</a></p>
        <p v-if="checkActivityNotice" class="checks-note" :class="{ warn: checkActivityNotice.kind === 'warn' }" role="status">{{ checkActivityNotice.text }}</p>
        <ol class="activity-list">
          <li v-for="event in changeActivity" :key="event.id" :class="`activity-${event.kind}`">
            <span class="activity-node" aria-hidden="true"></span>
            <div class="activity-content">
              <div class="activity-meta"><span>{{ event.timestampLabel }}</span><time :datetime="event.timestamp">{{ date(event.timestamp) }}</time></div>
              <strong>{{ event.title }}</strong>
              <p>{{ event.detail }} · {{ event.revisionLabel }}</p>
              <div class="activity-links">
                <a :href="event.sourceUrl" target="_blank" rel="noreferrer">{{ event.sourceLabel }} ↗</a>
                <a v-if="event.markedRevision && !event.revision && event.markedRevision.url" :href="event.markedRevision.url" target="_blank" rel="noreferrer">VIEW MARKED REVISION {{ event.markedRevision.shortSha }} ↗</a>
                <button v-if="event.revision" type="button" :aria-label="`Focus activity revision ${event.revision.shortSha}`" @click="focusRevision(event.revision.sha)">FOCUS REVISION</button>
              </div>
            </div>
          </li>
        </ol>
      </section>

      <h3 class="section-title"><span>REVISION TIMELINE <span v-if="selectedRevision">· FOCUSED</span></span><span>COMMIT TIME · OLDEST FIRST</span></h3>
      <button v-if="selectedRevision" class="clear-focus" @click="clearFocus">SHOW FULL HISTORY</button>
      <ol class="timeline"><li v-for="commit in visibleCommits" :key="commit.sha"><span class="node"></span><div class="commit"><div class="commit-meta"><span><a :href="commit.url" target="_blank" rel="noreferrer">{{ commit.shortSha }}</a><span v-if="headCommitLabel(pr.checks, commit.sha)" class="head-tag">{{ headCommitLabel(pr.checks, commit.sha) }}</span></span><time>{{ date(commit.committedAt) }}</time></div><h4>{{ commit.message }}</h4><p>COMMITTED BY {{ commit.author }}</p><div class="commit-actions"><button v-if="selectedRevision !== commit.sha" class="focus-revision" :aria-label="`Focus history on revision ${commit.shortSha}`" @click="focusRevision(commit.sha)">FOCUS THIS REVISION</button><span v-else class="selected-tag">FOCUSED REVISION</span><button type="button" class="copy-sha" :aria-label="`Copy full SHA for revision ${commit.shortSha}`" @click="copySha(commit.sha)">COPY SHA</button></div></div></li></ol>
      <p class="copy-status" :class="{ warn: copyResult && !copyResult.ok }" role="status">{{ copyResult?.message ?? '' }}</p>
      <p class="footnote">Commit timestamps come from Git commit metadata. They do not establish push time or complete pull request event chronology.</p>

      <section class="qodo-reviews" aria-labelledby="qodo-reviews-title">
        <h3 id="qodo-reviews-title" class="section-title"><span>QODO REVIEW EVIDENCE <span v-if="selectedRevision">· MATCHED REVISION</span></span><span v-if="pr.qodoReviews.availability === 'available'">{{ reviewCount }}</span></h3>
        <div v-if="headCoverage" class="review-coverage" :class="headCoverage.state" role="status" aria-live="polite">
          <strong v-if="headCoverage.state === 'outside_history'">PUBLISHED COMMENT · MARKER OUTSIDE IMPORTED HISTORY</strong>
          <strong v-else-if="headCoverage.state === 'current' || headCoverage.state === 'stale'">PUBLISHED COMMENT · PR HEAD {{ headCoverage.state.toUpperCase() }}</strong>
          <strong v-else>QODO REVIEW COVERAGE · {{ headCoverage.state.toUpperCase() }}</strong>
          <p>{{ headCoverage.detail }}</p>
          <p v-if="headCoverage.state === 'current' || headCoverage.state === 'stale' || headCoverage.state === 'outside_history'" class="review-coverage-sources">
            <a v-if="headCoverage.revisionUrl" :href="headCoverage.revisionUrl" target="_blank" rel="noreferrer">MARKED REVISION {{ headCoverage.revisionSha.slice(0, 7) }} ↗</a>
            <a :href="headCoverage.sourceUrl" target="_blank" rel="noreferrer">SOURCE COMMENT ↗</a>
          </p>
          <p v-if="headCoverage.state === 'current' || headCoverage.state === 'stale' || headCoverage.state === 'outside_history'" class="review-coverage-caveat">A comment matched to the PR head does not establish the latest complete Qodo review.</p>
          <p v-if="headCoverage.state === 'stale'" class="review-coverage-next">Inspect the current PR head in Qodo before treating these comments as feedback on the version you intend to ship.</p>
          <p v-else-if="headCoverage.state === 'unknown'" class="review-coverage-next">Open the source comments and confirm which revision Qodo reviewed.</p>
        </div>
        <p v-if="pr.qodoReviews.availability === 'unavailable'" class="checks-note warn" role="status"><strong>Qodo review unavailable.</strong> {{ pr.qodoReviews.reason }}</p>
        <p v-else-if="pr.qodoReviews.items.length === 0 && pr.qodoReviews.unrecognizedCommentCount > 0" class="checks-note warn" role="status"><strong>{{ pr.qodoReviews.unrecognizedCommentCount }} Qodo comment{{ pr.qodoReviews.unrecognizedCommentCount === 1 ? '' : 's' }} not parsed as review output.</strong> Ghostline recognized only the published summary and code-review formats. <a :href="pr.url" target="_blank" rel="noreferrer">Inspect the pull request on GitHub ↗</a></p>
        <p v-else-if="pr.qodoReviews.items.length === 0" class="checks-note" role="status"><strong>No recognized Qodo review comment found.</strong> Ghostline found no published Qodo summary or code-review comment in the GitHub issue-comment feed.</p>
        <p v-else-if="selectedRevision && selectedReviews.matched.length === 0" class="checks-note" role="status"><strong>No Qodo comment is marked for this revision.</strong> This does not mean the revision had no findings.</p>
        <ol v-else class="review-list"><li v-for="review in selectedReviews.matched" :key="review.id"><ReviewCard :review="review" :commits="pr.commits" @focus-revision="focusRevision" /></li></ol>
        <p v-if="pr.qodoReviews.availability === 'available' && pr.qodoReviews.items.length > 0 && pr.qodoReviews.unrecognizedCommentCount > 0" class="checks-note warn" role="status">{{ pr.qodoReviews.unrecognizedCommentCount }} additional Qodo comment{{ pr.qodoReviews.unrecognizedCommentCount === 1 ? '' : 's' }} could not be parsed as review output. <a :href="pr.url" target="_blank" rel="noreferrer">Inspect all comments on GitHub ↗</a></p>
        <section v-if="selectedReviews.unmarked.length" class="unmarked-reviews"><h4 class="section-title"><span>REVISION NOT MARKED · CONTEXT ONLY</span></h4><ol class="review-list"><li v-for="review in selectedReviews.unmarked" :key="review.id"><ReviewCard :review="review" :commits="pr.commits" context-only @focus-revision="focusRevision" /></li></ol></section>
        <section v-if="pr.qodoReviews.availability === 'available'" class="recurrence" aria-labelledby="recurrence-title"><h4 id="recurrence-title" class="section-title"><span>POSSIBLE REPEATS · FULL IMPORT</span></h4><p v-if="recurringFindings.length === 0" class="checks-note" role="status">No candidate repeat across distinct marked revisions was found. Same-revision or unmarked title matches are not shown as possible repeats.</p><ol v-else class="recurrence-list"><li v-for="finding in recurringFindings" :key="finding.id"><div class="review-meta"><strong>{{ finding.title }}</strong><span class="recurrence-count">{{ finding.occurrences.length }} distinct revisions</span></div><ul><li v-for="occurrence in finding.occurrences" :key="occurrence.findingId"><time :datetime="occurrence.createdAt">QODO COMMENT {{ occurrence.reviewId }} · {{ date(occurrence.createdAt) }}</time><a v-if="occurrence.commitSha && occurrence.commitUrl" :href="occurrence.commitUrl" target="_blank" rel="noreferrer">REVISION {{ occurrence.commitSha.slice(0, 7) }} ↗</a><span v-else>REVISION NOT MARKED</span><a :href="occurrence.sourceUrl" target="_blank" rel="noreferrer">SOURCE FINDING ↗</a></li></ul><button type="button" class="trace-open" :aria-expanded="selectedTraceId === finding.id" :aria-controls="selectedTraceId === finding.id ? 'deep-trace' : undefined" @click="toggleTrace(finding.id, $event)">{{ selectedTraceId === finding.id ? 'CLOSE TRACE' : 'COMPARE SOURCE EVIDENCE' }}</button></li></ol><DeepTrace v-if="activeTrace" :trace="activeTrace" @close="clearTrace" @focus-revision="focusRevision" /><p class="footnote">A normalized title match is a discovery signal, not proof that separate comments describe the same defect, that a repair was attempted, or that a later change fixed it.</p></section>
        <p class="footnote">Qodo review outcomes are reviewer feedback, not independent proof of correctness. Findings and summaries apply to their linked revision. A finding missing from a later review does not by itself prove the change was fixed.</p>
        <form class="qodo-connect" @submit.prevent="loadStructuredQodoReview">
          <label for="qodo-api-key">OPTIONAL · LOAD STRUCTURED QODO REVIEW</label>
          <div class="qodo-connect-row"><input id="qodo-api-key" v-model="qodoApiKey" type="password" autocomplete="off" spellcheck="false" placeholder="Personal Qodo API key" :disabled="!qodoKeyTransportSecure"><button id="load-qodo-review" type="submit" :disabled="!qodoApiKey || !qodoKeyTransportSecure">{{ qodoReviewLoading ? 'LOADING STRUCTURED REVIEW' : qodoStructuredReview ? 'REFRESH STRUCTURED REVIEW' : 'LOAD STRUCTURED REVIEW' }} <span>↗</span></button></div>
          <p v-if="!qodoKeyTransportSecure" class="checks-note warn" role="status"><strong>Secure connection required.</strong> Open Ghostline over HTTPS before sending a Qodo key. Localhost is also allowed.</p>
          <p v-else class="footnote">The key is used for this request only, sent in a header, and never saved by Ghostline. Use only a Ghostline instance you trust.</p>
          <p v-if="qodoReviewLoading" class="checks-note" role="status">Loading structured review data for {{ pr.url }}…</p>
          <p v-if="qodoReviewError" class="checks-note warn" role="alert">{{ qodoReviewError }}</p>
        </form>
        <StructuredQodoReview v-if="qodoStructuredReview && pr" :review="qodoStructuredReview" :pull-request-url="pr.url" :head-sha="pr.checks.revisionSha" />
      </section>
    </section>
    </div>
    </main>
    <footer><span>GHOSTLINE // DAY 01</span><span>EVERY SIGNAL HAS A SOURCE.</span></footer>
  </div>
</template>

<style scoped>
.nox-summary {
  display: grid;
  gap: 6px;
  padding: 12px 14px;
  border: 1px solid rgba(0, 229, 255, .28);
  color: #d2eaf2;
  background: rgba(0, 229, 255, .035);
}

.nox-summary.warn { border-color: rgba(252, 238, 10, .35); }
.nox-summary span, .nox-location, .nox-source { color: #8a91a7; font-size: .82rem; }
.nox-findings { list-style: none; padding: 0; margin: 12px 0; display: grid; gap: 10px; }
.nox-findings li { padding: 12px 14px; border: 1px solid rgba(255, 43, 214, .2); background: rgba(9, 11, 22, .55); }
.nox-findings li.suppressed { border-color: rgba(252, 238, 10, .28); }
.nox-finding-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.nox-finding-heading > span { font: 600 .68rem/1.2 ui-monospace, monospace; text-transform: uppercase; color: #fcee0a; }
.nox-finding-heading .severity-critical, .nox-finding-heading .severity-high { color: #ff708d; }
.nox-findings p { margin: 8px 0 0; line-height: 1.5; }
.nox-findings .nox-location { font-family: ui-monospace, monospace; overflow-wrap: anywhere; }
.nox-findings .nox-suppression { color: #c5c6d5; font-size: .88rem; }
.nox-finding-heading .suppression-label { color: #fcee0a; }
.nox-source { margin-top: 12px; }
.nox-report a { color: #00e5ff; text-decoration-color: rgba(0, 229, 255, .55); text-underline-offset: .2em; }
.nox-report a:focus-visible { outline: 2px solid #00e5ff; outline-offset: 3px; }

.check-name .check-output {
  max-width: 760px;
  margin-top: 8px;
  line-height: 1.55;
  letter-spacing: .015em;
  overflow-wrap: anywhere;
}

.check-output strong {
  display: block;
  color: #c6cbdb;
  font-weight: 600;
  margin-bottom: 3px;
}

.check-output span {
  display: block;
  color: #8a91a7;
}

.check-output .check-output-note {
  margin-top: 4px;
  color: #c6cbdb;
  font-style: italic;
}
</style>
