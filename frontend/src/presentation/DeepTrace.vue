<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { compareRevisions } from '../application/compareRevisions'
import type { RevisionComparison } from '../domain/comparison'
import { interventionPrompt, type FindingTrace } from '../domain/trace'

const props = defineProps<{ trace: FindingTrace; pullRequestUrl: string }>()
const emit = defineEmits<{
  close: []
  focusRevision: [sha: string]
}>()

const copyStatus = ref('')
const heading = ref<HTMLHeadingElement | null>(null)
const comparison = ref<RevisionComparison>()
const comparisonLoading = ref(false)
const comparisonError = ref('')
const comparisonTarget = ref<{ baseSha: string; headSha: string }>()
const comparisonRequest = ref(0)

onMounted(() => heading.value?.focus())
watch(() => `${props.trace.id}:${props.pullRequestUrl}:${props.trace.occurrences.map((item) => item.commitSha ?? '').join(',')}`, () => {
  copyStatus.value = ''
  comparisonRequest.value++
  comparison.value = undefined
  comparisonTarget.value = undefined
  comparisonLoading.value = false
  comparisonError.value = ''
})

function date(value: string) {
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

async function copyGuidance(trace: FindingTrace) {
  try {
    if (!navigator.clipboard?.writeText) throw new Error('Clipboard access is unavailable.')
    await navigator.clipboard.writeText(interventionPrompt(trace))
    copyStatus.value = 'Guidance copied. Review it, then paste it into your existing IDE or coding agent.'
  } catch {
    copyStatus.value = 'Could not copy guidance here. Use the source links and summary above in your existing IDE or coding agent.'
  }
}

async function inspectCandidate(baseSha: string, headSha: string) {
  const request = ++comparisonRequest.value
  comparisonTarget.value = { baseSha, headSha }
  comparison.value = undefined
  comparisonError.value = ''
  comparisonLoading.value = true
  try {
    const result = await compareRevisions(props.pullRequestUrl, baseSha, headSha)
    if (request === comparisonRequest.value) comparison.value = result
  } catch (error) {
    if (request === comparisonRequest.value) comparisonError.value = error instanceof Error ? error.message : 'Could not compare these revisions.'
  } finally {
    if (request === comparisonRequest.value) comparisonLoading.value = false
  }
}
</script>

<template>
  <section id="deep-trace" class="deep-trace" aria-labelledby="deep-trace-title">
    <header class="trace-heading">
      <div>
        <p class="eyebrow">DEEP TRACE / NORMALIZED QODO TITLE MATCH</p>
        <h4 id="deep-trace-title" ref="heading" tabindex="-1">{{ trace.title }}</h4>
      </div>
      <button type="button" class="trace-close" aria-label="Close Deep Trace" @click="emit('close')">CLOSE TRACE</button>
    </header>

    <section class="intervention-summary" aria-labelledby="intervention-title">
      <h5 id="intervention-title">INTERVENTION SUMMARY</h5>
      <section class="revision-signal" aria-labelledby="revision-signal-title">
        <span id="revision-signal-title">REVISION SIGNAL</span>
        <strong>{{ trace.summary.revisionSignal.label }}</strong>
        <p>{{ trace.summary.revisionSignal.detail }}</p>
      </section>
      <p><strong>{{ trace.summary.evidence }}</strong></p>
      <p>{{ trace.summary.revisionCoverage }}</p>
      <p>{{ trace.summary.nextStep }}</p>
      <div class="trace-actions">
        <button type="button" class="trace-copy" @click="copyGuidance(trace)">COPY HANDOFF CONTEXT</button>
        <span role="status" aria-live="polite">{{ copyStatus }}</span>
      </div>
    </section>

    <h5 class="trace-events-title">SOURCE EVENTS · QODO COMMENT TIME</h5>
    <ol class="trace-events">
      <li v-for="occurrence in trace.occurrences" :key="occurrence.findingId">
        <div class="trace-event-meta">
          <span>COMMENT #{{ occurrence.reviewId }}</span>
          <time :datetime="occurrence.createdAt">{{ date(occurrence.createdAt) }}</time>
        </div>
        <p v-if="[...occurrence.categories, occurrence.lifecycle, occurrence.severity, occurrence.actionLevel].some(Boolean)" class="finding-labels">{{ [...occurrence.categories, occurrence.lifecycle, occurrence.severity, occurrence.actionLevel].filter(Boolean).join(' · ') }}</p>
        <p v-if="occurrence.description" class="finding-description">{{ occurrence.description }}</p>
        <p v-if="occurrence.commitSha && occurrence.revisionInPullRequest" class="review-revision">REVIEWED PR REVISION <a v-if="occurrence.commitUrl" :href="occurrence.commitUrl" target="_blank" rel="noreferrer">{{ occurrence.commitSha.slice(0, 7) }} ↗</a><span v-else>{{ occurrence.commitSha.slice(0, 7) }}</span></p>
        <p v-else-if="occurrence.commitSha" class="review-revision unmarked">REVISION MARKED · NOT IN IMPORTED PR COMMIT LIST <a v-if="occurrence.commitUrl" :href="occurrence.commitUrl" target="_blank" rel="noreferrer">{{ occurrence.commitSha.slice(0, 7) }} ↗</a></p>
        <p v-else class="review-revision unmarked">REVISION NOT MARKED</p>
        <div class="trace-links">
          <a class="review-source" :href="occurrence.sourceUrl" target="_blank" rel="noreferrer">SOURCE FINDING ↗</a>
          <a v-if="occurrence.url !== occurrence.sourceUrl" class="review-source" :href="occurrence.url" target="_blank" rel="noreferrer">QODO COMMENT ↗</a>
          <button v-if="occurrence.revision" type="button" class="focus-revision" :aria-label="`Focus PR revision ${occurrence.revision.shortSha}`" @click="emit('focusRevision', occurrence.revision.sha)">FOCUS REVISION</button>
        </div>
      </li>
    </ol>

    <section class="revision-spans" aria-labelledby="revision-spans-title">
      <h5 id="revision-spans-title" class="trace-events-title">CANDIDATE PR REVISIONS · COMMIT-LIST SPANS</h5>
      <ol class="trace-events revision-span-list">
        <li v-for="span in trace.revisionSpans" :key="`${span.fromReviewId}:${span.toReviewId}`">
          <div class="trace-event-meta">
            <span><a :href="span.fromCommentUrl" target="_blank" rel="noreferrer">QODO COMMENT #{{ span.fromReviewId }} ↗</a> → <a :href="span.toCommentUrl" target="_blank" rel="noreferrer">QODO COMMENT #{{ span.toReviewId }} ↗</a></span>
          </div>
          <p v-if="span.fromRevision && span.toRevision" class="review-revision">
            REVIEWED ANCHORS
            <a :href="span.fromRevision.url" target="_blank" rel="noreferrer">{{ span.fromRevision.shortSha }} ↗</a>
            →
            <a :href="span.toRevision.url" target="_blank" rel="noreferrer">{{ span.toRevision.shortSha }} ↗</a>
          </p>
          <p v-if="span.status === 'unavailable'" class="checks-note">Span unavailable: one or both comments do not mark a revision in this PR’s imported commit list.</p>
          <p v-else-if="span.status === 'same_revision'" class="checks-note">Both comments mark the same PR revision; there is no revision interval to inspect.</p>
          <p v-else-if="span.status === 'out_of_order'" class="checks-note">Marked revisions run backward in GitHub’s PR commit-list order, so Ghostline does not infer a span.</p>
          <p v-else-if="span.status === 'adjacent'" class="checks-note">These marked revisions are adjacent in GitHub’s PR commit list; no candidate commit appears between them.</p>
          <template v-else>
            <p class="checks-note">The commits below appear between these reviewed revision anchors in GitHub’s PR commit-list order. Inspect them as candidates; these are not verified repairs.</p>
            <ul class="review-followups-list">
              <li v-for="revision in span.candidateRevisions" :key="revision.sha">
                <span><a :href="revision.url" target="_blank" rel="noreferrer">{{ revision.shortSha }} ↗</a> {{ revision.message }}</span>
                <button type="button" :aria-label="`Focus candidate PR revision ${revision.shortSha}`" @click="emit('focusRevision', revision.sha)">FOCUS</button>
                <button v-if="span.fromRevision" type="button" :aria-label="`Compare candidate ${revision.shortSha} with earlier review revision ${span.fromRevision.shortSha}`" @click="inspectCandidate(span.fromRevision.sha, revision.sha)">INSPECT CHANGES</button>
                <section v-if="comparisonTarget?.baseSha === span.fromRevision?.sha && comparisonTarget?.headSha === revision.sha && comparisonLoading" class="candidate-comparison" role="status">Loading GitHub comparison for the selected revision pair…</section>
                <section v-else-if="comparisonTarget?.baseSha === span.fromRevision?.sha && comparisonTarget?.headSha === revision.sha && comparisonError" class="candidate-comparison comparison-error" role="status"><strong>Comparison unavailable.</strong> {{ comparisonError }} <span>You can still inspect either revision directly on GitHub.</span></section>
                <section v-else-if="comparisonTarget?.baseSha === span.fromRevision?.sha && comparisonTarget?.headSha === revision.sha && comparison" class="candidate-comparison" aria-label="Candidate revision comparison">
                  <p class="comparison-boundary">CANDIDATE DIFF · {{ comparison.baseSha.slice(0, 7) }} → {{ comparison.headSha.slice(0, 7) }}. This shows changed files between revisions; it does not prove a finding was addressed.</p>
                  <p class="comparison-summary">{{ comparison.totalCommits }} commits · {{ comparison.files.length }} changed file{{ comparison.files.length === 1 ? '' : 's' }} returned by GitHub <a :href="comparison.compareUrl" target="_blank" rel="noreferrer">OPEN FULL COMPARISON ↗</a></p>
                  <ul v-if="comparison.files.length" class="comparison-files"><li v-for="file in comparison.files" :key="file.path"><a :href="file.url" target="_blank" rel="noreferrer">{{ file.path }} ↗</a><span>{{ file.status }} · +{{ file.additions }} −{{ file.deletions }}</span></li></ul>
                  <p v-else class="checks-note">GitHub returned no changed file entries for this revision pair.</p>
                </section>
              </li>
            </ul>
          </template>
        </li>
      </ol>
    </section>

    <p class="footnote">The title match and Qodo comment timestamps are observed evidence. They do not establish causation, a repair attempt, or a successful fix.</p>
    <p class="trace-boundary">Ghostline observes published review comments. Use your existing IDE or coding agent to inspect and guide the work.</p>
  </section>
</template>
