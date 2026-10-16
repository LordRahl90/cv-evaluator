<script setup lang="ts">
// /matches: every job the user's searches found, scored against their CV.
// Filters live in the URL (?search=&min=&status=&sort=) so they survive a
// reload and the back button; "Load more" follows the API's cursor.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter, type LocationQueryRaw } from 'vue-router'
import AppIcon from '../components/AppIcon'
import AlertMessage from '../components/AlertMessage.vue'
import BusyButton from '../components/BusyButton.vue'
import MatchRow from '../components/MatchRow.vue'
import {
  getResultsSummary,
  listResults,
  type ResultSort,
  type ResultStatus,
  type ResultsSummary,
  type SearchResult,
} from '../api/results'
import { isEvaluating } from '../utils/matches'

const PAGE_SIZE = 20

const route = useRoute()
const router = useRouter()

// ── Filters (from the URL) ────────────────────────────────────────────────

type StatusFilter = 'active' | ResultStatus | 'all'

const STATUS_OPTIONS: { value: StatusFilter; label: string }[] = [
  { value: 'active', label: 'Not dismissed' },
  { value: 'new', label: 'New' },
  { value: 'saved', label: 'Saved' },
  { value: 'applied', label: 'Applied' },
  { value: 'dismissed', label: 'Dismissed' },
  { value: 'all', label: 'All' },
]
const MIN_OPTIONS = [
  { value: '', label: 'Any' },
  { value: '70', label: '70+' },
  { value: '80', label: '80+' },
]
const SORT_OPTIONS: { value: ResultSort; label: string }[] = [
  { value: 'score', label: 'Best score' },
  { value: 'newest', label: 'Newest' },
]

const one = (v: unknown) => (typeof v === 'string' ? v : '')

const searchId = computed(() => one(route.query.search))
const minScore = computed(() =>
  ['70', '80'].includes(one(route.query.min)) ? one(route.query.min) : '',
)
const statusFilter = computed<StatusFilter>(() => {
  const v = one(route.query.status)
  return STATUS_OPTIONS.some((o) => o.value === v) ? (v as StatusFilter) : 'active'
})
const sort = computed<ResultSort>(() => (one(route.query.sort) === 'newest' ? 'newest' : 'score'))
const filtered = computed(() => minScore.value !== '' || statusFilter.value !== 'active')

function statuses(filter: StatusFilter): ResultStatus[] {
  if (filter === 'active') return ['new', 'saved', 'applied']
  if (filter === 'all') return []
  return [filter]
}

function setQuery(patch: Record<string, string>) {
  const query: LocationQueryRaw = { ...route.query }
  for (const [key, value] of Object.entries(patch)) query[key] = value || undefined
  router.replace({ query })
}

const onMin = (e: Event) => setQuery({ min: (e.target as HTMLSelectElement).value })
const onStatus = (e: Event) => {
  const v = (e.target as HTMLSelectElement).value
  setQuery({ status: v === 'active' ? '' : v })
}
const onSort = (e: Event) => {
  const v = (e.target as HTMLSelectElement).value
  setQuery({ sort: v === 'score' ? '' : v })
}
const tabQuery = (id: string): LocationQueryRaw => ({ ...route.query, search: id || undefined })
const clearFiltersQuery = computed<LocationQueryRaw>(() => ({
  search: searchId.value || undefined,
}))

// ── Data ──────────────────────────────────────────────────────────────────

const summary = ref<ResultsSummary | null>(null)
const results = ref<SearchResult[]>([])
const total = ref(0)
const hasMore = ref(false)
const status = ref<'loading' | 'ready' | 'error'>('loading')
const error = ref<Error | null>(null)
const loadingMore = ref(false)
const moreError = ref<Error | null>(null)
let controller: AbortController | null = null

const asError = (err: unknown) => (err instanceof Error ? err : new Error(String(err)))
const aborted = (err: unknown) => err instanceof DOMException && err.name === 'AbortError'

function params(startingAfter?: string) {
  return {
    searchId: searchId.value || undefined,
    minScore: minScore.value ? Number(minScore.value) : undefined,
    status: statuses(statusFilter.value),
    sort: sort.value,
    limit: PAGE_SIZE,
    startingAfter,
  }
}

