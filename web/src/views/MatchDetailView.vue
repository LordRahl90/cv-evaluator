<script setup lang="ts">
// /matches/:id: one match with its score, breakdown, requirements,
// strengths, gaps and summary, and the user's actions on it. Actions update
// the page straight away and roll back if the API refuses.
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import AppIcon from '../components/AppIcon'
import AlertMessage from '../components/AlertMessage.vue'
import BusyButton from '../components/BusyButton.vue'
import ScoreRing from '../components/ScoreRing.vue'
import { ApiError } from '../api/client'
import { listCVs } from '../api/cvs'
import type { CVSummary } from '../api/types'
import {
  getResult,
  reEvaluateResult,
  updateResultStatus,
  type Requirement,
  type ResultStatus,
  type SearchResultDetail,
} from '../api/results'
import { formatDay, formatRelative } from '../utils/format'
import {
  STATUS_BADGES,
  STATUS_LABELS,
  STRONG_SCORE,
  criterionLabel,
  dotted,
  evaluationLabel,
  formatDateTime,
  isEvaluating,
  providerLabel,
  scoreTone,
} from '../utils/matches'

const DESCRIPTION_PREVIEW = 480

const route = useRoute()
const id = computed(() => String(route.params.id ?? ''))

const detail = ref<SearchResultDetail | null>(null)
const cvs = ref<CVSummary[]>([])
const status = ref<'loading' | 'ready' | 'error'>('loading')
const error = ref<Error | null>(null)
const notFound = ref(false)

async function load() {
  status.value = 'loading'
  error.value = null
  notFound.value = false
  actionError.value = ''
  try {
    const [result, cvList] = await Promise.all([
      getResult(id.value),
      // Only used to describe the CV that was scored; never fatal.
      listCVs().catch(() => [] as CVSummary[]),
    ])
    detail.value = result
    cvs.value = cvList
    status.value = 'ready'
  } catch (err) {
    notFound.value = err instanceof ApiError && (err.status === 404 || err.status === 400)
    error.value = err instanceof Error ? err : new Error(String(err))
    status.value = 'error'
  }
}

onMounted(load)
watch(id, load)

// ── Actions (optimistic, with rollback) ───────────────────────────────────

type Busy = 'save' | 'apply' | 'dismiss' | 'reevaluate' | null
const busy = ref<Busy>(null)
const actionError = ref('')
const announcement = ref('')

const DONE_MESSAGES: Record<ResultStatus, string> = {
  new: 'Moved back to new.',
  saved: 'Saved.',
  applied: 'Marked as applied.',
  dismissed: 'Dismissed.',
}

async function setStatus(target: ResultStatus, key: Exclude<Busy, null>) {
  const current = detail.value
  if (!current || busy.value) return
  const previous = current.status
  current.status = target
  busy.value = key
  actionError.value = ''
  announcement.value = ''
  try {
    detail.value = await updateResultStatus(current.id, target)
    announcement.value = DONE_MESSAGES[target]
  } catch (err) {
    current.status = previous
    actionError.value = `We couldn't update this match. ${(err as Error).message}`
  } finally {
    busy.value = null
  }
}

async function reEvaluate() {
  const current = detail.value
  if (!current || busy.value) return
  const previous = current.evaluation_status
  current.evaluation_status = 'pending'
  busy.value = 'reevaluate'
  actionError.value = ''
  announcement.value = ''
  try {
    detail.value = await reEvaluateResult(current.id)
    announcement.value = 'Queued for scoring. The new score appears here when it is ready.'
  } catch (err) {
    if (err instanceof ApiError && err.status === 409) {
      // Already queued or being scored: the optimistic state is right.
      announcement.value = 'This job is already being scored.'
    } else {
      current.evaluation_status = previous
      actionError.value = `We couldn't re-evaluate this match. ${(err as Error).message}`
    }
  } finally {
    busy.value = null
  }
}

const toggleSave = () => setStatus(detail.value?.status === 'saved' ? 'new' : 'saved', 'save')
const toggleApplied = () =>
  setStatus(detail.value?.status === 'applied' ? 'new' : 'applied', 'apply')
const toggleDismissed = () =>
  setStatus(detail.value?.status === 'dismissed' ? 'new' : 'dismissed', 'dismiss')

// ── Derived display ───────────────────────────────────────────────────────

