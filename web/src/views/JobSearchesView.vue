<script setup lang="ts">
// Job searches (/searches): the saved searches with their schedule, switch
// and actions, an empty state that lists the portals every search covers,
// and the "Job portals" suggestion section (#suggest).
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import AlertMessage from '../components/AlertMessage.vue'
import AppIcon from '../components/AppIcon'
import JobSearchCard from '../components/JobSearchCard.vue'
import PortalLogo from '../components/PortalLogo.vue'
import PortalSuggestions from '../components/PortalSuggestions.vue'
import { listCVs } from '../api/cvs'
import { listJobSearches } from '../api/jobSearches'
import { listProviders, type Provider } from '../api/providers'
import type { JobSearch } from '../api/types'

type LoadStatus = 'loading' | 'ready' | 'error'

const route = useRoute()

const status = ref<LoadStatus>('loading')
const error = ref('')
const searches = ref<JobSearch[]>([])

const providersStatus = ref<LoadStatus>('loading')
const providers = ref<Provider[]>([])

// null until known; the "upload a CV first" note only shows when it's false.
const hasCV = ref<boolean | null>(null)

const notice = ref('')
const heading = ref<HTMLElement | null>(null)

async function loadSearches() {
  status.value = 'loading'
  error.value = ''
  try {
    searches.value = await listJobSearches()
    status.value = 'ready'
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
    status.value = 'error'
  }
}

async function loadProviders() {
  providersStatus.value = 'loading'
  try {
    providers.value = await listProviders()
    providersStatus.value = 'ready'
  } catch {
    providersStatus.value = 'error'
  }
}

async function loadCVs() {
  try {
    hasCV.value = (await listCVs()).length > 0
  } catch {
    hasCV.value = null
  }
}

const running = computed(() => searches.value.filter((s) => s.enabled).length)
const subtitle = computed(() => {
  const n = searches.value.length
  return `${n} ${n === 1 ? 'search' : 'searches'} · ${running.value} running · checked on their own schedule`
})

function onUpdate(updated: JobSearch) {
  searches.value = searches.value.map((s) => (s.id === updated.id ? updated : s))
}

async function onDeleted(deleted: JobSearch) {
  searches.value = searches.value.filter((s) => s.id !== deleted.id)
  notice.value = `Deleted ${deleted.name}.`
  await nextTick()
  heading.value?.focus()
}

// Links to /searches#suggest land on the suggestion section once it's there.
async function revealHash() {
  if (route.hash !== '#suggest' || status.value !== 'ready') return
  await nextTick()
  // Wait a frame so this runs after the router's own scroll-to-top.
  await new Promise((resolve) => requestAnimationFrame(resolve))
  const el = document.getElementById('suggest')
  el?.scrollIntoView({ block: 'start' })
  el?.focus({ preventScroll: true })
}

watch(() => [route.hash, status.value], revealHash)

onMounted(() => {
  const flash = (window.history.state as { flash?: unknown } | null)?.flash
  if (typeof flash === 'string') {
    notice.value = flash
    // Show it once: not again on reload or when coming back to this entry.
    window.history.replaceState({ ...window.history.state, flash: undefined }, '')
  }
  loadSearches()
  loadProviders()
  loadCVs()
})
</script>

