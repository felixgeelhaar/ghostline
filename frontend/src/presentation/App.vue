<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { importPullRequest } from '../application/importPullRequest'
import { fetchRevisionChecks } from '../application/fetchRevisionChecks'
import { fetchCheckAnnotations } from '../application/fetchCheckAnnotations'
import { copyRevisionSha, type CopyResult } from '../application/copyRevisionSha'
import { checkOutcome, headCommitLabel, orderChecks, summarizeChecks, type Check, type CheckTone } from '../domain/checks'
import type { CheckAnnotationPage } from '../domain/checkAnnotations'
import { findRecurringFindings, reviewCountSummary, reviewsForRevision } from '../domain/reviews'
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
const selectedTraceId = ref('')
const copyResult = ref<CopyResult>()
const annotationPages = ref<Record<string, CheckAnnotationPage>>({})
const annotationOpen = ref('')
const annotationLoading = ref<Record<string, boolean>>({})
const annotationErrors = ref<Record<string, string>>({})
const annotationPageNumbers = ref<Record<string, number>>({})
let traceTrigger: HTMLButtonElement | undefined
let revisionRequest = 0
let importRequest = 0
let annotationGeneration = 0
const annotationRequests = new Map<string, number>()
const activeChecks = computed(() => selectedRevision.value ? focusedChecks.value : pr.value?.checks)
const orderedChecks = computed(() => orderChecks(activeChecks.value?.availability === 'available' ? activeChecks.value.items : []))
const checksSummary = computed(() => summarizeChecks(orderedChecks.value))
const recurringFindings = computed(() => findRecurringFindings(pr.value?.qodoReviews.items ?? []))
const visibleCommits = computed(() => pr.value?.commits.filter((commit) => !selectedRevision.value || commit.sha === selectedRevision.value) ?? [])
const selectedReviews = computed(() => reviewsForRevision(pr.value?.qodoReviews.items ?? [], selectedRevision.value || undefined))
const reviewCount = computed(() => reviewCountSummary(pr.value?.qodoReviews.items ?? [], selectedRevision.value || undefined))
const activeTrace = computed(() => {
  const finding = recurringFindings.value.find((item) => item.id === selectedTraceId.value)
  return finding ? buildFindingTrace(finding, pr.value?.commits ?? []) : undefined
})
const glyph: Record<CheckTone, string> = { pass: '✓', fail: '✕', running: '●', neutral: '–' }

