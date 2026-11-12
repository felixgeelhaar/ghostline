<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { importPullRequest } from '../application/importPullRequest'
import { fetchRevisionChecks } from '../application/fetchRevisionChecks'
import { fetchStructuredQodoReview } from '../application/fetchStructuredQodoReview'
import { fetchAuthStatus } from '../application/fetchAuthStatus'
import { isAuthenticationRequired } from '../application/apiError'
import { copyRevisionSha, type CopyResult } from '../application/copyRevisionSha'
import type { AuthStatus } from '../domain/auth'
import { buildChangeActivity, countReviewsWithUnavailableDetails, describeCheckActivity } from '../domain/changeActivity'
import { checkOutcome, headCommitLabel, orderChecks, summarizeChecks, type CheckTone } from '../domain/checks'
import { findRecurringFindings, reviewCountSummary, reviewsForRevision } from '../domain/reviews'
import { structuredQodoNextAction, structuredRevisionLabel, type StructuredQodoReviewSet } from '../domain/structuredQodoReviews'
import { buildFindingTrace } from '../domain/trace'
import { validatePullRequestURL, type PullRequest } from '../domain/pullRequest'
import ReviewCard from './ReviewCard.vue'
import DeepTrace from './DeepTrace.vue'

const url = ref('')
const pr = ref<PullRequest>()
const error = ref('')
const loading = ref(false)
const selectedRevision = ref('')
const focusedChecks = ref<PullRequest['checks']>()
const checksLoading = ref(false)
const checksError = ref('')
const structuredQodoKey = ref('')
const structuredQodoReview = ref<StructuredQodoReviewSet>()
const structuredQodoError = ref('')
const structuredQodoLoading = ref(false)
const selectedTraceId = ref('')
const copyResult = ref<CopyResult>()
const authStatus = ref<AuthStatus>()
const authError = ref('')
const authLoading = ref(true)
let traceTrigger: HTMLButtonElement | undefined
let revisionRequest = 0
let importRequest = 0
let structuredQodoRequest = 0
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
const visibleCommits = computed(() => pr.value?.commits.filter((commit) => !selectedRevision.value || commit.sha === selectedRevision.value) ?? [])
const selectedReviews = computed(() => reviewsForRevision(pr.value?.qodoReviews.items ?? [], selectedRevision.value || undefined))
const reviewCount = computed(() => reviewCountSummary(pr.value?.qodoReviews.items ?? [], selectedRevision.value || undefined))
const activeTrace = computed(() => {
  const finding = recurringFindings.value.find((item) => item.id === selectedTraceId.value)
  return finding ? buildFindingTrace(finding, pr.value?.commits ?? []) : undefined
})
const glyph: Record<CheckTone, string> = { pass: '✓', fail: '✕', running: '●', neutral: '–' }

onMounted(async () => {
  try {
    authStatus.value = await fetchAuthStatus()
  } catch (e) {
    authError.value = e instanceof Error ? e.message : 'Could not check Ghostline sign-in status.'
  } finally {
    authLoading.value = false
  }
})