<template>
  <div class="page">
    <div class="page-stack searches-page">
      <div class="page-head">
        <div class="page-head-text">
          <h1 ref="heading" class="page-title" tabindex="-1">Job searches</h1>
          <p v-if="status === 'ready' && searches.length" class="page-sub">{{ subtitle }}</p>
          <p v-else class="page-sub">
            Tell us where to look. We check each search on its schedule and score every new job.
          </p>
        </div>
        <div v-if="status === 'ready' && searches.length" class="page-actions">
          <RouterLink to="/searches/new" class="btn btn-primary">
            <AppIcon name="plus" />New search
          </RouterLink>
        </div>
      </div>

      <AlertMessage v-if="notice" tone="info" data-testid="searches-notice">{{
        notice
      }}</AlertMessage>

      <!-- Loading -->
      <div v-if="status === 'loading'" class="searches-list" role="status" aria-live="polite">
        <span class="sr-only">Loading your job searches…</span>
        <div
          v-for="n in 3"
          :key="n"
          class="card search-card-skeleton"
          aria-hidden="true"
          data-testid="searches-skeleton"
        >
          <span class="skeleton search-card-skeleton-icon" />
          <div class="search-card-skeleton-body">
            <span class="skeleton skeleton-line skeleton-w-32" />
            <span class="skeleton skeleton-line skeleton-w-80" />
          </div>
          <span class="skeleton search-card-skeleton-switch" />
        </div>
      </div>

      <!-- Error -->
      <section v-else-if="status === 'error'" class="card error-card">
        <AlertMessage>We couldn't load your job searches. {{ error }}</AlertMessage>
        <button type="button" class="btn btn-secondary" @click="loadSearches">
          <AppIcon name="refresh" />Try again
        </button>
      </section>

      <!-- Empty -->
      <template v-else-if="!searches.length">
        <section class="card" aria-labelledby="searches-empty-title" data-testid="searches-empty">
          <div class="searches-empty">
            <span class="searches-empty-icon" aria-hidden="true"><AppIcon name="search" /></span>
            <h2 id="searches-empty-title" class="searches-empty-title">No job searches yet</h2>
            <p class="searches-empty-text">
              A search is a role and a location, like "Senior Go Engineer in Copenhagen, daily".
              Each run checks every job portal we support, so you never have to pick one.
            </p>
            <RouterLink to="/searches/new" class="btn btn-primary searches-empty-cta">
              <AppIcon name="plus" />Create your first search
            </RouterLink>
          </div>
          <div class="searches-covers">
            <h3 class="searches-covers-label">Every search covers</h3>
            <ul class="searches-covers-grid">
              <template v-if="providersStatus === 'loading'">
                <li v-for="n in 2" :key="n" class="searches-portal" aria-hidden="true">
                  <span class="skeleton searches-portal-logo-skeleton" />
                  <span class="skeleton skeleton-line skeleton-w-24" />
                </li>
              </template>
              <li v-else-if="providersStatus === 'error'" class="searches-portal">
                <span class="searches-portal-text">
                  We couldn't load the portal list.
                  <button type="button" class="link-button" @click="loadProviders">
                    Try again
                  </button>
                </span>
              </li>
              <template v-else>
                <li v-for="p in providers" :key="p.name" class="searches-portal">
                  <PortalLogo :name="p.name" :display-name="p.display_name" />
                  <div>
                    <div class="searches-portal-name">{{ p.display_name }}</div>
                    <div class="searches-portal-coverage">{{ p.coverage }}</div>
                  </div>
                </li>
              </template>
              <li>
                <RouterLink
                  :to="{ path: '/searches', hash: '#suggest' }"
                  class="searches-portal searches-portal-link"
                >
                  <span class="portal-logo portal-logo-md portal-logo-add" aria-hidden="true">
                    <AppIcon name="plus" />
                  </span>
                  <div>
                    <div class="searches-portal-name">Missing a job site?</div>
                    <div class="searches-portal-coverage">Suggest a portal and we'll add it</div>
                  </div>
                </RouterLink>
              </li>
            </ul>
          </div>
        </section>

        <section v-if="hasCV === false" class="card searches-cv-note" aria-label="Before you start">
          <AppIcon name="alert" />
          <p class="searches-cv-note-text">
            Jobs are scored against your active CV. You haven't uploaded one yet, so searches will
            collect jobs but hold off scoring them.
          </p>
          <RouterLink to="/cvs" class="btn btn-secondary">Upload CV</RouterLink>
        </section>
      </template>

      <!-- List -->
      <div v-else class="searches-list">
        <JobSearchCard
          v-for="s in searches"
          :key="s.id"
          :search="s"
          @update="onUpdate"
          @deleted="onDeleted"
        />
      </div>

      <PortalSuggestions
        v-if="status === 'ready'"
        :providers="providers"
        :providers-status="providersStatus"
        @retry-providers="loadProviders"
      />
    </div>
  </div>
</template>