async function loadPR() {
  const request = ++importRequest
  error.value = validatePullRequestURL(url.value) ?? ''
  if (error.value) {
    loading.value = false
    return
  }
  loading.value = true; pr.value = undefined; selectedRevision.value = ''; selectedTraceId.value = ''; traceTrigger = undefined; focusedChecks.value = undefined; checksError.value = ''; checksLoading.value = false; copyResult.value = undefined; copyRequest++; revisionRequest++; annotationGeneration++; annotationPages.value = {}; annotationOpen.value = ''; annotationLoading.value = {}; annotationErrors.value = {}; annotationRequests.clear()
  try {
    const importedPR = await importPullRequest(url.value)
    if (request === importRequest) pr.value = importedPR
  } catch (e) {
    if (request === importRequest) error.value = e instanceof Error ? e.message : 'Could not load the pull request.'
  } finally {
    if (request === importRequest) loading.value = false
  }
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
    if (generation === annotationGeneration && annotationRequests.get(key) === request && pr.value === importedPR && activeChecks.value?.revisionSha === sha && annotationOpen.value === `${sha}:${check.id}`) {
      annotationPages.value = { ...annotationPages.value, [key]: result }
    }
  } catch (e) {
    if (generation === annotationGeneration && annotationRequests.get(key) === request && pr.value === importedPR && activeChecks.value?.revisionSha === sha && annotationOpen.value === `${sha}:${check.id}`) {
      annotationErrors.value = { ...annotationErrors.value, [key]: e instanceof Error ? e.message : 'Could not load GitHub annotations for this check.' }
    }
  } finally {
    if (generation === annotationGeneration && annotationRequests.get(key) === request) annotationLoading.value = { ...annotationLoading.value, [key]: false }
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
    if (request === revisionRequest && selectedRevision.value === sha) checksError.value = e instanceof Error ? e.message : 'Could not load checks for this revision.'
  } finally {
    if (request === revisionRequest) checksLoading.value = false
  }
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
function date(value: string) { return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
</script>

<template>
  <div class="shell">
    <a class="skip-link" href="#main-content">Skip to main content</a>
    <header class="topbar"><a class="brand" href="#"><span class="brand-mark">G</span><span>GHOSTLINE<small>ENGINEERING CONTROL ROOM</small></span></a><span class="live"><i></i> SYSTEM READY</span></header>
    <main id="main-content" tabindex="-1">
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
          <template v-else><p class="checks-summary" :class="checksSummary.tone" role="status"><span aria-hidden="true">{{ glyph[checksSummary.tone] }}</span>{{ checksSummary.label }}</p><ul class="check-list"><li v-for="(check, i) in orderedChecks" :key="`${check.kind}:${check.name}:${i}`"><span class="check-state" :class="checkOutcome(check).tone"><span aria-hidden="true">{{ glyph[checkOutcome(check).tone] }}</span>{{ checkOutcome(check).label }}</span><div class="check-name"><a v-if="check.url" :href="check.url" target="_blank" rel="noreferrer">{{ check.name }} <span aria-hidden="true">↗</span><span class="sr-only">(opens on GitHub)</span></a><span v-else>{{ check.name }}</span><small>{{ [check.source, check.url ? '' : 'no GitHub link reported'].filter(Boolean).join(' · ') }}</small><button v-if="check.kind === 'check_run' && check.id > 0" type="button" class="annotation-toggle" :aria-expanded="annotationOpen === `${activeChecks?.revisionSha}:${check.id}`" @click="toggleAnnotations(check)">INSPECT GITHUB ANNOTATIONS</button><section v-if="check.kind === 'check_run' && annotationOpen === `${activeChecks?.revisionSha}:${check.id}`" class="annotation-panel" :aria-label="`GitHub annotations for ${check.name} on revision ${activeChecks?.revisionShortSha}`"><p class="annotation-boundary">GitHub check annotations · revision {{ activeChecks?.revisionShortSha }} · <a v-if="check.url" :href="check.url" target="_blank" rel="noreferrer">check run source ↗</a></p><p v-if="annotationLoadingFor(activeChecks?.revisionSha ?? '', check.id)" role="status">Loading annotations from GitHub…</p><p v-else-if="annotationErrorFor(activeChecks?.revisionSha ?? '', check.id)" class="checks-note warn" role="status">{{ annotationErrorFor(activeChecks?.revisionSha ?? '', check.id) }}</p><p v-else-if="annotationsFor(activeChecks?.revisionSha ?? '', check.id)?.items.length === 0" role="status">GitHub reported no line annotations for this check run and revision.</p><ul v-else class="annotation-list"><li v-for="(annotation, j) in annotationsFor(activeChecks?.revisionSha ?? '', check.id)?.items" :key="`${annotation.path}:${annotation.startLine}:${j}`"><strong>{{ annotation.level.toUpperCase() }}<template v-if="annotation.title"> · {{ annotation.title }}</template></strong><p>{{ annotation.message }}</p><details v-if="annotation.details" class="annotation-details"><summary>RAW DETAILS</summary><pre>{{ annotation.details }}</pre></details><small>{{ annotation.path }}:{{ annotation.startLine }}<template v-if="annotation.endLine !== annotation.startLine">–{{ annotation.endLine }}</template></small><a v-if="annotation.sourceUrl" :href="annotation.sourceUrl" target="_blank" rel="noreferrer">VIEW SOURCE LINES ↗</a></li></ul><div v-if="annotationsFor(activeChecks?.revisionSha ?? '', check.id)?.hasMore" class="annotation-pagination"><button type="button" :disabled="annotationLoadingFor(activeChecks?.revisionSha ?? '', check.id)" @click="showAnnotationPage(check, currentAnnotationPage(activeChecks?.revisionSha ?? '', check.id) + 1)">LOAD NEXT PAGE</button></div><div v-if="currentAnnotationPage(activeChecks?.revisionSha ?? '', check.id) > 1" class="annotation-pagination"><button type="button" @click="showAnnotationPage(check, currentAnnotationPage(activeChecks?.revisionSha ?? '', check.id) - 1)">PREVIOUS PAGE</button><span>PAGE {{ currentAnnotationPage(activeChecks?.revisionSha ?? '', check.id) }}</span></div></section></div></li></ul></template>
        </template>
        <p class="footnote">GitHub reported these checks for revision {{ activeChecks?.revisionShortSha || selectedRevision.slice(0, 7) }} at fetch time. Results apply only to that revision. Re-import to refresh PR head checks.</p>
      </section>

      <h3 class="section-title"><span>REVISION TIMELINE <span v-if="selectedRevision">· FOCUSED</span></span><span>COMMIT TIME · OLDEST FIRST</span></h3>
      <button v-if="selectedRevision" class="clear-focus" @click="clearFocus">SHOW FULL HISTORY</button>
      <ol class="timeline"><li v-for="commit in visibleCommits" :key="commit.sha"><span class="node"></span><div class="commit"><div class="commit-meta"><span><a :href="commit.url" target="_blank" rel="noreferrer">{{ commit.shortSha }}</a><span v-if="headCommitLabel(pr.checks, commit.sha)" class="head-tag">{{ headCommitLabel(pr.checks, commit.sha) }}</span></span><time>{{ date(commit.committedAt) }}</time></div><h4>{{ commit.message }}</h4><p>COMMITTED BY {{ commit.author }}</p><div class="commit-actions"><button v-if="selectedRevision !== commit.sha" class="focus-revision" :aria-label="`Focus history on revision ${commit.shortSha}`" @click="focusRevision(commit.sha)">FOCUS THIS REVISION</button><span v-else class="selected-tag">FOCUSED REVISION</span><button type="button" class="copy-sha" :aria-label="`Copy full SHA for revision ${commit.shortSha}`" @click="copySha(commit.sha)">COPY SHA</button></div></div></li></ol>
      <p class="copy-status" :class="{ warn: copyResult && !copyResult.ok }" role="status">{{ copyResult?.message ?? '' }}</p>
      <p class="footnote">Commit timestamps come from Git commit metadata. They do not establish push time or complete pull request event chronology.</p>

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
    </main>
    <footer><span>GHOSTLINE // DAY 01</span><span>EVERY SIGNAL HAS A SOURCE.</span></footer>
  </div>
</template>
