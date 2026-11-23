<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { searchPullRequests } from '../application/searchPullRequests'
import { GhostlineApiError } from '../application/apiError'
import { classifyImportInput, suggestionStateLabel, type PullRequestSuggestion } from '../domain/pullRequestSearch'

// One field for a pull request URL, owner/repo#number, or, when signed in, text that
// searches the user's own public pull requests (ARIA APG combobox with a list popup).
const props = defineProps<{ modelValue: string; loading: boolean; canSearch: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: string]; import: [target: string]; 'search-failed': [error: unknown] }>()

const SEARCH_DELAY_MS = 300
const listboxId = 'pr-search-results'

type SearchState =
  | { status: 'idle' }
  | { status: 'searching' }
  | { status: 'done'; items: PullRequestSuggestion[]; partial: boolean }
  | { status: 'failed'; message: string; signInAgain: boolean }

const field = ref<HTMLInputElement>()
const state = ref<SearchState>({ status: 'idle' })
const open = ref(false)
const activeIndex = ref(-1)
let searchRequest = 0
let timer: ReturnType<typeof setTimeout> | undefined

const items = computed(() => state.value.status === 'done' ? state.value.items : [])
const activeId = computed(() => open.value && activeIndex.value >= 0 ? optionId(activeIndex.value) : undefined)
const announcement = computed(() => {
  if (!open.value) return ''
  switch (state.value.status) {
    case 'searching': return 'Searching your pull requests…'
    case 'done': {
      const count = state.value.items.length
      if (count === 0) return 'No matching pull requests — paste a PR URL'
      const found = `${count} ${count === 1 ? 'pull request' : 'pull requests'} found`
      return state.value.partial ? `${found}. GitHub returned a partial result.` : found
    }
    default: return ''
  }
})
const placeholder = computed(() => props.canSearch
  ? 'Search your pull requests, or paste a URL or owner/repo#123'
  : 'https://github.com/owner/repo/pull/123 or owner/repo#123')

function optionId(index: number) { return `${listboxId}-${index}` }

function cancelSearch() {
  searchRequest++
  if (timer !== undefined) clearTimeout(timer)
  timer = undefined
}

function close() {
  open.value = false
  activeIndex.value = -1
}

function onInput(event: Event) {
  const value = (event.target as HTMLInputElement).value
  emit('update:modelValue', value)
  // Results for the earlier text must not stay clickable while the new search waits.
  cancelSearch()
  state.value = { status: 'idle' }
  close()
  const input = classifyImportInput(value)
  if (!props.canSearch || input.kind !== 'search') return
  const request = searchRequest
  timer = setTimeout(() => { void runSearch(input.query, request) }, SEARCH_DELAY_MS)
}

async function runSearch(query: string, request: number) {
  timer = undefined
  state.value = { status: 'searching' }
  open.value = true
  try {
    const result = await searchPullRequests(query)
    // A later keystroke, Escape or pick started a newer request; this answer is stale.
    if (request !== searchRequest) return
    state.value = { status: 'done', items: result.items, partial: result.partial }
  } catch (error) {
    if (request !== searchRequest) return
    emit('search-failed', error)
    state.value = {
      status: 'failed',
      message: error instanceof Error ? error.message : 'Could not search your pull requests.',
      // The session is valid but the server holds no GitHub token for it (for example after a restart).
      signInAgain: error instanceof GhostlineApiError && error.code === 'SEARCH_SIGNIN_REQUIRED',
    }
  }
}

// The import target is the picked suggestion's own URL, never the field's text.
function choose(item: PullRequestSuggestion) {
  cancelSearch()
  close()
  state.value = { status: 'idle' }
  emit('update:modelValue', item.url)
  emit('import', item.url)
}

