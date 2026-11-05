<script setup lang="ts">
import { computed, ref } from 'vue'
import { importPullRequest } from '../application/importPullRequest'
import { fetchRevisionChecks } from '../application/fetchRevisionChecks'
import { checkOutcome, headCommitLabel, orderChecks, summarizeChecks, type CheckTone } from '../domain/checks'
import { findRecurringFindings, reviewsForRevision } from '../domain/reviews'
import { validatePullRequestURL, type PullRequest } from '../domain/pullRequest'
import ReviewCard from './ReviewCard.vue'

const url = ref('')
const pr = ref<PullRequest>()
const error = ref('')
const loading = ref(false)
const selectedRevision = ref('')
const focusedChecks = ref<PullRequest['checks']>()
const checksLoading = ref(false)
const checksError = ref('')
let revisionRequest = 0
const activeChecks = computed(() => selectedRevision.value ? focusedChecks.value : pr.value?.checks)
const orderedChecks = computed(() => orderChecks(activeChecks.value?.availability === 'available' ? activeChecks.value.items : []))
const checksSummary = computed(() => summarizeChecks(orderedChecks.value))
const recurringFindings = computed(() => findRecurringFindings(pr.value?.qodoReviews.items ?? []))
const visibleCommits = computed(() => pr.value?.commits.filter((commit) => !selectedRevision.value || commit.sha === selectedRevision.value) ?? [])
const selectedReviews = computed(() => reviewsForRevision(pr.value?.qodoReviews.items ?? [], selectedRevision.value || undefined))
const glyph: Record<CheckTone, string> = { pass: '✓', fail: '✕', running: '●', neutral: '–' }