async function loadPR() {
  const request = ++importRequest
  error.value = validatePullRequestURL(url.value) ?? ''
  if (error.value) {
    loading.value = false
    return
  }
  loading.value = true; pr.value = undefined; selectedRevision.value = ''; selectedTraceId.value = ''; traceTrigger = undefined; focusedChecks.value = undefined; checksError.value = ''; checksLoading.value = false; copyResult.value = undefined; structuredQodoKey.value = ''; structuredQodoReview.value = undefined; structuredQodoError.value = ''; structuredQodoLoading.value = false; structuredQodoRequest++; copyRequest++; revisionRequest++
  try {
    const importedPR = await importPullRequest(url.value)
    if (request === importRequest) pr.value = importedPR
  } catch (e) {
    if (request === importRequest && !(isAuthenticationRequired(e) && returnToSignIn())) {
      error.value = e instanceof Error ? e.message : 'Could not load the pull request.'
    }
  } finally {
    if (request === importRequest) loading.value = false
  }
}
async function loadStructuredQodo() {
  const importedPR = pr.value
  const apiKey = structuredQodoKey.value.trim()
  if (!importedPR || !apiKey) return
  const request = ++structuredQodoRequest
  structuredQodoKey.value = ''
  structuredQodoReview.value = undefined
  structuredQodoError.value = ''
  structuredQodoLoading.value = true
  try {
    const review = await fetchStructuredQodoReview(importedPR.url, apiKey)
    if (request === structuredQodoRequest && pr.value === importedPR) structuredQodoReview.value = review
  } catch (e) {
    if (request === structuredQodoRequest && pr.value === importedPR && !(isAuthenticationRequired(e) && returnToSignIn())) {
      structuredQodoError.value = e instanceof Error ? e.message : 'Could not load structured Qodo review data.'
    }
  } finally {
    if (request === structuredQodoRequest) structuredQodoLoading.value = false
  }
}
async function focusRevision(sha: string) {
  const importedPR = pr.value
  if (!importedPR) return
  selectedRevision.value = sha
  focusedChecks.value = undefined
  checksError.value = ''
  checksLoading.value = false
  const request = ++revisionRequest
  if (sha === importedPR.checks.revisionSha) {
    focusedChecks.value = importedPR.checks
    return
  }
  checksLoading.value = true
  try {
    const checks = await fetchRevisionChecks(importedPR.url, sha)
    if (request === revisionRequest && selectedRevision.value === sha) focusedChecks.value = checks
  } catch (e) {
    if (request === revisionRequest && selectedRevision.value === sha && !(isAuthenticationRequired(e) && returnToSignIn())) {
      checksError.value = e instanceof Error ? e.message : 'Could not load checks for this revision.'
    }
  } finally {
    if (request === revisionRequest) checksLoading.value = false
  }
}
function returnToSignIn(): boolean {
  if (!authStatus.value?.enabled) return false
  authStatus.value = { enabled: true, login: null }
  pr.value = undefined
  loading.value = false
  selectedRevision.value = ''
  selectedTraceId.value = ''
  traceTrigger = undefined
  focusedChecks.value = undefined
  checksLoading.value = false
  checksError.value = ''
  copyResult.value = undefined
  structuredQodoKey.value = ''
  structuredQodoReview.value = undefined
  structuredQodoError.value = ''
  structuredQodoLoading.value = false
  error.value = ''
  authError.value = ''
  importRequest++
  revisionRequest++
  structuredQodoRequest++
  copyRequest++
  return true
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
  selectedRevision.value = ''
  focusedChecks.value = undefined
  checksLoading.value = false
  checksError.value = ''
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
function reloadPage() { window.location.reload() }
function date(value: string) { return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
function safeEvidenceURL(value?: string) { if (!value) return undefined; try { const parsed = new URL(value); return parsed.protocol === 'https:' ? parsed.href : undefined } catch { return undefined } }
</script>

<template>
  <div class="shell">
    <a class="skip-link" href="#main-content">Skip to main content</a>
    <header class="topbar"><a class="brand" href="#"><span class="brand-mark">G</span><span>GHOSTLINE<small>ENGINEERING CONTROL ROOM</small></span></a><div class="topbar-status"><form v-if="authStatus?.enabled && authStatus.login" method="post" action="/auth/logout"><span class="signed-in">{{ authStatus.login }}</span><button class="sign-out" type="submit">SIGN OUT</button></form><span class="live"><i></i> SYSTEM READY</span></div></header>
    <main id="main-content" tabindex="-1">
    <section v-if="authLoading" class="auth-panel" role="status"><p class="eyebrow">ACCESS CHECK</p><h1>Verifying your session.</h1><p class="lede">Ghostline is checking whether this instance requires sign-in.</p></section>
    <section v-else-if="authError" class="auth-panel auth-error" role="alert"><p class="eyebrow">ACCESS STATUS UNAVAILABLE</p><h1>Cannot verify access.</h1><p class="lede">{{ authError }}</p><button type="button" @click="reloadPage">RETRY</button></section>
    <section v-else-if="authStatus?.enabled && !authStatus.login" class="auth-panel"><p class="eyebrow">GHOSTLINE / AUTHORIZED ACCESS</p><h1>Sign in to continue.</h1><p class="lede">This hosted instance is limited to approved GitHub accounts. Ghostline reads your public GitHub profile to verify your account; it does not request repository access.</p><a class="github-sign-in" href="/auth/github/login">CONTINUE WITH GITHUB <span aria-hidden="true">↗</span></a></section>
    <div v-else class="workspace">
    <section class="hero"><p class="eyebrow">CHANGE INTELLIGENCE / 001</p><h1>See the work.<br><em>Trace the decisions.</em></h1><p class="lede">Import a pull request to inspect its revision history. Commit timestamps show when commits were authored, not when they were pushed.</p></section>
    <section class="import-panel"><label for="pr-url">GITHUB PULL REQUEST</label><div class="input-row"><span class="prefix">↗</span><input id="pr-url" v-model="url" placeholder="https://github.com/owner/repo/pull/123" @keyup.enter="loadPR"><button :disabled="loading" @click="loadPR">{{ loading ? 'FETCHING…' : 'IMPORT CHANGE' }} <span>↗</span></button></div><p v-if="error" class="error" role="alert">{{ error }}</p></section>
    <section v-if="pr" class="change-card">
      <div class="change-heading"><div><span class="eyebrow">{{ pr.repository }} / PULL REQUEST #{{ pr.number }}</span><h2>{{ pr.title }}</h2></div><span class="state" :class="pr.state">{{ pr.state }}</span></div>
      <div class="meta"><span>{{ pr.commits.length }} COMMITS</span><span>OPENED {{ date(pr.createdAt) }}</span><a :href="pr.url" target="_blank" rel="noreferrer">VIEW ON GITHUB ↗</a></div>

      <section class="checks" aria-labelledby="checks-title">
        <h3 id="checks-title" class="section-title"><span>CHECKS · {{ selectedRevision ? 'FOCUSED REVISION' : 'PR HEAD' }}</span><span v-if="activeChecks?.revisionSha"><a :href="activeChecks.revisionUrl" target="_blank" rel="noreferrer" :aria-label="`Revision ${activeChecks.revisionShortSha} on GitHub`">{{ activeChecks.revisionShortSha }} ↗</a></span></h3>
        <p v-if="checksLoading" class="checks-note" role="status">Loading GitHub checks for revision {{ selectedRevision.slice(0, 7) }}…</p>
        <p v-else-if="checksError" class="checks-note warn" role="status"><strong>Checks unavailable.</strong> {{ checksError }}</p>
        <template v-else-if="activeChecks">
          <p v-if="activeChecks.availability === 'unavailable'" class="checks-note warn" role="status"><strong>Checks unavailable.</strong> {{ activeChecks.reason }}</p>
          <p v-else-if="activeChecks.items.length === 0" class="checks-note" role="status"><strong>No checks reported.</strong> GitHub returned no check runs or commit statuses for revision {{ activeChecks.revisionShortSha }}.</p>
          <template v-else><p class="checks-summary" :class="checksSummary.tone" role="status"><span aria-hidden="true">{{ glyph[checksSummary.tone] }}</span>{{ checksSummary.label }}</p><ul class="check-list"><li v-for="(check, i) in orderedChecks" :key="`${check.kind}:${check.name}:${i}`"><span class="check-state" :class="checkOutcome(check).tone"><span aria-hidden="true">{{ glyph[checkOutcome(check).tone] }}</span>{{ checkOutcome(check).label }}</span><span class="check-name"><a v-if="check.url" :href="check.url" target="_blank" rel="noreferrer">{{ check.name }} <span aria-hidden="true">↗</span><span class="sr-only">(opens on GitHub)</span></a><span v-else>{{ check.name }}</span><small>{{ [check.source, check.url ? '' : 'no GitHub link reported'].filter(Boolean).join(' · ') }}</small></span></li></ul></template>
        </template>
        <p class="footnote">GitHub reported these checks for revision {{ activeChecks?.revisionShortSha || selectedRevision.slice(0, 7) }} at fetch time. Results apply only to that revision. Re-import to refresh PR head checks.</p>
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

      <section class="structured-qodo" aria-labelledby="structured-qodo-title">
        <h3 id="structured-qodo-title" class="section-title"><span>QODO STRUCTURED REVIEW DATA</span><span v-if="structuredQodoReview">{{ structuredQodoReview.findings.length }} FINDINGS · {{ structuredQodoReview.reviewRuns.length }} RUNS</span></h3>
        <p class="footnote">Optional lookup from Qodo Agentic Toolbox. This is separate from the published GitHub comments below. The key is sent to this Ghostline instance and forwarded to Qodo for this request only; it is not stored. Use only with a trusted local or self-hosted instance, not a public multi-user service.</p>
        <form id="structured-qodo-form" class="qodo-key-form" @submit.prevent="loadStructuredQodo">
          <label for="qodo-api-key">PERSONAL QODO API KEY</label>
          <div class="input-row"><input id="qodo-api-key" v-model="structuredQodoKey" type="password" autocomplete="off" spellcheck="false" placeholder="Paste a request-scoped Qodo API key"><button type="submit" :disabled="structuredQodoLoading || !structuredQodoKey.trim()">{{ structuredQodoLoading ? 'FETCHING…' : 'LOAD STRUCTURED REVIEW' }} <span>↗</span></button></div>
        </form>
        <p v-if="structuredQodoLoading" class="checks-note" role="status">Loading Qodo findings and review-run history for {{ pr.repository }}#{{ pr.number }}…</p>
        <p v-else-if="structuredQodoError" class="checks-note warn" role="alert"><strong>Structured Qodo data unavailable.</strong> {{ structuredQodoError }} The published-comment view remains available below.</p>
        <div v-else-if="structuredQodoReview" class="structured-qodo-results">
          <p class="review-provenance">SOURCE: QODO AGENTIC TOOLBOX · CURRENT PR HEAD: {{ structuredQodoReview.currentRevisionSha.slice(0, 7) }}</p>
          <p class="revision-signal"><span>NEXT ACTION</span><strong>{{ structuredQodoNextAction(structuredQodoReview) }}</strong></p>
          <section v-if="structuredQodoReview.reviewSession" class="structured-session" aria-label="Current Qodo review session">
            <h4 class="section-title"><span>REVIEW SESSION · {{ structuredQodoReview.reviewSession.id }}</span><span>{{ structuredQodoReview.reviewSession.status || 'STATUS NOT REPORTED' }}</span></h4>
            <p>Qodo reports {{ structuredQodoReview.reviewSession.reviewComplete === undefined ? 'completion status unknown' : structuredQodoReview.reviewSession.reviewComplete ? 'this review complete' : 'this review incomplete' }}<template v-if="structuredQodoReview.reviewSession.commitSha"> · revision {{ structuredQodoReview.reviewSession.commitSha.slice(0, 7) }} · {{ structuredRevisionLabel(structuredQodoReview.reviewSession.revisionStatus) }}</template>.</p>
          </section>
          <p v-if="structuredQodoReview.findings.length === 0" class="checks-note" role="status"><strong>No findings returned by Qodo.</strong> Use the run evidence and next action above to determine which revision this empty result covers. This is review feedback, not independent verification.</p>
          <ol v-else class="structured-finding-list">
            <li v-for="finding in structuredQodoReview.findings" :key="finding.id">
              <div class="finding-heading"><strong>{{ finding.title }}</strong><span>{{ finding.status || 'STATUS NOT REPORTED' }}</span></div>
              <p class="finding-labels">QODO FINDING {{ finding.id }}<template v-if="finding.category"> · {{ finding.category }}</template><template v-if="finding.actionLevel"> · {{ finding.actionLevel }}</template><template v-if="finding.attributionStatus"> · ATTRIBUTION {{ finding.attributionStatus }}</template></p>
              <p class="structured-revision">{{ structuredRevisionLabel(finding.revisionStatus) }}<template v-if="finding.gitSha"> · {{ finding.gitSha }}</template></p>
              <p v-if="finding.description" class="finding-description">{{ finding.description }}</p>
              <div v-if="finding.reviewRunIds.length" class="structured-run-links"><span>LINKED QODO RUNS</span><code v-for="runId in finding.reviewRunIds" :key="runId">{{ runId }}</code></div>
              <p v-else class="structured-run-links"><span>FINDING-TO-RUN LINK</span><strong>Not reported by Qodo</strong></p>
              <ul v-if="finding.locations.length" class="structured-location-list"><li v-for="(location, index) in finding.locations" :key="`${location.filePath}:${location.startLine}:${index}`"><strong>{{ location.filePath || 'Location not named' }}</strong><span v-if="location.startLine">:{{ location.startLine }}<template v-if="location.endLine && location.endLine !== location.startLine">–{{ location.endLine }}</template></span><pre v-if="location.codeSnippet"><code>{{ location.codeSnippet }}</code></pre></li></ul>
              <ul v-if="finding.citations.length" class="structured-citation-list"><li v-for="(citation, index) in finding.citations" :key="`${citation.source}:${index}`"><span v-if="citation.sourceType">{{ citation.sourceType }} · </span><a v-if="safeEvidenceURL(citation.source)" :href="safeEvidenceURL(citation.source)" target="_blank" rel="noreferrer">{{ citation.source }} ↗</a><span v-else-if="citation.source">{{ citation.source }}</span><span v-if="citation.citation"> — {{ citation.citation }}</span></li></ul>
            </li>
          </ol>
          <section class="structured-runs" aria-labelledby="structured-runs-title"><h4 id="structured-runs-title" class="section-title"><span>QODO REVIEW RUNS</span><span>{{ structuredQodoReview.reviewRuns.length }} RECORDED</span></h4><p v-if="structuredQodoReview.reviewRuns.length === 0" class="checks-note">No review-run history was returned.</p><ol v-else><li v-for="run in structuredQodoReview.reviewRuns" :key="run.id"><strong>{{ run.id }}</strong><span>{{ run.status || 'STATUS NOT REPORTED' }} · {{ structuredRevisionLabel(run.revisionStatus) }}</span><code v-if="run.commitSha">{{ run.commitSha }}</code><time v-if="run.startedAt">{{ run.startedAt }}</time><span>Qodo reports {{ run.reviewComplete === undefined ? 'completion unknown' : run.reviewComplete ? 'complete' : 'incomplete' }}</span></li></ol></section>
          <p class="footnote">Finding status and attribution are Qodo-reported. A later commit, a missing finding in another review, or an earlier green check does not prove a repair or validate this revision.</p>
        </div>
      </section>

      <section class="qodo-reviews" aria-labelledby="qodo-reviews-title">
        <h3 id="qodo-reviews-title" class="section-title"><span>QODO REVIEW EVIDENCE <span v-if="selectedRevision">· MATCHED REVISION</span></span><span v-if="pr.qodoReviews.availability === 'available'">{{ reviewCount }}</span></h3>
        <p v-if="pr.qodoReviews.availability === 'unavailable'" class="checks-note warn" role="status"><strong>Qodo review unavailable.</strong> {{ pr.qodoReviews.reason }}</p>
        <p v-else-if="pr.qodoReviews.items.length === 0 && pr.qodoReviews.unrecognizedCommentCount > 0" class="checks-note warn" role="status"><strong>{{ pr.qodoReviews.unrecognizedCommentCount }} Qodo comment{{ pr.qodoReviews.unrecognizedCommentCount === 1 ? '' : 's' }} not parsed as review output.</strong> Ghostline recognized only the published summary and code-review formats. <a :href="pr.url" target="_blank" rel="noreferrer">Inspect the pull request on GitHub ↗</a></p>
        <p v-else-if="pr.qodoReviews.items.length === 0" class="checks-note" role="status"><strong>No recognized Qodo review comment found.</strong> Ghostline found no published Qodo summary or code-review comment in the GitHub issue-comment feed.</p>
        <p v-else-if="selectedRevision && selectedReviews.matched.length === 0" class="checks-note" role="status"><strong>No Qodo comment is marked for this revision.</strong> This does not mean the revision had no findings.</p>
        <ol v-else class="review-list"><li v-for="review in selectedReviews.matched" :key="review.id"><ReviewCard :review="review" :commits="pr.commits" @focus-revision="focusRevision" /></li></ol>
        <p v-if="pr.qodoReviews.availability === 'available' && pr.qodoReviews.items.length > 0 && pr.qodoReviews.unrecognizedCommentCount > 0" class="checks-note warn" role="status">{{ pr.qodoReviews.unrecognizedCommentCount }} additional Qodo comment{{ pr.qodoReviews.unrecognizedCommentCount === 1 ? '' : 's' }} could not be parsed as review output. <a :href="pr.url" target="_blank" rel="noreferrer">Inspect all comments on GitHub ↗</a></p>
        <section v-if="selectedReviews.unmarked.length" class="unmarked-reviews"><h4 class="section-title"><span>REVISION NOT MARKED · CONTEXT ONLY</span></h4><ol class="review-list"><li v-for="review in selectedReviews.unmarked" :key="review.id"><ReviewCard :review="review" :commits="pr.commits" context-only @focus-revision="focusRevision" /></li></ol></section>
        <section v-if="pr.qodoReviews.availability === 'available'" class="recurrence" aria-labelledby="recurrence-title"><h4 id="recurrence-title" class="section-title"><span>REPEATED FINDINGS · FULL IMPORT</span></h4><p v-if="recurringFindings.length === 0" class="checks-note" role="status">No repeated finding title appears in the Qodo comments returned for this import.</p><ol v-else class="recurrence-list"><li v-for="finding in recurringFindings" :key="finding.id"><div class="review-meta"><strong>{{ finding.title }}</strong><span class="recurrence-count">{{ finding.occurrences.length }} separate comments</span></div><ul><li v-for="occurrence in finding.occurrences" :key="occurrence.reviewId"><time :datetime="occurrence.createdAt">{{ date(occurrence.createdAt) }}</time><a v-if="occurrence.commitSha && occurrence.commitUrl" :href="occurrence.commitUrl" target="_blank" rel="noreferrer">REVISION {{ occurrence.commitSha.slice(0, 7) }} ↗</a><span v-else>REVISION NOT MARKED</span><a :href="occurrence.sourceUrl" target="_blank" rel="noreferrer">SOURCE FINDING ↗</a></li></ul><button type="button" class="trace-open" :aria-expanded="selectedTraceId === finding.id" :aria-controls="selectedTraceId === finding.id ? 'deep-trace' : undefined" @click="toggleTrace(finding.id, $event)">{{ selectedTraceId === finding.id ? 'CLOSE DEEP TRACE' : 'OPEN DEEP TRACE' }}</button></li></ol><DeepTrace v-if="activeTrace" :trace="activeTrace" @close="clearTrace" @focus-revision="focusRevision" /><p class="footnote">A repeated title is an observed match across separate Qodo comments. It does not establish what caused the repeat or whether a later change fixed it.</p></section>
        <p class="footnote">Qodo review outcomes are reviewer feedback, not independent proof of correctness. Findings and summaries apply to their linked revision. A finding missing from a later review does not by itself prove the change was fixed.</p>
      </section>
    </section>
    </div>
    </main>
    <footer><span>GHOSTLINE // DAY 01</span><span>EVERY SIGNAL HAS A SOURCE.</span></footer>
  </div>
</template>