function submit() {
  if (open.value && activeIndex.value >= 0 && items.value[activeIndex.value]) {
    choose(items.value[activeIndex.value]!)
    return
  }
  // Read the field itself: the bound value can lag a same-tick edit by one render.
  const text = field.value?.value ?? props.modelValue
  const input = classifyImportInput(text)
  if (props.canSearch && input.kind === 'search') {
    // Search text is not an import target: show (or fetch now) the matches to pick from.
    if (timer !== undefined || state.value.status === 'idle' || state.value.status === 'failed') {
      cancelSearch()
      void runSearch(input.query, searchRequest)
    } else {
      open.value = true
    }
    return
  }
  cancelSearch()
  close()
  emit('import', text)
}

// Leaving the field drops the list and any search still waiting for typing to pause.
function onBlur() {
  cancelSearch()
  if (state.value.status === 'searching') state.value = { status: 'idle' }
  close()
}

function move(step: 1 | -1) {
  if (items.value.length === 0) return
  open.value = true
  const count = items.value.length
  activeIndex.value = activeIndex.value < 0
    ? (step === 1 ? 0 : count - 1)
    : (activeIndex.value + step + count) % count
}

function onKeydown(event: KeyboardEvent) {
  switch (event.key) {
    case 'ArrowDown': event.preventDefault(); move(1); break
    case 'ArrowUp': event.preventDefault(); move(-1); break
    case 'Enter': event.preventDefault(); submit(); break
    case 'Escape':
      // Also stops a search still waiting for typing to pause.
      if (open.value) event.preventDefault()
      cancelSearch()
      if (state.value.status === 'searching') state.value = { status: 'idle' }
      close()
      break
  }
}

function updated(value: string) {
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

onBeforeUnmount(cancelSearch)
</script>

<template>
  <label for="pr-url">GITHUB PULL REQUEST</label>
  <div class="input-row combobox">
    <span class="prefix">↗</span>
    <input
      id="pr-url"
      ref="field"
      :value="modelValue"
      :placeholder="placeholder"
      role="combobox"
      aria-autocomplete="list"
      :aria-expanded="open"
      :aria-controls="listboxId"
      :aria-activedescendant="activeId"
      autocomplete="off"
      spellcheck="false"
      @input="onInput"
      @keydown="onKeydown"
      @blur="onBlur"
    >
    <button :disabled="loading" @mousedown.prevent @click="submit">{{ loading ? 'FETCHING…' : 'IMPORT CHANGE' }} <span>↗</span></button>
    <div v-show="open" class="pr-search-popup">
      <ul :id="listboxId" role="listbox" aria-label="Your pull requests">
        <li
          v-for="(item, index) in items"
          :id="optionId(index)"
          :key="item.url"
          role="option"
          :aria-selected="index === activeIndex"
          :class="{ active: index === activeIndex }"
          @mousedown.prevent="choose(item)"
        >
          <span class="pr-search-ref">{{ item.repository }} #{{ item.number }}</span>
          <span class="pr-search-title">{{ item.title }}</span>
          <span class="pr-search-meta"><span class="state" :class="item.state">{{ suggestionStateLabel(item.state) }}</span> UPDATED {{ updated(item.updatedAt) }}</span>
        </li>
      </ul>
      <p v-if="state.status === 'searching'" class="pr-search-note">Searching your pull requests…</p>
      <p v-else-if="state.status === 'done' && items.length === 0" class="pr-search-note">No matching pull requests — paste a PR URL</p>
      <p v-else-if="state.status === 'done' && state.partial" class="pr-search-note warn">GitHub returned a partial result; some of your pull requests may be missing.</p>
      <p v-if="state.status === 'failed'" class="pr-search-note error" role="alert">{{ state.message }}</p>
      <form v-if="state.status === 'failed' && state.signInAgain" class="pr-search-signin" action="/auth/login" method="post"><button type="submit" @mousedown.prevent>SIGN IN AGAIN <span aria-hidden="true">↗</span></button></form>
    </div>
  </div>
  <p class="sr-only" role="status" aria-live="polite">{{ announcement }}</p>
</template>