async function loadSummary() {
  try {
    summary.value = await getResultsSummary()
  } catch {
    // The tabs and counts are extras; the list still works without them.
    summary.value = null
  }
}

async function load() {
  controller?.abort()
  controller = new AbortController()
  const { signal } = controller
  status.value = 'loading'
  error.value = null
  moreError.value = null
  try {
    const page = await listResults(params(), { signal })
    results.value = page.data
    total.value = page.total_count
    hasMore.value = page.has_more
    status.value = 'ready'
  } catch (err) {
    if (aborted(err)) return
    error.value = asError(err)
    status.value = 'error'
  }
}

async function loadMore() {
  const last = results.value[results.value.length - 1]
  if (!last || loadingMore.value) return
  loadingMore.value = true
  moreError.value = null
  try {
    const page = await listResults(params(last.id), { signal: controller?.signal })
    results.value = [...results.value, ...page.data]
    total.value = page.total_count
    hasMore.value = page.has_more
  } catch (err) {
    if (!aborted(err)) moreError.value = asError(err)
  } finally {
    loadingMore.value = false
  }
}

function refresh() {
  loadSummary()
  load()
}

onMounted(refresh)
onBeforeUnmount(() => controller?.abort())
watch(
  () => [searchId.value, minScore.value, statusFilter.value, sort.value],
  () => load(),
)

// ── Derived display ───────────────────────────────────────────────────────

const searches = computed(() => summary.value?.by_search ?? [])
const currentSearch = computed(() => searches.value.find((s) => s.id === searchId.value) ?? null)

const subtitle = computed(() => {
  const s = summary.value
  if (!s || s.scored === 0) return 'Every job your searches found, scored against your active CV.'
  const scored = `${s.scored} ${s.scored === 1 ? 'job' : 'jobs'} scored against your CV`
  return `${scored} · ${s.new_this_week} new this week`
})

// Sorted by score, unscored results come last: if the first one has no
// score yet, nothing in view has been scored.
const inProgress = computed(
  () =>
    results.value[0]?.evaluation === null &&
    results.value.some((r) => isEvaluating(r.evaluation_status)),
)

type Empty = { title: string; text: string; action?: 'new-search' | 'clear' }
const empty = computed<Empty | null>(() => {
  if (status.value !== 'ready' || results.value.length > 0) return null
  if (filtered.value) {
    return {
      title: 'No matches for these filters',
      text: 'Try a lower minimum score or another status.',
      action: 'clear',
    }
  }
  if (currentSearch.value || searchId.value) {
    return {
      title: 'No jobs found yet',
      text: "This search hasn't found any jobs yet. Matches appear here after it runs.",
    }
  }
  if (summary.value && summary.value.by_search.length === 0) {
    return {
      title: 'No matches yet',
      text: "Add a job search and we'll score every job it finds against your CV.",
      action: 'new-search',
    }
  }
  return { title: 'No matches yet', text: 'Matches appear here after your first search runs.' }
})
</script>

