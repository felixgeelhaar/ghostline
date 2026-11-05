<script setup lang="ts">
import { computed } from 'vue'
import { laterRevisionsToInspect, type QodoReview, type RevisionRef } from '../domain/reviews'

const props = defineProps<{ review: QodoReview; commits: RevisionRef[]; contextOnly?: boolean }>()
const emit = defineEmits<{ focusRevision: [sha: string] }>()
const laterRevisions = computed(() => laterRevisionsToInspect(props.review, props.commits))

function date(value: string) {
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}
</script>

<template>
  <div class="review-meta"><strong>{{ review.kind === 'findings' ? 'Code review findings' : 'Pull request summary' }}</strong><time>{{ date(review.createdAt) }}</time></div>
  <p class="review-provenance">OBSERVED · {{ review.provider }} via GitHub issue comment #{{ review.id }} · {{ review.sourceAuthor }}</p>
  <p v-if="review.summary" class="review-summary">{{ review.summary }}</p>
  <p v-if="review.kind === 'findings' && review.findings.length === 0" class="checks-note warn" role="status"><strong>Finding details unavailable.</strong> Ghostline recognized the Qodo review comment but could not extract its findings. Open the source comment for the complete review.</p>
  <ul v-if="review.findings.length" class="finding-list"><li v-for="finding in review.findings" :key="finding.id">
    <div class="finding-heading"><strong>{{ finding.title }}</strong><span v-if="finding.severity">{{ finding.severity }}</span></div>
    <p class="finding-labels">{{ [...finding.categories, finding.lifecycle, finding.actionLevel].filter(Boolean).join(' · ') }}</p>
    <p v-if="finding.description" class="finding-description">{{ finding.description }}</p>
    <a class="review-source" :href="finding.sourceUrl" target="_blank" rel="noreferrer">SOURCE FINDING ↗</a>
  </li></ul>
  <p v-if="review.commitSha && review.commitUrl" class="review-revision">REVIEWED REVISION <a :href="review.commitUrl" target="_blank" rel="noreferrer">{{ review.commitSha.slice(0, 7) }} ↗</a></p>
  <p v-else-if="contextOnly" class="review-revision unmarked">REVISION NOT MARKED · CONTEXT ONLY</p>
  <section v-if="laterRevisions.length" class="review-followups" aria-label="Later revisions to inspect">
    <h4>LATER PR REVISIONS · ORDER ONLY</h4>
    <ul><li v-for="revision in laterRevisions" :key="revision.sha"><span><a :href="revision.url" target="_blank" rel="noreferrer">{{ revision.shortSha }} ↗</a> {{ revision.message }}</span><button type="button" @click="emit('focusRevision', revision.sha)">FOCUS</button></li></ul>
    <p>These commits follow the inspected revision in GitHub’s pull-request commit list. That ordering does not show that a commit repaired a finding.</p>
  </section>
  <a class="review-source" :href="review.url" target="_blank" rel="noreferrer">VIEW ORIGINAL QODO COMMENT ↗</a>
</template>
