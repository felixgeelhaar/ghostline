<script setup lang="ts">
import { computed } from 'vue'
import { reviewSessionFreshness, reviewRunForFinding, type QodoStructuredReview } from '../domain/qodoReviewSession'

const props = defineProps<{ review: QodoStructuredReview; pullRequestUrl: string; headSha: string }>()
const freshness = computed(() => reviewSessionFreshness(props.review, props.headSha))

function revisionURL(sha?: string) {
  if (!sha) return undefined
  const pr = new URL(props.pullRequestUrl)
  const [, owner, repo] = pr.pathname.split('/')
  return `https://github.com/${encodeURIComponent(owner ?? '')}/${encodeURIComponent(repo ?? '')}/commit/${encodeURIComponent(sha)}`
}

function citationURL(source?: string) {
  if (!source) return undefined
  try {
    const candidate = new URL(source)
    return candidate.protocol === 'https:' && !candidate.username && !candidate.password ? candidate.href : undefined
  } catch {
    return undefined
  }
}
</script>

<template>
  <section class="qodo-structured" aria-labelledby="qodo-structured-title">
    <h4 id="qodo-structured-title" class="section-title"><span>STRUCTURED QODO REVIEW · DIRECT SOURCE</span><span>{{ review.findings.length }} FINDINGS</span></h4>
    <p class="review-provenance">OBSERVED · {{ review.source }}</p>
    <p v-if="!review.reviewSession" class="checks-note" role="status"><strong>No review session returned.</strong> Qodo did not provide structured review-session data for this pull request.</p>
    <template v-if="review.reviewSession">
      <div class="qodo-session-status" :class="freshness">
        <div><span>QODO SESSION · {{ review.reviewSession.status.toUpperCase() }}</span>
          <p v-if="review.reviewSession.commitSha">Reviewed revision <a :href="revisionURL(review.reviewSession.commitSha)" target="_blank" rel="noreferrer">{{ review.reviewSession.commitSha.slice(0, 7) }} ↗</a></p>
          <p v-else>Qodo did not return the reviewed commit SHA.</p>
        </div>
        <strong>{{ freshness === 'current' ? 'MATCHES PR HEAD' : freshness === 'stale' ? 'DOES NOT MATCH PR HEAD' : 'REVISION UNKNOWN' }}</strong>
      </div>
      <p v-if="freshness === 'stale'" class="checks-note warn" role="status"><strong>Review evidence is stale.</strong> The reviewed commit does not match the imported pull request head; these findings do not describe the current head.</p>
      <p v-else-if="freshness === 'unknown'" class="checks-note warn" role="status"><strong>Review freshness is unknown.</strong> Ghostline cannot compare this session with a known pull request head revision.</p>
    </template>
    <p v-if="review.findings.length === 0" class="checks-note" role="status">Qodo returned no current findings for this review response.</p>
    <ol v-else class="qodo-managed-findings">
        <li v-for="finding in review.findings" :key="finding.id">
          <div class="finding-heading"><strong>{{ finding.title }}</strong><span v-if="finding.actionLevel">{{ finding.actionLevel.replaceAll('_', ' ') }}</span></div>
          <p class="finding-labels">QODO FINDING {{ finding.id }} · REPORTED {{ finding.status || 'NO STATUS' }}<span v-if="finding.category"> · {{ finding.category }}</span><span v-if="finding.attributionStatus"> · {{ finding.attributionStatus }}</span></p>
          <p v-if="finding.description" class="finding-description">{{ finding.description }}</p>
          <p v-if="finding.gitSha" class="review-revision">RAISED ON <a :href="revisionURL(finding.gitSha)" target="_blank" rel="noreferrer">{{ finding.gitSha.slice(0, 7) }} ↗</a></p>
          <p v-if="finding.filePath" class="finding-location">{{ finding.filePath }}<span v-if="finding.startLine">:{{ finding.startLine }}<span v-if="finding.endLine && finding.endLine !== finding.startLine">–{{ finding.endLine }}</span></span><span v-if="finding.side"> · {{ finding.side }}</span></p>
          <section v-if="finding.evidence" class="qodo-finding-evidence" aria-label="Qodo citation evidence">
            <p v-if="finding.evidence.explanation">QODO EVIDENCE · {{ finding.evidence.explanation }}</p>
            <ul v-if="finding.evidence.citations?.length"><li v-for="(citation, index) in finding.evidence.citations" :key="`${finding.id}:citation:${index}`">
              <span class="citation-type">{{ citation.sourceType || 'SOURCE' }}</span>
              <a v-if="citationURL(citation.source)" :href="citationURL(citation.source)" target="_blank" rel="noreferrer">{{ citation.source }} ↗</a>
              <span v-else-if="citation.source">{{ citation.source }}</span>
              <span v-if="citation.context"> · {{ citation.context }}</span>
              <blockquote v-if="citation.text">{{ citation.text }}</blockquote>
            </li></ul>
          </section>
          <p v-if="finding.resolutionReason" class="finding-description">Qodo’s recorded reason: {{ finding.resolutionReason }}</p>
          <p v-if="reviewRunForFinding(finding, review)" class="qodo-run-link">QODO-LINKED REVIEW RUN {{ reviewRunForFinding(finding, review)?.id }} · {{ reviewRunForFinding(finding, review)?.status }}<span v-if="reviewRunForFinding(finding, review)?.commitSha"> · REVISION {{ reviewRunForFinding(finding, review)?.commitSha?.slice(0, 7) }}</span></p>
          <p v-else class="qodo-run-link">NO REVIEW-RUN LINK RETURNED FOR THIS FINDING</p>
        </li>
    </ol>
    <section v-if="review.reviewRuns.length" class="qodo-runs" aria-label="Qodo review runs">
        <h5>RECORDED QODO REVIEW RUNS</h5>
        <ol><li v-for="run in review.reviewRuns" :key="run.id"><span>{{ run.status.toUpperCase() }} · {{ run.id }}</span><a v-if="run.commitSha" :href="revisionURL(run.commitSha)" target="_blank" rel="noreferrer">{{ run.commitSha.slice(0, 7) }} ↗</a><span v-else>REVISION NOT RETURNED</span></li></ol>
    </section>
    <p class="footnote">Statuses and run links are reported by Qodo. Ghostline does not independently verify that a finding was fixed, nor infer causation from commit order.</p>
  </section>
</template>
