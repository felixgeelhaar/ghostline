<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { interventionPrompt, type FindingTrace } from '../domain/trace'

const props = defineProps<{ trace: FindingTrace }>()
const emit = defineEmits<{
  close: []
  focusRevision: [sha: string]
}>()

const copyStatus = ref('')
const heading = ref<HTMLHeadingElement | null>(null)

onMounted(() => heading.value?.focus())
watch(() => props.trace.id, () => { copyStatus.value = '' })

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