async function loadPR() {
  error.value = validatePullRequestURL(url.value) ?? ''
  if (error.value) return
  loading.value = true; pr.value = undefined; selectedRevision.value = ''; focusedChecks.value = undefined; checksError.value = ''; checksLoading.value = false; revisionRequest++
  try { pr.value = await importPullRequest(url.value) } catch (e) { error.value = e instanceof Error ? e.message : 'Could not load the pull request.' }
  finally { loading.value = false }
}
async function focusRevision(sha: string) {
  selectedRevision.value = sha
  focusedChecks.value = undefined
  checksError.value = ''
  checksLoading.value = false
  const request = ++revisionRequest
  if (sha === pr.value?.checks.revisionSha) {
    focusedChecks.value = pr.value.checks
    return
  }
  checksLoading.value = true
  try {
    const checks = await fetchRevisionChecks(url.value, sha)
    if (request === revisionRequest && selectedRevision.value === sha) focusedChecks.value = checks
  } catch (e) {
    if (request === revisionRequest && selectedRevision.value === sha) checksError.value = e instanceof Error ? e.message : 'Could not load checks for this revision.'
  } finally {
    if (request === revisionRequest) checksLoading.value = false
  }
}
function clearFocus() {
  revisionRequest++
  selectedRevision.value = ''
  focusedChecks.value = undefined
  checksLoading.value = false
  checksError.value = ''
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
          <template v-else><p class="checks-summary" :class="checksSummary.tone" role="status"><span aria-hidden="true">{{ glyph[checksSummary.tone] }}</span>{{ checksSummary.label }}</p><ul class="check-list"><li v-for="(check, i) in orderedChecks" :key="`${check.kind}:${check.name}:${i}`"><span class="check-state" :class="checkOutcome(check).tone"><span aria-hidden="true">{{ glyph[checkOutcome(check).tone] }}</span>{{ checkOutcome(check).label }}</span><span class="check-name"><a v-if="check.url" :href="check.url" target="_blank" rel="noreferrer">{{ check.name }} <span aria-hidden="true">↗</span><span class="sr-only">(opens on GitHub)</span></a><span v-else>{{ check.name }}</span><small>{{ [check.source, check.url ? '' : 'no GitHub link reported'].filter(Boolean).join(' · ') }}</small></span></li></ul></template>
        </template>
        <p class="footnote">GitHub reported these checks for revision {{ activeChecks?.revisionShortSha || selectedRevision.slice(0, 7) }} at fetch time. Results apply only to that revision. Re-import to refresh PR head checks.</p>
      </section>

      <h3 class="section-title"><span>REVISION TIMELINE <span v-if="selectedRevision">· FOCUSED</span></span><span>COMMIT TIME · OLDEST FIRST</span></h3>
      <button v-if="selectedRevision" class="clear-focus" @click="clearFocus">SHOW FULL HISTORY</button>
      <ol class="timeline"><li v-for="commit in visibleCommits" :key="commit.sha"><span class="node"></span><div class="commit"><div class="commit-meta"><span><a :href="commit.url" target="_blank" rel="noreferrer">{{ commit.shortSha }}</a><span v-if="headCommitLabel(pr.checks, commit.sha)" class="head-tag">{{ headCommitLabel(pr.checks, commit.sha) }}</span></span><time>{{ date(commit.committedAt) }}</time></div><h4>{{ commit.message }}</h4><p>COMMITTED BY {{ commit.author }}</p><button v-if="selectedRevision !== commit.sha" class="focus-revision" :aria-label="`Focus history on revision ${commit.shortSha}`" @click="focusRevision(commit.sha)">FOCUS THIS REVISION</button><span v-else class="selected-tag">FOCUSED REVISION</span></div></li></ol>
      <p class="footnote">Commit timestamps come from Git commit metadata. They do not establish push time or complete pull request event chronology.</p>

      <section class="qodo-reviews" aria-labelledby="qodo-reviews-title">
        <h3 id="qodo-reviews-title" class="section-title"><span>QODO REVIEW EVIDENCE <span v-if="selectedRevision">· MATCHED REVISION</span></span><span v-if="pr.qodoReviews.availability === 'available'">{{ selectedReviews.matched.length }} COMMENTS</span></h3>
        <p v-if="pr.qodoReviews.availability === 'unavailable'" class="checks-note warn" role="status"><strong>Qodo review unavailable.</strong> {{ pr.qodoReviews.reason }}</p>
        <p v-else-if="pr.qodoReviews.items.length === 0" class="checks-note" role="status"><strong>No Qodo review recorded.</strong> GitHub has no Qodo review comments for this pull request.</p>
        <p v-else-if="selectedRevision && selectedReviews.matched.length === 0" class="checks-note" role="status"><strong>No Qodo comment is marked for this revision.</strong> This does not mean the revision had no findings.</p>
        <ol v-else class="review-list"><li v-for="review in selectedReviews.matched" :key="review.id"><ReviewCard :review="review" /></li></ol>
        <section v-if="selectedRevision && selectedReviews.unmarked.length" class="unmarked-reviews"><h4 class="section-title"><span>REVISION NOT MARKED · CONTEXT ONLY</span></h4><ol class="review-list"><li v-for="review in selectedReviews.unmarked" :key="review.id"><ReviewCard :review="review" context-only /></li></ol></section>
        <section v-if="pr.qodoReviews.availability === 'available'" class="recurrence" aria-labelledby="recurrence-title"><h4 id="recurrence-title" class="section-title"><span>REPEATED FINDINGS · FULL IMPORT</span></h4><p v-if="recurringFindings.length === 0" class="checks-note" role="status">No repeated finding title appears in the Qodo comments returned for this import.</p><ol v-else class="recurrence-list"><li v-for="finding in recurringFindings" :key="finding.title.toLowerCase()"><div class="review-meta"><strong>{{ finding.title }}</strong><span class="recurrence-count">{{ finding.occurrences.length }} separate comments</span></div><ul><li v-for="occurrence in finding.occurrences" :key="occurrence.reviewId"><time>{{ date(occurrence.createdAt) }}</time><a v-if="occurrence.commitSha && occurrence.commitUrl" :href="occurrence.commitUrl" target="_blank" rel="noreferrer">REVISION {{ occurrence.commitSha.slice(0, 7) }} ↗</a><span v-else>REVISION NOT MARKED</span><a :href="occurrence.url" target="_blank" rel="noreferrer">SOURCE COMMENT ↗</a></li></ul></li></ol><p class="footnote">A repeated title is an observed match across separate Qodo comments. It does not establish what caused the repeat or whether a later change fixed it.</p></section>
        <p class="footnote">Review comments are evidence from Qodo at the linked revision. A finding missing from a later review does not by itself prove the change was fixed.</p>
      </section>
    </section>
    </main>
    <footer><span>GHOSTLINE // DAY 01</span><span>EVERY SIGNAL HAS A SOURCE.</span></footer>
  </div>
</template>