<template>
  <div class="page">
    <div class="page-stack matches-page">
      <div class="page-head">
        <div class="page-head-text">
          <h1 class="page-title">Matches</h1>
          <p class="page-sub">{{ subtitle }}</p>
        </div>
      </div>

      <nav v-if="searches.length" class="match-tabs" aria-label="Filter by search">
        <RouterLink
          :to="{ query: tabQuery('') }"
          class="match-tab"
          :class="{ 'is-active': !searchId }"
          :aria-current="!searchId ? 'page' : undefined"
        >
          All searches <span v-if="summary" class="match-tab-count">{{ summary.total }}</span>
        </RouterLink>
        <RouterLink
          v-for="s in searches"
          :key="s.id"
          :to="{ query: tabQuery(s.id) }"
          class="match-tab"
          :class="{ 'is-active': searchId === s.id }"
          :aria-current="searchId === s.id ? 'page' : undefined"
        >
          {{ s.name }} <span class="match-tab-count">{{ s.total }}</span>
        </RouterLink>
      </nav>

      <div class="match-filters">
        <div class="match-filter">
          <label class="match-filter-label" for="match-min">Min score</label>
          <select id="match-min" class="match-select" :value="minScore" @change="onMin">
            <option v-for="o in MIN_OPTIONS" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
        </div>
        <div class="match-filter">
          <label class="match-filter-label" for="match-status">Status</label>
          <select id="match-status" class="match-select" :value="statusFilter" @change="onStatus">
            <option v-for="o in STATUS_OPTIONS" :key="o.value" :value="o.value">
              {{ o.label }}
            </option>
          </select>
        </div>
        <div class="match-filter">
          <label class="match-filter-label" for="match-sort">Sort</label>
          <select id="match-sort" class="match-select" :value="sort" @change="onSort">
            <option v-for="o in SORT_OPTIONS" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
        </div>
        <span
          v-if="status === 'ready' && results.length"
          class="match-count"
          role="status"
          data-testid="match-count"
        >
          Showing {{ results.length }} of {{ total }}
        </span>
      </div>

      <!-- Loading -->
      <section
        v-if="status === 'loading'"
        class="card match-list"
        aria-label="Matches loading"
        aria-busy="true"
        data-testid="matches-loading"
      >
        <span class="sr-only" role="status">Loading matches…</span>
        <div v-for="n in 4" :key="n" class="match-skeleton" aria-hidden="true">
          <span class="match-skeleton-ring" />
          <div class="match-skeleton-body">
            <span class="skeleton skeleton-line skeleton-w-40" />
            <span class="skeleton skeleton-line skeleton-w-24" />
          </div>
        </div>
      </section>

      <!-- Error -->
      <section v-else-if="status === 'error'" class="card error-card">
        <AlertMessage>We couldn't load your matches. {{ error?.message }}</AlertMessage>
        <button type="button" class="btn btn-secondary" @click="refresh">
          <AppIcon name="refresh" />Try again
        </button>
      </section>

      <!-- Empty -->
      <section
        v-else-if="empty"
        class="card empty-state empty-state-page"
        data-testid="matches-empty-state"
      >
        <span class="empty-state-icon"><AppIcon name="star" /></span>
        <h2 class="empty-state-title">{{ empty.title }}</h2>
        <p class="empty-state-text">{{ empty.text }}</p>
        <RouterLink v-if="empty.action === 'new-search'" to="/searches/new" class="btn btn-primary">
          <AppIcon name="plus" />New search
        </RouterLink>
        <RouterLink
          v-else-if="empty.action === 'clear'"
          :to="{ query: clearFiltersQuery }"
          class="btn btn-secondary"
        >
          Clear filters
        </RouterLink>
      </section>

      <!-- Results -->
      <template v-else>
        <section
          v-if="inProgress"
          class="card match-progress"
          aria-labelledby="match-progress-title"
          data-testid="matches-in-progress"
        >
          <div class="match-progress-head">
            <h2 id="match-progress-title" class="match-progress-title">Scoring in progress</h2>
            <span class="badge badge-amber">
              {{ total }} {{ total === 1 ? 'job' : 'jobs' }} found
            </span>
          </div>
          <p class="match-progress-text">
            You can leave this page. Matches appear here as each job is scored.
          </p>
          <ol class="match-stages">
            <li class="match-stage">
              <span class="match-stage-dot is-done"><AppIcon name="check" /></span>
              <div class="match-stage-body">
                <div class="match-stage-title">Found jobs on the portals</div>
                <div class="match-stage-note">
                  {{ total }} {{ total === 1 ? 'job' : 'jobs' }} so far
                </div>
              </div>
            </li>
            <li class="match-stage">
              <span class="match-stage-dot is-active"><AppIcon name="clock" /></span>
              <div class="match-stage-body">
                <div class="match-stage-title">Scoring against your CV</div>
                <div class="match-stage-bar" aria-hidden="true"><span /></div>
              </div>
            </li>
          </ol>
          <button type="button" class="btn btn-secondary match-progress-check" @click="refresh">
            <AppIcon name="refresh" />Check again
          </button>
        </section>

        <section class="card match-list" aria-label="Matches">
          <MatchRow v-for="r in results" :key="r.id" :result="r" />
        </section>

        <AlertMessage v-if="moreError">
          We couldn't load more matches. {{ moreError.message }}
        </AlertMessage>
        <div v-if="hasMore" class="match-more">
          <BusyButton
            type="button"
            variant="secondary"
            :busy="loadingMore"
            busy-label="Loading…"
            @click="loadMore"
          >
            Load more matches
          </BusyButton>
        </div>
      </template>
    </div>
  </div>
</template>
