<script setup lang="ts">
import { computed } from 'vue'
import { findingLifecycleStatus, findingRevisionFreshness, qodoCitationURL, qodoRevisionURL, reviewAttentionSummary, reviewSessionFreshness, reviewRunForFinding, type QodoStructuredReview } from '../domain/qodoReviewSession'

const props = defineProps<{ review: QodoStructuredReview; pullRequestUrl: string; headSha: string }>()
const freshness = computed(() => reviewSessionFreshness(props.review, props.headSha))
const attention = computed(() => reviewAttentionSummary(props.review, props.headSha))

</script>

<template>
  <section class="qodo-structured" aria-labelledby="qodo-structured-title">
    <h4 id="qodo-structured-title" class="section-title"><span>STRUCTURED QODO REVIEW · DIRECT SOURCE</span><span>{{ review.findings.length }} FINDINGS</span></h4>
    <p class="review-provenance">OBSERVED · {{ review.source }}</p>
    <aside class="qodo-attention" :class="attention.kind" role="status" aria-live="polite" aria-label="Qodo review next action">
      <p class="qodo-attention-label">ENGINEER NEXT ACTION · BASED ON QODO’S REPORTED STATE</p>
      <h5>{{ attention.title }}</h5>
      <p>{{ attention.detail }}</p>
      <p class="qodo-attention-next"><strong>NEXT</strong> {{ attention.nextAction }}</p>
    </aside>
    <p v-for="warning in review.warnings ?? []" :key="warning" class="checks-note warn" role="alert"><strong>Partial Qodo response.</strong> {{ warning }}</p>
    <p v-if="!review.reviewSession" class="checks-note" role="status"><strong>No review session returned.</strong> Qodo did not provide structured review-session data for this pull request.</p>
    <template v-if="review.reviewSession">
      <div class="qodo-session-status" :class="freshness">
        <div><span>QODO SESSION · {{ review.reviewSession.status.toUpperCase() }}</span>
          <p v-if="review.reviewSession.commitSha">Reviewed revision <a v-if="qodoRevisionURL(pullRequestUrl, review.reviewSession.commitSha)" :href="qodoRevisionURL(pullRequestUrl, review.reviewSession.commitSha)" target="_blank" rel="noreferrer">{{ review.reviewSession.commitSha.slice(0, 7) }} ↗</a><span v-else>{{ review.reviewSession.commitSha.slice(0, 7) }} · source link unavailable</span></p>
          <p v-else>Qodo did not return the reviewed commit SHA.</p>
        </div>
        <strong>{{ freshness === 'current' ? 'MATCHES PR HEAD' : freshness === 'stale' ? 'DOES NOT MATCH PR HEAD' : 'REVISION UNKNOWN' }}</strong>
      </div>
      <p v-if="freshness === 'stale'" class="checks-note warn" role="status"><strong>Review session is stale.</strong> The reviewed commit does not match the imported pull request head. Inspect each finding’s own revision marker to see which findings match the current head.</p>
      <p v-else-if="freshness === 'unknown'" class="checks-note warn" role="status"><strong>Review freshness is unknown.</strong> <span v-if="!headSha">Ghostline cannot compare this session with a known pull request head revision.</span><span v-else>Qodo did not return the reviewed commit SHA, so Ghostline cannot compare this session with the imported PR head.</span></p>
    </template>
    <p v-if="review.findings.length === 0" class="checks-note" role="status">Qodo returned no findings in this response.<span v-if="freshness === 'current'"> The review session matches the imported PR head.</span><span v-else-if="freshness === 'stale'"> This review session does not match the imported PR head.</span><span v-else> Review freshness is unknown.</span></p>
    <ol v-else class="qodo-managed-findings">
        <li v-for="finding in review.findings" :key="finding.id">
          <div class="finding-heading"><strong>{{ finding.title }}</strong><span v-if="finding.actionLevel">{{ finding.actionLevel.replaceAll('_', ' ') }}</span></div>
          <p class="finding-labels">QODO FINDING {{ finding.id }} · QODO STATUS {{ findingLifecycleStatus(finding) || 'UNCLASSIFIED' }} · ATTRIBUTION {{ finding.attributionStatus || 'NOT RETURNED' }} · LEGACY STATUS {{ finding.status || 'NOT RETURNED' }}<span v-if="finding.category"> · {{ finding.category }}</span></p>
          <p v-if="finding.description" class="finding-description">{{ finding.description }}</p>
          <p v-if="finding.gitSha" class="review-revision">RAISED ON <a v-if="qodoRevisionURL(pullRequestUrl, finding.gitSha)" :href="qodoRevisionURL(pullRequestUrl, finding.gitSha)" target="_blank" rel="noreferrer">{{ finding.gitSha.slice(0, 7) }} ↗</a><span v-else>{{ finding.gitSha.slice(0, 7) }} · source link unavailable</span></p>
          <p v-else class="review-revision">QODO DID NOT RETURN A FINDING-SPECIFIC REVISION<span v-if="reviewRunForFinding(finding, review)?.commitSha"> · LINKED RUN REVISION {{ reviewRunForFinding(finding, review)?.commitSha?.slice(0, 7) }}</span><span v-else-if="finding.reviewRunId"> · LINKED RUN REVISION NOT RETURNED</span><span v-else> · NO LINKED RUN REVISION AVAILABLE</span></p>
          <p v-if="finding.gitSha && findingRevisionFreshness(finding, headSha) === 'stale'" class="checks-note warn" role="status"><strong>Finding revision does not match the imported PR head.</strong> Qodo attached this finding to {{ finding.gitSha.slice(0, 7) }}, while Ghostline imported {{ headSha.slice(0, 7) }}.</p>
          <p v-if="finding.filePath" class="finding-location">{{ finding.filePath }}<span v-if="finding.startLine">:{{ finding.startLine }}<span v-if="finding.endLine && finding.endLine !== finding.startLine">–{{ finding.endLine }}</span></span><span v-if="finding.side"> · {{ finding.side }}</span></p>
          <pre v-if="finding.codeSnippet" class="qodo-code-snippet" aria-label="Code excerpt reported by Qodo">{{ finding.codeSnippet }}</pre>
          <section v-if="finding.evidence" class="qodo-finding-evidence" aria-label="Qodo citation evidence">
            <p v-if="finding.evidence.explanation">QODO EVIDENCE · {{ finding.evidence.explanation }}</p>
            <ul v-if="finding.evidence.citations?.length"><li v-for="(citation, index) in finding.evidence.citations" :key="`${finding.id}:citation:${index}`">
              <span class="citation-type">{{ citation.sourceType || 'SOURCE' }}</span>
              <a v-if="qodoCitationURL(pullRequestUrl, citation.source)" :href="qodoCitationURL(pullRequestUrl, citation.source)" target="_blank" rel="noreferrer">{{ citation.source }} ↗</a>
              <span v-else-if="citation.source">{{ citation.source }}</span>
              <span v-if="citation.context"> · {{ citation.context }}</span>
              <blockquote v-if="citation.text">{{ citation.text }}</blockquote>
              <span v-if="citation.metadata !== undefined" class="citation-metadata">CITATION METADATA · {{ JSON.stringify(citation.metadata) }}</span>
            </li></ul>
          </section>
          <p v-if="finding.resolutionReason" class="finding-description">Qodo’s recorded reason: {{ finding.resolutionReason }}</p>
          <p v-if="reviewRunForFinding(finding, review)" class="qodo-run-link">QODO-LINKED REVIEW RUN {{ reviewRunForFinding(finding, review)?.id }} · {{ reviewRunForFinding(finding, review)?.status }}<span v-if="reviewRunForFinding(finding, review)?.commitSha"> · REVISION {{ reviewRunForFinding(finding, review)?.commitSha?.slice(0, 7) }}</span></p>
          <p v-else-if="finding.reviewRunId" class="qodo-run-link">QODO REPORTED LINKED REVIEW RUN {{ finding.reviewRunId }} · DETAILS NOT RETURNED</p>
          <p v-else class="qodo-run-link">NO REVIEW-RUN LINK RETURNED FOR THIS FINDING</p>
        </li>
    </ol>
    <section v-if="review.reviewRuns.length" class="qodo-runs" aria-label="Qodo review runs">
        <h5>RECORDED QODO REVIEW RUNS</h5>
        <ol><li v-for="run in review.reviewRuns" :key="run.id"><span>{{ run.status.toUpperCase() }} · {{ run.id }}</span><a v-if="run.commitSha && qodoRevisionURL(pullRequestUrl, run.commitSha)" :href="qodoRevisionURL(pullRequestUrl, run.commitSha)" target="_blank" rel="noreferrer">{{ run.commitSha.slice(0, 7) }} ↗</a><span v-else-if="run.commitSha">{{ run.commitSha.slice(0, 7) }} · SOURCE LINK UNAVAILABLE</span><span v-else>REVISION NOT RETURNED</span></li></ol>
    </section>
    <p class="footnote">Statuses and run links are reported by Qodo. Ghostline does not independently verify that a finding was fixed, nor infer causation from commit order.</p>
  </section>
</template>
