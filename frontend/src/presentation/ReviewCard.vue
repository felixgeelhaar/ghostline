<script setup lang="ts">
import type { QodoReview } from '../domain/reviews'

defineProps<{ review: QodoReview; contextOnly?: boolean }>()

function date(value: string) {
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}
</script>

<template>
  <div class="review-meta"><strong>{{ review.kind === 'findings' ? 'Code review findings' : 'Pull request summary' }}</strong><time>{{ date(review.createdAt) }}</time></div>
  <p v-if="review.summary" class="review-summary">{{ review.summary }}</p>
  <ul v-if="review.findingTitles.length" class="finding-list"><li v-for="(title, index) in review.findingTitles" :key="`${review.id}:${index}`">{{ title }}</li></ul>
  <p v-if="review.commitSha && review.commitUrl" class="review-revision">REVIEWED REVISION <a :href="review.commitUrl" target="_blank" rel="noreferrer">{{ review.commitSha.slice(0, 7) }} ↗</a></p>
  <p v-else-if="contextOnly" class="review-revision unmarked">REVISION NOT MARKED · CONTEXT ONLY</p>
  <a class="review-source" :href="review.url" target="_blank" rel="noreferrer">VIEW ORIGINAL QODO COMMENT ↗</a>
</template>
