<script setup lang="ts">
import { computed, onMounted } from 'vue'
import AppIcon, { type IconName } from '../components/AppIcon'
import type { JobSearch } from '../api/types'
import AlertMessage from '../components/AlertMessage.vue'
import { useOverview } from '../composables/useOverview'
import { useMatchesOverview } from '../composables/useMatchesOverview'
import ScoreRing from '../components/ScoreRing.vue'
import { dotted, providerLabel } from '../utils/matches'
import { capitalise, formatDay, formatRelative, formatRunTime, greeting } from '../utils/format'

const {
  status,
  error,
  load,
  user,
  cvs,
  searches,
  hasCV,
  hasSearch,
  stepsDone,
  setupComplete,
  latestCV,
  activeSearches,
  pausedCount,
  nextRunAt,
  lastRun,
} = useOverview()

const matches = useMatchesOverview()
const matchSummary = matches.summary

onMounted(() => {
  load()
  matches.load()
})

const firstName = computed(() => user.value?.first_name?.trim() || '')

const title = computed(() => {
  const name = firstName.value
  if (!setupComplete.value) return name ? `Welcome, ${name}` : 'Welcome'
  return name ? `${greeting()}, ${name}` : greeting()
})

const subtitle = computed(() => {
  if (!setupComplete.value) {
    const left = 3 - stepsDone.value
    return `${left === 1 ? 'One more step' : 'Two more steps'} and your first scored jobs start arriving.`
  }
  const fresh = matchSummary.value?.new_this_week ?? 0
  if (fresh > 0) return `${plural(fresh, 'new match', 'new matches')} this week.`
  const n = activeSearches.value.length
  if (n === 0) return 'All your searches are paused. Resume one to keep new jobs coming.'
  return `${n} active ${n === 1 ? 'search is' : 'searches are'} looking for new jobs on your schedule.`
})

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

interface Step {
  key: string
  title: string
  text: string
  done: boolean
  action?: { to: string; label: string; icon: IconName; primary: boolean }
}

// The first unfinished step gets the primary button, as in the design.
const steps = computed<Step[]>(() => [
  {
    key: 'account',
    title: 'Create your account',
    text: 'Done. Welcome aboard.',
    done: true,
  },
  {
    key: 'cv',
    title: 'Upload your CV',
    text: hasCV.value
      ? `Done. ${plural(cvs.value.length, 'CV', 'CVs')} on file.`
      : 'A PDF. We extract it and split it into sections so jobs can be matched against each part.',
    done: hasCV.value,
    action: { to: '/cvs', label: 'Upload CV', icon: 'upload', primary: true },
  },
  {
    key: 'search',
    title: 'Add a job search',
    text: hasSearch.value
      ? `Done. ${plural(searches.value.length, 'search', 'searches')} saved.`
      : 'Choose a role and a location. We check every portal we support on your schedule.',
    done: hasSearch.value,
    action: { to: '/searches/new', label: 'New search', icon: 'plus', primary: hasCV.value },
  },
])

const searchStat = computed(() => {
  const active = activeSearches.value.length
  let note = 'None yet'
  if (active > 0)
    note = nextRunAt.value
      ? `Next run ${formatRelative(nextRunAt.value)}`
      : 'Waiting for the scheduler'
  else if (pausedCount.value > 0) note = `${pausedCount.value} paused`
  return { value: active, note }
})

const cvNote = computed(() => {
  if (!latestCV.value) return 'Upload one to start'
  const more = cvs.value.length > 1 ? ` · ${cvs.value.length} on file` : ''
  return `Updated ${formatDay(latestCV.value.updated_at)}${more}`
})

const runStat = computed(() => {
  if (lastRun.value) {
    return {
      value: formatRelative(lastRun.value.last_run_at),
      note: lastRun.value.name,
      empty: false,
    }
  }
  return {
    value: 'Not yet',
    note: hasSearch.value ? 'The first run starts shortly' : 'Runs start after you add a search',
    empty: true,
  }
})

const newMatchesNote = computed(() => {
  const n = matchSummary.value?.new_this_week_searches ?? 0
  if (!matchSummary.value?.new_this_week) return 'None found this week'
  return `Across ${plural(n, 'search', 'searches')}`
})

const strongNote = computed(() => {
  const s = matchSummary.value
  if (!s || s.strong_matches === 0) return `Scored ${s?.strong_score ?? 80} or more`
  return s.strong_matches_new > 0 ? `${s.strong_matches_new} still new` : 'All triaged'
})

const recentSearches = computed(() => searches.value.slice(0, 5))

function searchDetail(s: JobSearch): string {
  const freq = capitalise(s.frequency)
  if (s.last_run_at) return `${freq} · ran ${formatRunTime(s.last_run_at)}`
  if (s.next_run_at) return `${freq} · next run ${formatRelative(s.next_run_at)}`
  return `${freq} · waiting for its first run`
}
</script>