const evaluation = computed(() => detail.value?.evaluation ?? null)
const score = computed(() => evaluation.value?.overall_score ?? null)
const evaluating = computed(() =>
  detail.value ? isEvaluating(detail.value.evaluation_status) : false,
)
const evalBadge = computed(() =>
  detail.value ? evaluationLabel(detail.value.evaluation_status, score.value !== null) : null,
)

const meta = computed(() => {
  const d = detail.value
  if (!d) return ''
  const found = `found on ${providerLabel(d.job.provider)} ${formatRelative(d.created_at)}`
  return dotted(d.job.company, d.job.location, found)
})

const triageLabel = computed(() => {
  const s = detail.value?.status
  if (s === 'applied') return 'Applied'
  if (s === 'dismissed') return 'Dismissed'
  if (s === 'saved') return 'Saved, not applied yet'
  return 'Not applied yet'
})

const cvUsed = computed(() => {
  const e = evaluation.value
  if (!e) return null
  const cv = cvs.value.find((c) => c.id === e.cv_id)
  if (!cv) return null
  const latest = [...cvs.value].sort(
    (a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime(),
  )[0]
  return `Uploaded ${formatDay(cv.created_at)}${latest?.id === cv.id ? ' (active)' : ''}`
})

const REQUIREMENT_ICON = { yes: 'check', partial: 'clock', no: 'alert' } as const
const REQUIREMENT_MET = { yes: 'Met', partial: 'Partly met', no: 'Not met' } as const
const requirementKey = (r: Requirement, i: number) => `${i}-${r.requirement}`

const description = computed(() => detail.value?.job.description?.trim() ?? '')
const showFullDescription = ref(false)
const longDescription = computed(() => description.value.length > DESCRIPTION_PREVIEW)
const visibleDescription = computed(() =>
  longDescription.value && !showFullDescription.value
    ? `${description.value.slice(0, DESCRIPTION_PREVIEW).trimEnd()}…`
    : description.value,
)
</script>

<template>
  <div class="page">
    <div class="page-stack match-detail">
      <RouterLink to="/matches" class="match-back"><AppIcon name="arrowLeft" />Matches</RouterLink>

      <!-- Loading -->
      <div v-if="status === 'loading'" class="match-detail-stack" data-testid="match-loading">
        <span class="sr-only" role="status">Loading the match…</span>
        <div class="card skeleton-card" aria-hidden="true">
          <span class="skeleton skeleton-title" />
          <span class="skeleton skeleton-line skeleton-w-56" />
        </div>
        <div class="card skeleton-card" aria-hidden="true">
          <span class="skeleton skeleton-line skeleton-w-32" />
          <span class="skeleton skeleton-row" />
          <span class="skeleton skeleton-row" />
        </div>
      </div>

      <!-- Not found / error -->
      <section v-else-if="status === 'error' && notFound" class="card empty-state empty-state-page">
        <span class="empty-state-icon"><AppIcon name="search" /></span>
        <h1 class="empty-state-title">We couldn't find this match</h1>
        <p class="empty-state-text">It may belong to a search you deleted.</p>
        <RouterLink to="/matches" class="btn btn-secondary">
          <AppIcon name="arrowLeft" />Back to matches
        </RouterLink>
      </section>
      <section v-else-if="status === 'error'" class="card error-card">
        <AlertMessage>We couldn't load this match. {{ error?.message }}</AlertMessage>
        <button type="button" class="btn btn-secondary" @click="load">
          <AppIcon name="refresh" />Try again
        </button>
      </section>

      <!-- Ready -->
      <template v-else-if="detail">
        <section class="card match-hero" aria-labelledby="match-title">
          <ScoreRing :score="score" size="lg" caption="overall" />
          <div class="match-hero-body">
            <div class="match-hero-badges">
              <span v-if="score !== null && score >= STRONG_SCORE" class="badge badge-green">
                Strong match
              </span>
              <span class="badge badge-grey">{{ detail.search.name }}</span>
              <span
                v-if="detail.status !== 'new'"
                class="badge"
                :class="STATUS_BADGES[detail.status]"
                data-testid="match-status"
              >
                {{ STATUS_LABELS[detail.status] }}
              </span>
              <span v-if="evalBadge" class="badge badge-amber" data-testid="match-eval-status">
                {{ evalBadge }}
              </span>
            </div>
            <h1 id="match-title" class="match-hero-title">
              {{ detail.job.title || 'Untitled job' }}
            </h1>
            <div class="match-hero-meta">{{ meta }}</div>
          </div>
          <div class="match-hero-actions">
            <a
              v-if="detail.job.details_url"
              :href="detail.job.details_url"
              class="btn btn-secondary"
              target="_blank"
              rel="noopener noreferrer"
            >
              <AppIcon name="external" />Open posting<span class="sr-only">
                (opens in a new tab)</span
              >
            </a>
            <BusyButton
              type="button"
              variant="secondary"
              :busy="busy === 'save'"
              busy-label="Saving…"
              :aria-pressed="detail.status === 'saved' ? 'true' : 'false'"
              :disabled="busy !== null"
              @click="toggleSave"
            >
              <AppIcon name="bookmark" />{{ detail.status === 'saved' ? 'Saved' : 'Save' }}
            </BusyButton>
            <BusyButton
              type="button"
              variant="primary"
              :busy="busy === 'apply'"
              busy-label="Saving…"
              :aria-pressed="detail.status === 'applied' ? 'true' : 'false'"
              :disabled="busy !== null"
              @click="toggleApplied"
            >
              <AppIcon name="check" />{{ detail.status === 'applied' ? 'Applied' : 'Mark applied' }}
            </BusyButton>
          </div>
        </section>

        <AlertMessage v-if="actionError">{{ actionError }}</AlertMessage>
        <p class="sr-only" role="status" aria-live="polite">{{ announcement }}</p>

        <div class="match-columns">
          <div class="match-main">
            <template v-if="evaluation">
              <section class="card match-section" aria-labelledby="match-summary-title">
                <h2 id="match-summary-title" class="match-section-title">
                  Why it scored {{ evaluation.overall_score }}
                </h2>
                <p v-if="evaluation.summary" class="match-summary">{{ evaluation.summary }}</p>
                <p v-else class="match-muted">The evaluation came without a summary.</p>
              </section>

              <section
                v-if="evaluation.breakdown.length"
                class="card match-section"
                aria-labelledby="match-breakdown-title"
              >
                <h2 id="match-breakdown-title" class="match-section-title">Score breakdown</h2>
                <ul class="match-metrics">
                  <li
                    v-for="b in evaluation.breakdown"
                    :key="b.criterion"
                    class="match-metric"
                    data-testid="match-metric"
                  >
                    <span class="match-metric-label">{{ criterionLabel(b.criterion) }}</span>
                    <span
                      class="match-metric-track"
                      :class="`is-${scoreTone(b.score)}`"
                      :style="{ '--score': b.score }"
                      aria-hidden="true"
                    >
                      <span class="match-metric-fill" />
                    </span>
                    <b class="match-metric-value">{{ b.score }}</b>
                    <span v-if="b.reason" class="match-metric-reason">{{ b.reason }}</span>
                  </li>
                </ul>
              </section>

              <section
                v-if="evaluation.requirements?.length"
                class="card match-section"
                aria-labelledby="match-req-title"
              >
                <h2 id="match-req-title" class="match-section-title">Requirements</h2>
                <ul class="match-reqs">
                  <li
                    v-for="(r, i) in evaluation.requirements"
                    :key="requirementKey(r, i)"
                    class="match-req"
                    :class="`is-${r.met}`"
                    data-testid="match-requirement"
                  >
                    <span class="match-req-icon"><AppIcon :name="REQUIREMENT_ICON[r.met]" /></span>
                    <div class="match-req-body">
                      <div class="match-req-line">
                        <span class="match-req-text">{{ r.requirement }}</span>
                        <span class="badge badge-grey">
                          {{ r.importance === 'must' ? 'Must have' : 'Nice to have' }}
                        </span>
                        <span class="sr-only">{{ REQUIREMENT_MET[r.met] }}</span>
                      </div>
                      <div v-if="r.evidence" class="match-req-evidence">{{ r.evidence }}</div>
                    </div>
                  </li>
                </ul>
              </section>

              <div class="match-lists">
                <section class="card match-section" aria-labelledby="match-str-title">
                  <h2 id="match-str-title" class="match-section-title">Strengths</h2>
                  <ul v-if="evaluation.strengths.length" class="match-items">
                    <li v-for="s in evaluation.strengths" :key="s" class="match-item is-strength">
                      <AppIcon name="check" />{{ s }}
                    </li>
                  </ul>
                  <p v-else class="match-muted">None listed.</p>
                </section>
                <section class="card match-section" aria-labelledby="match-gap-title">
                  <h2 id="match-gap-title" class="match-section-title">Missing skills</h2>
                  <ul v-if="evaluation.missing_skills.length" class="match-items">
                    <li v-for="s in evaluation.missing_skills" :key="s" class="match-item is-gap">
                      <AppIcon name="alert" />{{ s }}
                    </li>
                  </ul>
                  <p v-else class="match-muted">Nothing missing.</p>
                </section>
              </div>
            </template>

            <section
              v-else
              class="card match-section"
              aria-labelledby="match-pending-title"
              data-testid="match-unscored"
            >
              <template v-if="evaluating">
                <h2 id="match-pending-title" class="match-section-title">Scoring in progress</h2>
                <p class="match-muted">
                  This job is waiting to be scored against your CV. You can leave this page; the
                  score appears here when it is ready.
                </p>
                <button type="button" class="btn btn-secondary match-section-action" @click="load">
                  <AppIcon name="refresh" />Check again
                </button>
              </template>
              <template v-else>
                <h2 id="match-pending-title" class="match-section-title">
                  We couldn't score this job
                </h2>
                <p class="match-muted">
                  Scoring failed after several tries. Re-evaluate to try again.
                </p>
              </template>
            </section>
          </div>

          <aside class="match-aside">
            <section class="card match-section" aria-labelledby="match-eval-title">
              <h2 id="match-eval-title" class="match-section-title">Evaluation</h2>
              <dl class="match-kv">
                <template v-if="evaluation">
                  <div class="match-kv-row">
                    <dt>CV used</dt>
                    <dd>
                      <RouterLink to="/cvs" class="link-strong">
                        {{ cvUsed ?? 'No longer on file' }}
                      </RouterLink>
                    </dd>
                  </div>
                  <div class="match-kv-row">
                    <dt>Evaluated</dt>
                    <dd>{{ formatDateTime(evaluation.evaluated_at) }}</dd>
                  </div>
                  <div v-if="evaluation.model" class="match-kv-row">
                    <dt>Model</dt>
                    <dd>{{ evaluation.model }}</dd>
                  </div>
                  <div v-if="evaluation.prompt_version" class="match-kv-row">
                    <dt>Prompt</dt>
                    <dd>{{ evaluation.prompt_version }}</dd>
                  </div>
                </template>
                <div class="match-kv-row">
                  <dt>Status</dt>
                  <dd>{{ triageLabel }}</dd>
                </div>
              </dl>
              <BusyButton
                type="button"
                variant="secondary"
                :busy="busy === 'reevaluate'"
                busy-label="Queuing…"
                :disabled="busy !== null || evaluating"
                @click="reEvaluate"
              >
                <AppIcon name="refresh" />{{
                  evaluating ? 'Scoring in progress' : 'Re-evaluate with active CV'
                }}
              </BusyButton>
              <BusyButton
                type="button"
                variant="secondary"
                class="match-dismiss"
                :busy="busy === 'dismiss'"
                busy-label="Saving…"
                :disabled="busy !== null"
                @click="toggleDismissed"
              >
                {{ detail.status === 'dismissed' ? 'Restore this job' : 'Dismiss this job' }}
              </BusyButton>
            </section>

            <section class="card match-section" aria-labelledby="match-desc-title">
              <h2 id="match-desc-title" class="match-section-title">From the posting</h2>
              <p v-if="detail.job.posted_at" class="match-muted">
                Posted {{ detail.job.posted_at }}
              </p>
              <p v-if="description" id="match-description" class="match-description">
                {{ visibleDescription }}
              </p>
              <p v-else class="match-muted">The portal gave no description for this job.</p>
              <button
                v-if="longDescription"
                type="button"
                class="match-link-btn"
                aria-controls="match-description"
                :aria-expanded="showFullDescription ? 'true' : 'false'"
                @click="showFullDescription = !showFullDescription"
              >
                {{ showFullDescription ? 'Show less' : 'Read the full description' }}
              </button>
            </section>
          </aside>
        </div>
      </template>
    </div>
  </div>
</template>
