<script setup lang="ts">
import { computed } from 'vue'
import { orderReadinessReasons, readinessHeadline, type Readiness } from '../domain/readiness'

const props = defineProps<{ readiness?: Readiness; loading: boolean; error: string; importedHeadSha?: string }>()
defineEmits<{ check: [] }>()

const reasons = computed(() => orderReadinessReasons(props.readiness?.reasons ?? []))
const headMoved = computed(() => Boolean(props.readiness && props.importedHeadSha && props.readiness.headSha !== props.importedHeadSha))
const glyph = { pass: '✓', fail: '✕', incomplete: '●' } as const
function subjectLabel(subject: string) {
  if (subject.startsWith('check:')) return `CHECK · ${subject.slice(6)}`
  return { 'pull-request': 'PULL REQUEST', branch: 'BRANCH', qodo: 'QODO REVIEW', 'required-checks': 'REQUIRED CHECKS' }[subject] ?? subject.toUpperCase()
}
</script>

<template>
  <section class="readiness" aria-labelledby="readiness-title">
    <h3 id="readiness-title" class="section-title"><span>MERGE READINESS · CURRENT HEAD</span><span v-if="readiness"><a :href="readiness.headUrl" target="_blank" rel="noreferrer" :aria-label="`Head ${readiness.headShortSha} on GitHub`">{{ readiness.headShortSha }} ↗</a></span></h3>
    <div class="readiness-actions">
      <button type="button" :disabled="loading" @click="$emit('check')">{{ loading ? 'CHECKING…' : readiness ? 'CHECK AGAIN' : 'CHECK MERGE READINESS' }}</button>
      <p>Reads GitHub live for the pull request’s current head: branch position, the checks branch protection requires, and Qodo’s latest code review.</p>
    </div>
    <p v-if="loading" class="checks-note" role="status">Reading readiness evidence from GitHub…</p>
    <p v-else-if="error" class="checks-note warn" role="status"><strong>Readiness unavailable.</strong> {{ error }}</p>
    <template v-else-if="readiness">
      <p class="readiness-verdict" :class="readiness.verdict" role="status"><span aria-hidden="true">{{ glyph[readiness.verdict] }}</span>{{ readinessHeadline(readiness) }}</p>
      <p v-if="headMoved" class="checks-note warn readiness-head-moved" role="status"><strong>The head moved.</strong> This verdict describes {{ readiness.headShortSha }}; the imported timeline ends at {{ importedHeadSha?.slice(0, 7) }}. Re-import to see the new commits.</p>
      <p v-if="readiness.findings" class="readiness-findings">QODO FINDINGS ON HEAD · {{ readiness.findings.high }} HIGH · {{ readiness.findings.medium }} MEDIUM · {{ readiness.findings.low }} LOW</p>
      <ul class="readiness-reasons">
        <li v-for="reason in reasons" :key="reason.subject" :class="reason.outcome">
          <span class="check-state" :class="reason.outcome === 'incomplete' ? 'running' : reason.outcome"><span aria-hidden="true">{{ glyph[reason.outcome] }}</span>{{ reason.outcome }}</span>
          <div><small>{{ subjectLabel(reason.subject) }}</small><p>{{ reason.message }}</p><a v-if="reason.sourceUrl" :href="reason.sourceUrl" target="_blank" rel="noreferrer">SOURCE ↗<span class="sr-only"> (opens on GitHub)</span></a></div>
        </li>
      </ul>
      <p class="footnote">A verdict describes one commit at the moment it was read. Ghostline does not merge; a PASS means every required source covered this head, not that the change is correct.</p>
    </template>
  </section>
</template>