<template>
  <div class="page">
    <!-- Loading -->
    <div v-if="status === 'loading'" class="page-stack" role="status" aria-live="polite">
      <span class="sr-only">Loading your overview…</span>
      <div class="page-head" aria-hidden="true">
        <div class="page-head-text">
          <span class="skeleton skeleton-title" />
          <span class="skeleton skeleton-line skeleton-w-80" />
        </div>
      </div>
      <div class="card skeleton-card" aria-hidden="true" data-testid="overview-skeleton">
        <span class="skeleton skeleton-line skeleton-w-32" />
        <span class="skeleton skeleton-bar" />
        <span class="skeleton skeleton-row" />
        <span class="skeleton skeleton-row" />
        <span class="skeleton skeleton-row" />
      </div>
      <div class="stat-grid" aria-hidden="true">
        <div v-for="n in 3" :key="n" class="card stat">
          <span class="skeleton skeleton-line skeleton-w-40" />
          <span class="skeleton skeleton-value" />
          <span class="skeleton skeleton-line skeleton-w-56" />
        </div>
      </div>
    </div>

    <!-- Error -->
    <div v-else-if="status === 'error'" class="page-stack">
      <div class="page-head">
        <div class="page-head-text">
          <h1 class="page-title">Overview</h1>
        </div>
      </div>
      <section class="card error-card">
        <AlertMessage>We couldn't load your overview. {{ error?.message }}</AlertMessage>
        <button type="button" class="btn btn-secondary" @click="load">
          <AppIcon name="refresh" />Try again
        </button>
      </section>
    </div>

    <!-- Ready -->
    <div v-else class="page-stack">
      <div class="page-head">
        <div class="page-head-text">
          <h1 class="page-title">{{ title }}</h1>
          <p class="page-sub">{{ subtitle }}</p>
        </div>
        <div v-if="setupComplete" class="page-actions">
          <RouterLink to="/quick-match" class="btn btn-secondary">
            <AppIcon name="bolt" />Quick match
          </RouterLink>
          <RouterLink to="/searches/new" class="btn btn-primary">
            <AppIcon name="plus" />New search
          </RouterLink>
        </div>
      </div>

      <section v-if="!setupComplete" class="card setup" aria-labelledby="setup-title">
        <div class="setup-head">
          <h2 id="setup-title" class="section-title">Get set up</h2>
          <span class="badge badge-grey">{{ stepsDone }} of 3 done</span>
        </div>
        <div class="setup-progress-wrap">
          <progress
            class="progress"
            :value="stepsDone"
            max="3"
            :aria-label="`${stepsDone} of 3 setup steps done`"
          />
        </div>
        <ol class="checklist">
          <li
            v-for="(step, i) in steps"
            :key="step.key"
            class="check"
            :class="{ 'is-done': step.done }"
            :data-testid="`step-${step.key}`"
          >
            <span class="check-num" :class="{ 'is-done': step.done }">
              <AppIcon v-if="step.done" name="check" />
              <template v-else>{{ i + 1 }}</template>
              <span class="sr-only">{{ step.done ? '(done)' : '(to do)' }}</span>
            </span>
            <div class="check-body">
              <div class="check-title">{{ step.title }}</div>
              <div class="check-text">{{ step.text }}</div>
            </div>
            <RouterLink
              v-if="!step.done && step.action"
              :to="step.action.to"
              class="btn"
              :class="step.action.primary ? 'btn-primary' : 'btn-secondary'"
            >
              <AppIcon :name="step.action.icon" />{{ step.action.label }}
            </RouterLink>
          </li>
        </ol>
      </section>

      <section class="stat-grid" aria-label="Activity summary">
        <!-- Match counts, once there is something to match (GET /results/summary). -->
        <template v-if="setupComplete">
          <div class="card stat" data-testid="stat-new-matches">
            <div class="stat-label">New matches this week</div>
            <span v-if="matches.status.value === 'loading'" class="skeleton skeleton-value" />
            <div
              v-else
              class="stat-value"
              :class="{ 'is-empty': !matchSummary || matchSummary.new_this_week === 0 }"
            >
              {{ matchSummary ? matchSummary.new_this_week : '–' }}
            </div>
            <div class="stat-note">
              {{ matches.status.value === 'error' ? "Couldn't load" : newMatchesNote }}
            </div>
          </div>
          <div class="card stat" data-testid="stat-strong-matches">
            <div class="stat-label">Strong matches ({{ matchSummary?.strong_score ?? 80 }}+)</div>
            <span v-if="matches.status.value === 'loading'" class="skeleton skeleton-value" />
            <div
              v-else
              class="stat-value"
              :class="
                !matchSummary || matchSummary.strong_matches === 0
                  ? 'is-empty'
                  : 'stat-value-accent'
              "
            >
              {{ matchSummary ? matchSummary.strong_matches : '–' }}
            </div>
            <div class="stat-note">
              {{ matches.status.value === 'error' ? "Couldn't load" : strongNote }}
            </div>
          </div>
        </template>
        <div class="card stat" data-testid="stat-searches">
          <div class="stat-label">Active searches</div>
          <div class="stat-value" :class="{ 'is-empty': searchStat.value === 0 }">
            {{ searchStat.value }}
          </div>
          <div class="stat-note">{{ searchStat.note }}</div>
        </div>
        <div class="card stat" data-testid="stat-cv">
          <div class="stat-label">Active CV</div>
          <div v-if="latestCV" class="stat-text">CV uploaded</div>
          <div v-else class="stat-value stat-value-sm is-empty">Not set</div>
          <div class="stat-note">{{ cvNote }}</div>
        </div>
        <div v-if="!setupComplete" class="card stat" data-testid="stat-last-run">
          <div class="stat-label">Last search run</div>
          <div v-if="runStat.empty" class="stat-value stat-value-sm is-empty">
            {{ runStat.value }}
          </div>
          <div v-else class="stat-text">{{ runStat.value }}</div>
          <div class="stat-note">{{ runStat.note }}</div>
        </div>
      </section>

      <div v-if="setupComplete" class="overview-columns">
        <section class="card overview-matches" aria-labelledby="matches-title">
          <div class="section-head">
            <h2 id="matches-title" class="section-title">Top new matches</h2>
            <RouterLink to="/matches" class="link-strong">See all matches</RouterLink>
          </div>
          <div
            v-if="matches.status.value === 'loading'"
            class="overview-match-list"
            aria-busy="true"
            data-testid="matches-loading"
          >
            <span class="sr-only" role="status">Loading matches…</span>
            <div v-for="n in 3" :key="n" class="overview-match-skeleton" aria-hidden="true">
              <span class="skeleton overview-match-skeleton-ring" />
              <span class="skeleton skeleton-line skeleton-w-40" />
            </div>
          </div>
          <div v-else-if="matches.status.value === 'error'" class="overview-match-error">
            <AlertMessage>
              We couldn't load your matches. {{ matches.error.value?.message }}
            </AlertMessage>
            <button type="button" class="btn btn-secondary" @click="matches.load">
              <AppIcon name="refresh" />Try again
            </button>
          </div>
          <ul v-else-if="matches.top.value.length" class="overview-match-list">
            <li v-for="r in matches.top.value" :key="r.id">
              <RouterLink :to="`/matches/${r.id}`" class="overview-match" data-testid="top-match">
                <ScoreRing :score="r.evaluation?.overall_score ?? null" />
                <div class="overview-match-body">
                  <div class="overview-match-title">{{ r.job.title || 'Untitled job' }}</div>
                  <div class="overview-match-meta">{{ dotted(r.job.company, r.job.location) }}</div>
                </div>
                <span class="badge badge-grey">{{ providerLabel(r.job.provider) }}</span>
                <span class="overview-match-time">{{ formatRelative(r.created_at) }}</span>
              </RouterLink>
            </li>
          </ul>
          <div v-else class="empty-state" data-testid="matches-empty">
            <span class="empty-state-icon"><AppIcon name="star" /></span>
            <template v-if="matchSummary && matchSummary.total > 0">
              <div class="empty-state-title">No new matches this week</div>
              <p class="empty-state-text">
                {{
                  matchSummary.evaluating > 0
                    ? `${plural(matchSummary.evaluating, 'job is', 'jobs are')} still being scored.`
                    : 'Your earlier matches are on the Matches page.'
                }}
              </p>
            </template>
            <template v-else>
              <div class="empty-state-title">No matches to show yet</div>
              <p class="empty-state-text">Matches appear here after your first search runs.</p>
            </template>
          </div>
        </section>

        <section class="card overview-activity" aria-labelledby="activity-title">
          <div class="section-head">
            <h2 id="activity-title" class="section-title">Search activity</h2>
            <RouterLink to="/searches" class="link-strong">Manage</RouterLink>
          </div>
          <ul class="activity-list">
            <li v-for="s in recentSearches" :key="s.id" class="activity-item">
              <template v-if="s.enabled">
                <div class="activity-row">
                  <span class="activity-name">{{ s.name }}</span>
                  <span class="badge badge-green">Active</span>
                </div>
                <div class="activity-detail">{{ searchDetail(s) }}</div>
              </template>
              <div v-else class="activity-row activity-row-paused">
                <span class="badge badge-amber">Paused</span>
                <span class="activity-detail">{{ s.name }}</span>
              </div>
            </li>
          </ul>
        </section>
      </div>

      <section v-else class="card promo">
        <span class="promo-icon"><AppIcon name="bolt" /></span>
        <div class="promo-body">
          <div class="promo-title">Have one job in mind already?</div>
          <div class="promo-text">
            Paste a job description into Quick match for an instant score, once your CV is uploaded.
          </div>
        </div>
        <RouterLink to="/quick-match" class="btn btn-secondary">Try Quick match</RouterLink>
      </section>
    </div>
  </div>
</template>
