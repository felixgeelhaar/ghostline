<script setup lang="ts">
import { computed, ref } from 'vue'
import { importPullRequest } from '../application/importPullRequest'
import { checkOutcome, headCommitLabel, orderChecks, summarizeChecks, type CheckTone } from '../domain/checks'
import { findRecurringFindings, reviewsForRevision } from '../domain/reviews'
import { validatePullRequestURL, type PullRequest } from '../domain/pullRequest'

const url = ref('')
const pr = ref<PullRequest>()
const error = ref('')
const loading = ref(false)
const selectedRevision = ref('')
const orderedChecks = computed(() => orderChecks(pr.value?.checks.items ?? []))
const checksSummary = computed(() => summarizeChecks(pr.value?.checks.items ?? []))
const recurringFindings = computed(() => findRecurringFindings(pr.value?.qodoReviews.items ?? []))
const visibleCommits = computed(() => pr.value?.commits.filter((commit) => !selectedRevision.value || commit.sha === selectedRevision.value) ?? [])
const selectedReviews = computed(() => reviewsForRevision(pr.value?.qodoReviews.items ?? [], selectedRevision.value || undefined))
const glyph: Record<CheckTone, string> = { pass: '✓', fail: '✕', running: '●', neutral: '–' }

async function loadPR() {
  error.value = validatePullRequestURL(url.value) ?? ''
  if (error.value) return
  loading.value = true; pr.value = undefined; selectedRevision.value = ''
  try { pr.value = await importPullRequest(url.value) } catch (e) { error.value = e instanceof Error ? e.message : 'Could not load the pull request.' }
  finally { loading.value = false }
}
function date(value: string) { return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
</script>

<template>
  <main class="shell">
    <header class="topbar"><a class="brand" href="#"><span class="brand-mark">G</span><span>GHOSTLINE<small>ENGINEERING CONTROL ROOM</small></span></a><span class="live"><i></i> SYSTEM READY</span></header>
    <section class="hero"><p class="eyebrow">CHANGE INTELLIGENCE / 001</p><h1>See the work.<br><em>Trace the decisions.</em></h1><p class="lede">Import a pull request to inspect its revision history. Commit timestamps show when commits were authored, not when they were pushed.</p></section>
    <section class="import-panel"><label for="pr-url">GITHUB PULL REQUEST</label><div class="input-row"><span class="prefix">↗</span><input id="pr-url" v-model="url" placeholder="https://github.com/owner/repo/pull/123" @keyup.enter="loadPR"><button :disabled="loading" @click="loadPR">{{ loading ? 'FETCHING…' : 'IMPORT CHANGE' }} <span>↗</span></button></div><p v-if="error" class="error" role="alert">{{ error }}</p></section>
    <section v-if="pr" class="change-card">
      <div class="change-heading"><div><span class="eyebrow">{{ pr.repository }} / PULL REQUEST #{{ pr.number }}</span><h2>{{ pr.title }}</h2></div><span class="state" :class="pr.state">{{ pr.state }}</span></div>
      <div class="meta"><span>{{ pr.commits.length }} COMMITS</span><span>OPENED {{ date(pr.createdAt) }}</span><a :href="pr.url" target="_blank" rel="noreferrer">VIEW ON GITHUB ↗</a></div>

      <section class="checks" aria-labelledby="checks-title">
        <h3 id="checks-title" class="section-title"><span>CHECKS · LATEST REVISION</span><span v-if="pr.checks.headSha">HEAD <a :href="pr.checks.headUrl" target="_blank" rel="noreferrer" :aria-label="`Head commit ${pr.checks.headShortSha} on GitHub`">{{ pr.checks.headShortSha }} ↗</a></span></h3>
        <p v-if="selectedRevision && selectedRevision !== pr.checks.headSha" class="checks-note" role="status"><strong>Checks shown for PR head only.</strong> Checks for focused revision {{ selectedRevision.slice(0, 7) }} were not loaded.</p>
        <template v-else>
          <p v-if="pr.checks.availability === 'unavailable'" class="checks-note warn" role="status"><strong>Checks unavailable.</strong> {{ pr.checks.reason }}</p>
          <p v-else-if="pr.checks.items.length === 0" class="checks-note" role="status"><strong>No checks reported.</strong> GitHub returned no check runs or commit statuses for head commit {{ pr.checks.headShortSha }}.</p>
          <template v-else><p class="checks-summary" :class="checksSummary.tone" role="status"><span aria-hidden="true">{{ glyph[checksSummary.tone] }}</span>{{ checksSummary.label }}</p><ul class="check-list"><li v-for="(check, i) in orderedChecks" :key="`${check.kind}:${check.name}:${i}`"><span class="check-state" :class="checkOutcome(check).tone"><span aria-hidden="true">{{ glyph[checkOutcome(check).tone] }}</span>{{ checkOutcome(check).label }}</span><span class="check-name"><a v-if="check.url" :href="check.url" target="_blank" rel="noreferrer">{{ check.name }} <span aria-hidden="true">↗</span><span class="sr-only">(opens on GitHub)</span></a><span v-else>{{ check.name }}</span><small>{{ [check.source, check.url ? '' : 'no GitHub link reported'].filter(Boolean).join(' · ') }}</small></span></li></ul></template>
        </template>
        <p class="footnote">Checks are GitHub’s latest results for head commit {{ pr.checks.headShortSha || '(unknown)' }} at import time. Checks on earlier commits are not shown and do not validate this revision. Re-import to refresh.</p>
      </section>

      <h3 class="section-title"><span>REVISION TIMELINE <span v-if="selectedRevision">· FOCUSED</span></span><span>COMMIT TIME · OLDEST FIRST</span></h3>
      <button v-if="selectedRevision" class="clear-focus" @click="selectedRevision = ''">SHOW FULL HISTORY</button>
      <ol class="timeline"><li v-for="commit in visibleCommits" :key="commit.sha"><span class="node"></span><div class="commit"><div class="commit-meta"><span><a :href="commit.url" target="_blank" rel="noreferrer">{{ commit.shortSha }}</a><span v-if="headCommitLabel(pr.checks, commit.sha)" class="head-tag">{{ headCommitLabel(pr.checks, commit.sha) }}</span></span><time>{{ date(commit.committedAt) }}</time></div><h4>{{ commit.message }}</h4><p>COMMITTED BY {{ commit.author }}</p><button v-if="selectedRevision !== commit.sha" class="focus-revision" :aria-label="`Focus history on revision ${commit.shortSha}`" @click="selectedRevision = commit.sha">FOCUS THIS REVISION</button><span v-else class="selected-tag">FOCUSED REVISION</span></div></li></ol>
      <p class="footnote">Commit timestamps come from Git commit metadata. They do not establish push time or complete pull request event chronology.</p>

      <section class="qodo-reviews" aria-labelledby="qodo-reviews-title">
        <h3 id="qodo-reviews-title" class="section-title"><span>QODO REVIEW EVIDENCE <span v-if="selectedRevision">· MATCHED REVISION</span></span><span v-if="pr.qodoReviews.availability === 'available'">{{ selectedReviews.matched.length }} COMMENTS</span></h3>
        <p v-if="pr.qodoReviews.availability === 'unavailable'" class="checks-note warn" role="status"><strong>Qodo review unavailable.</strong> {{ pr.qodoReviews.reason }}</p>
        <p v-else-if="pr.qodoReviews.items.length === 0" class="checks-note" role="status"><strong>No Qodo review recorded.</strong> GitHub has no Qodo review comments for this pull request.</p>
        <p v-else-if="selectedRevision && selectedReviews.matched.length === 0" class="checks-note" role="status"><strong>No Qodo comment is marked for this revision.</strong> This does not mean the revision had no findings.</p>
        <ol v-else class="review-list"><li v-for="review in selectedReviews.matched" :key="review.id"><div class="review-meta"><strong>{{ review.kind === 'findings' ? 'Code review findings' : 'Pull request summary' }}</strong><time>{{ date(review.createdAt) }}</time></div><p v-if="review.summary" class="review-summary">{{ review.summary }}</p><ul v-if="review.findingTitles.length" class="finding-list"><li v-for="(title, index) in review.findingTitles" :key="`${review.id}:${index}`">{{ title }}</li></ul><p v-if="review.commitSha" class="review-revision">REVIEWED REVISION <a :href="review.commitUrl" target="_blank" rel="noreferrer">{{ review.commitSha.slice(0, 7) }} ↗</a></p><p v-else-if="selectedRevision" class="review-revision unmarked">REVISION NOT MARKED · SHOWN FOR CONTEXT</p><a class="review-source" :href="review.url" target="_blank" rel="noreferrer">VIEW ORIGINAL QODO COMMENT ↗</a></li></ol>
        <section v-if="selectedRevision && selectedReviews.unmarked.length" class="unmarked-reviews"><h4 class="section-title"><span>REVISION NOT MARKED · CONTEXT ONLY</span></h4><ol class="review-list"><li v-for="review in selectedReviews.unmarked" :key="review.id"><div class="review-meta"><strong>{{ review.kind === 'findings' ? 'Code review findings' : 'Pull request summary' }}</strong><time>{{ date(review.createdAt) }}</time></div><p v-if="review.summary" class="review-summary">{{ review.summary }}</p><ul v-if="review.findingTitles.length" class="finding-list"><li v-for="(title, index) in review.findingTitles" :key="`${review.id}:${index}`">{{ title }}</li></ul><a class="review-source" :href="review.url" target="_blank" rel="noreferrer">VIEW ORIGINAL QODO COMMENT ↗</a></li></ol></section>
        <section v-if="pr.qodoReviews.availability === 'available'" class="recurrence" aria-labelledby="recurrence-title"><h4 id="recurrence-title" class="section-title"><span>REPEATED FINDINGS · FULL IMPORT</span></h4><p v-if="recurringFindings.length === 0" class="checks-note" role="status">No repeated finding title appears in the Qodo comments returned for this import.</p><ol v-else class="recurrence-list"><li v-for="finding in recurringFindings" :key="finding.title.toLowerCase()"><div class="review-meta"><strong>{{ finding.title }}</strong><span class="recurrence-count">{{ finding.occurrences.length }} separate comments</span></div><ul><li v-for="occurrence in finding.occurrences" :key="occurrence.reviewId"><time>{{ date(occurrence.createdAt) }}</time><a v-if="occurrence.commitSha && occurrence.commitUrl" :href="occurrence.commitUrl" target="_blank" rel="noreferrer">REVISION {{ occurrence.commitSha.slice(0, 7) }} ↗</a><span v-else>REVISION NOT MARKED</span><a :href="occurrence.url" target="_blank" rel="noreferrer">SOURCE COMMENT ↗</a></li></ul></li></ol><p class="footnote">A repeated title is an observed match across separate Qodo comments. It does not establish what caused the repeat or whether a later change fixed it.</p></section>
        <p class="footnote">Review comments are evidence from Qodo at the linked revision. A finding missing from a later review does not by itself prove the change was fixed.</p>
      </section>
    </section>
    <footer><span>GHOSTLINE // DAY 01</span><span>EVERY SIGNAL HAS A SOURCE.</span></footer>
  </main>
</template>
