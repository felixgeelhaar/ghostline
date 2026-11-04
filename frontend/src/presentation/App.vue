<script setup lang="ts">
import { computed, ref } from 'vue'
import { importPullRequest } from '../application/importPullRequest'
import { checkOutcome, headCommitLabel, orderChecks, summarizeChecks, type CheckTone } from '../domain/checks'
import { validatePullRequestURL, type PullRequest } from '../domain/pullRequest'

const url = ref('')
const pr = ref<PullRequest>()
const error = ref('')
const loading = ref(false)
const orderedChecks = computed(() => orderChecks(pr.value?.checks.items ?? []))
const checksSummary = computed(() => summarizeChecks(pr.value?.checks.items ?? []))
const glyph: Record<CheckTone, string> = { pass: '✓', fail: '✕', running: '●', neutral: '–' }

async function loadPR() {
  error.value = validatePullRequestURL(url.value) ?? ''
  if (error.value) return
  loading.value = true; pr.value = undefined
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
    <section v-if="pr" class="change-card"><div class="change-heading"><div><span class="eyebrow">{{ pr.repository }} / PULL REQUEST #{{ pr.number }}</span><h2>{{ pr.title }}</h2></div><span class="state" :class="pr.state">{{ pr.state }}</span></div><div class="meta"><span>{{ pr.commits.length }} COMMITS</span><span>OPENED {{ date(pr.createdAt) }}</span><a :href="pr.url" target="_blank" rel="noreferrer">VIEW ON GITHUB ↗</a></div><section class="checks" aria-labelledby="checks-title"><h3 id="checks-title" class="section-title"><span>CHECKS · LATEST REVISION</span><span v-if="pr.checks.headSha">HEAD <a :href="pr.checks.headUrl" target="_blank" rel="noreferrer" :aria-label="`Head commit ${pr.checks.headShortSha} on GitHub`">{{ pr.checks.headShortSha }} ↗</a></span></h3><p v-if="pr.checks.availability === 'unavailable'" class="checks-note warn" role="status"><strong>Checks unavailable.</strong> {{ pr.checks.reason }}</p><p v-else-if="pr.checks.items.length === 0" class="checks-note" role="status"><strong>No checks reported.</strong> GitHub returned no check runs or commit statuses for head commit {{ pr.checks.headShortSha }}.</p><template v-else><p class="checks-summary" :class="checksSummary.tone" role="status"><span aria-hidden="true">{{ glyph[checksSummary.tone] }}</span>{{ checksSummary.label }}</p><ul class="check-list"><li v-for="(check, i) in orderedChecks" :key="`${check.kind}:${check.name}:${i}`"><span class="check-state" :class="checkOutcome(check).tone"><span aria-hidden="true">{{ glyph[checkOutcome(check).tone] }}</span>{{ checkOutcome(check).label }}</span><span class="check-name"><a v-if="check.url" :href="check.url" target="_blank" rel="noreferrer">{{ check.name }} <span aria-hidden="true">↗</span><span class="sr-only">(opens on GitHub)</span></a><span v-else>{{ check.name }}</span><small>{{ [check.source, check.url ? '' : 'no GitHub link reported'].filter(Boolean).join(' · ') }}</small></span></li></ul></template><p class="footnote">Checks are GitHub’s latest results for head commit {{ pr.checks.headShortSha || '(unknown)' }} at import time. Checks on earlier commits are not shown and do not validate this revision. Re-import to refresh.</p></section><h3 class="section-title"><span>REVISION TIMELINE</span><span>COMMIT TIME · OLDEST FIRST</span></h3><ol class="timeline"><li v-for="commit in pr.commits" :key="commit.sha"><span class="node"></span><div class="commit"><div class="commit-meta"><span><a :href="commit.url" target="_blank" rel="noreferrer">{{ commit.shortSha }}</a><span v-if="headCommitLabel(pr.checks, commit.sha)" class="head-tag">{{ headCommitLabel(pr.checks, commit.sha) }}</span></span><time>{{ date(commit.committedAt) }}</time></div><h4>{{ commit.message }}</h4><p>COMMITTED BY {{ commit.author }}</p></div></li></ol><p class="footnote">Commit timestamps come from Git commit metadata. They do not establish push time or complete pull request event chronology.</p></section>
    <footer><span>GHOSTLINE // DAY 01</span><span>EVERY SIGNAL HAS A SOURCE.</span></footer>
  </main>
</template>
