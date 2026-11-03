<script setup lang="ts">
import { ref } from 'vue'
import { importPullRequest, scanRepository } from '../application/importPullRequest'
import { validatePullRequestURL, type PullRequest, type ScanResponse } from '../domain/pullRequest'

const url = ref('')
const repoPath = ref('')
const pr = ref<PullRequest>()
const scan = ref<ScanResponse>()
const error = ref('')
const loading = ref(false)
const scanning = ref(false)

async function loadPR() {
  error.value = validatePullRequestURL(url.value) ?? ''
  if (error.value) return
  loading.value = true; pr.value = undefined
  try { pr.value = await importPullRequest(url.value) } catch (e) { error.value = e instanceof Error ? e.message : 'Could not load the pull request.' }
  finally { loading.value = false }
}
async function runScans() {
  error.value = ''
  if (!repoPath.value.trim()) { error.value = 'Enter a local repository path to run scans.'; return }
  scanning.value = true
  try { scan.value = await scanRepository(repoPath.value.trim()) } catch (e) { error.value = e instanceof Error ? e.message : 'Could not run repository scans.' }
  finally { scanning.value = false }
}
function date(value: string) { return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
</script>

<template>
  <main class="shell">
    <header class="topbar"><a class="brand" href="#"><span class="brand-mark">G</span><span>GHOSTLINE<small>ENGINEERING CONTROL ROOM</small></span></a><span class="live"><i></i> SYSTEM READY</span></header>
    <section class="hero"><p class="eyebrow">CHANGE INTELLIGENCE / 001</p><h1>See the work.<br><em>Trace the decisions.</em></h1><p class="lede">Import a pull request to inspect its revision history. Commit timestamps show when commits were authored, not when they were pushed.</p></section>
    <section class="import-panel"><label for="pr-url">GITHUB PULL REQUEST</label><div class="input-row"><span class="prefix">↗</span><input id="pr-url" v-model="url" placeholder="https://github.com/owner/repo/pull/123" @keyup.enter="loadPR"><button :disabled="loading" @click="loadPR">{{ loading ? 'FETCHING…' : 'IMPORT CHANGE' }} <span>↗</span></button></div><p v-if="error" class="error" role="alert">{{ error }}</p></section>
    <section v-if="pr" class="change-card"><div class="change-heading"><div><span class="eyebrow">{{ pr.repository }} / PULL REQUEST #{{ pr.number }}</span><h2>{{ pr.title }}</h2></div><span class="state" :class="pr.state">{{ pr.state }}</span></div><div class="meta"><span>{{ pr.commits.length }} COMMITS</span><span>OPENED {{ date(pr.createdAt) }}</span><a :href="pr.url" target="_blank" rel="noreferrer">VIEW ON GITHUB ↗</a></div><h3 class="section-title"><span>REVISION TIMELINE</span><span>COMMIT TIME · OLDEST FIRST</span></h3><ol class="timeline"><li v-for="commit in pr.commits" :key="commit.sha"><span class="node"></span><div class="commit"><div class="commit-meta"><a :href="commit.url" target="_blank" rel="noreferrer">{{ commit.shortSha }}</a><time>{{ date(commit.committedAt) }}</time></div><h4>{{ commit.message }}</h4><p>COMMITTED BY {{ commit.author }}</p></div></li></ol><p class="footnote">Commit timestamps come from Git commit metadata. They do not establish push time or complete pull request event chronology.</p></section>
    <section class="scan-panel"><div><p class="eyebrow">LOCAL EVIDENCE / OPTIONAL</p><h2>Repository forensics</h2><p>Run Nox security analysis and the repository’s configured Warden validation gate. Results are separate from GitHub commit history.</p></div><div class="input-row"><input v-model="repoPath" aria-label="Local repository path" placeholder="/path/to/repository"><button :disabled="scanning" @click="runScans">{{ scanning ? 'SCANNING…' : 'RUN CHECKS' }}</button></div><div v-if="scan" class="scan-results"><article v-for="result in scan.results" :key="result.name"><b>{{ result.name }}</b><span :class="'scan-'+result.status">{{ result.status.replace('_',' ') }}</span><p>{{ result.summary }}</p><details v-if="result.output"><summary>Inspect source output</summary><pre>{{ result.output }}</pre></details></article></div><small class="scan-note">Nox runs offline. Warden runs its configured pre-push gate (set WARDEN_HOOK=pre-commit to run the faster gate). A nonzero exit means the gate failed; inspect source output for details. These results are not tied to the imported PR revision.</small></section>
    <footer><span>GHOSTLINE // DAY 01</span><span>EVERY SIGNAL HAS A SOURCE.</span></footer>
  </main>
</template>
