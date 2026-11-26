<script setup lang="ts">
import { computed, ref } from 'vue'
import { listOpenPullRequests } from '../application/listOpenPullRequests'
import { sweepReadiness } from '../application/readinessSweep'
import { openedHeadFor, rowNextAction, rowVerdict, type OpenedPullRequest, type OpenPullRequest, type OpenPullRequests, type RowCheck, type RowVerdict } from '../domain/openPullRequests'

// openedPull is the pull request the detail view imported, with the head it found.
const props = defineProps<{ openedPull?: OpenedPullRequest }>()
// request-failed passes each failure up, so the app can notice a lost sign-in.
const emit = defineEmits<{ open: [{ repository: string; row: OpenPullRequest }]; 'request-failed': [unknown] }>()

const repository = ref('')
const listing = ref(false)
const error = ref('')
const result = ref<OpenPullRequests>()
const checks = ref<Record<number, RowCheck>>({})
const checking = ref(false)
// Each list gets a generation; a request settling for an earlier list is dropped.
let generation = 0

const labels: Record<RowVerdict, string> = {
  pass: 'PASS', fail: 'FAIL', incomplete: 'INCOMPLETE', 'not-applicable': 'NOT APPLICABLE',
  stale: 'STALE', unchecked: 'NOT CHECKED', checking: 'CHECKING', error: 'UNAVAILABLE',
}
const tones: Record<RowVerdict, string> = {
  pass: 'pass', fail: 'fail', incomplete: 'running', 'not-applicable': 'neutral',
  stale: 'running', unchecked: 'neutral', checking: 'neutral', error: 'running',
}

const summary = computed(() => {
  const list = result.value
  if (!list) return ''
  const count = list.items.length
  const noun = count === 1 ? 'open pull request' : 'open pull requests'
  return list.complete
    ? `${count} ${noun} in ${list.repository}, newest first.`
    : `PARTIAL · the newest ${count} ${noun} in ${list.repository}; GitHub has more than ${list.limit}.`
})

function checkFor(row: OpenPullRequest): RowCheck { return checks.value[row.number] ?? { state: 'unchecked' } }
function openedHead(row: OpenPullRequest) { return result.value ? openedHeadFor(result.value.repository, row, props.openedPull) : undefined }
function verdictOf(row: OpenPullRequest) { return rowVerdict(row, checkFor(row), openedHead(row)) }
function nextActionOf(row: OpenPullRequest) { return rowNextAction(row, checkFor(row), openedHead(row)) }

async function list() {
  const request = ++generation
  listing.value = true
  checking.value = false
  error.value = ''
  result.value = undefined
  checks.value = {}
  try {
    const listed = await listOpenPullRequests(repository.value)
    if (request === generation) result.value = listed
  } catch (e) {
    if (request !== generation) return
    emit('request-failed', e)
    error.value = e instanceof Error ? e.message : 'Unable to list open pull requests.'
  } finally {
    if (request === generation) listing.value = false
  }
}

async function checkAll() {
  const list = result.value
  if (!list || checking.value) return
  const request = generation
  checking.value = true
  await sweepReadiness({
    repository: list.repository,
    rows: list.items,
    isCurrent: () => request === generation,
    skip: (row) => checkFor(row).state === 'checked',
    onUpdate: (number, check) => { checks.value = { ...checks.value, [number]: check } },
    onFailure: (error) => emit('request-failed', error),
  })
  if (request === generation) checking.value = false
}
</script>

<template>
  <section class="open-pulls" aria-labelledby="open-pulls-title">
    <h2 id="open-pulls-title" class="section-title"><span>OPEN PULL REQUESTS · READINESS</span></h2>
    <form @submit.prevent="list">
      <label for="open-pulls-repository">GITHUB REPOSITORY</label>
      <div class="input-row">
        <input id="open-pulls-repository" v-model="repository" placeholder="owner/repo" autocomplete="off" spellcheck="false" />
        <button type="submit" :disabled="listing">{{ listing ? 'LISTING…' : 'LIST OPEN PRS' }}</button>
      </div>
    </form>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <template v-else-if="result">
      <div class="open-pulls-summary">
        <p role="status" :class="{ partial: !result.complete }">{{ summary }}</p>
        <button v-if="result.items.length" type="button" class="open-pulls-check" :disabled="checking" @click="checkAll">{{ checking ? 'CHECKING…' : 'CHECK READINESS' }}</button>
      </div>
      <table v-if="result.items.length">
        <caption class="sr-only">Open pull requests in {{ result.repository }} with the readiness of each listed head</caption>
        <thead><tr><th scope="col">PULL REQUEST</th><th scope="col">HEAD</th><th scope="col">VERDICT</th><th scope="col">NEXT ACTION</th><th scope="col"><span class="sr-only">Open</span></th></tr></thead>
        <tbody>
          <tr v-for="row in result.items" :key="row.number">
            <td><span class="open-pulls-number">#{{ row.number }}</span> {{ row.title }}<small v-if="row.draft" class="open-pulls-tag">DRAFT</small><small class="open-pulls-author">{{ row.author }}</small></td>
            <td><code>{{ row.headShortSha }}</code></td>
            <td><span class="check-state" :class="tones[verdictOf(row)]">{{ labels[verdictOf(row)] }}</span></td>
            <td class="open-pulls-action">{{ nextActionOf(row) }}</td>
            <td><button type="button" :aria-label="`Open ${result.repository}#${row.number}: ${row.title}`" @click="emit('open', { repository: result.repository, row })">OPEN</button></td>
          </tr>
        </tbody>
      </table>
      <p class="footnote">A verdict counts only for the head shown. Checking reads GitHub live, one pull request at a time, and counts against the request limit; a row whose head moved is marked stale.</p>
    </template>
  </section>
</template>
