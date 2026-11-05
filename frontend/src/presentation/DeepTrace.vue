<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { interventionPrompt, type FindingTrace } from '../domain/trace'

defineProps<{ trace: FindingTrace }>()
const emit = defineEmits<{
  close: []
  focusRevision: [sha: string]
}>()

const copyStatus = ref('')
const heading = ref<HTMLHeadingElement | null>(null)

onMounted(() => heading.value?.focus())

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
        <p v-if="occurrence.categories.length" class="finding-labels">{{ [...occurrence.categories, occurrence.lifecycle, occurrence.severity, occurrence.actionLevel].filter(Boolean).join(' · ') }}</p>
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

    <p class="footnote">The title match and Qodo comment timestamps are observed evidence. They do not establish causation, a repair attempt, or a successful fix.</p>
    <p class="trace-boundary">Ghostline observes published review comments. Use your existing IDE or coding agent to inspect and guide the work.</p>
  </section>
</template>
