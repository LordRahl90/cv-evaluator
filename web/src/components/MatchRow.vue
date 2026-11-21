<script setup lang="ts">
// One row of the matches list: score ring, title with status and evaluation
// badges, company · location · portal · found, and the top missing skills.
// A job that is not scored yet says why, with a "Score it" button beside
// the link (a button can't sit inside it).
import { computed, ref } from 'vue'
import { ApiError } from '../api/client'
import { reEvaluateResult, type SearchResult, type SearchResultDetail } from '../api/results'
import BusyButton from './BusyButton.vue'
import MatchScoreRing from './MatchScoreRing.vue'
import { formatRelative } from '../utils/format'
import {
  STATUS_BADGES,
  STATUS_LABELS,
  dotted,
  evaluationLabel,
  isWaiting,
  providerLabel,
  scoreItError,
  waitingText,
} from '../utils/matches'

const props = defineProps<{ result: SearchResult }>()
const emit = defineEmits<{ queued: [detail: SearchResultDetail | null] }>()

const score = computed(() => props.result.evaluation?.overall_score ?? null)
const evalLabel = computed(() =>
  evaluationLabel(props.result.evaluation_status, score.value !== null),
)
const meta = computed(() =>
  dotted(
    props.result.job.company,
    props.result.job.location,
    providerLabel(props.result.job.provider),
    formatRelative(props.result.created_at),
  ),
)
const missing = computed(() => props.result.evaluation?.top_missing_skills ?? [])

// ── Score it ──────────────────────────────────────────────────────────────

const busy = ref(false)
const requested = ref(false)
const error = ref('')
const waiting = computed(() => isWaiting(props.result.evaluation_status, score.value !== null))
const reason = computed(() =>
  requested.value
    ? 'Queued to be scored next. The score appears here when it is ready.'
    : waitingText(props.result.waiting_reason),
)

async function scoreIt() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    emit('queued', await reEvaluateResult(props.result.id))
    requested.value = true
  } catch (err) {
    if (err instanceof ApiError && err.status === 409) {
      // Already asked for, or being scored: nothing more to do.
      requested.value = true
      emit('queued', null)
    } else {
      error.value = scoreItError(err)
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="match-row" :class="{ 'is-waiting': waiting }" data-testid="match-row">
    <RouterLink :to="`/matches/${result.id}`" class="match-row-link">
      <MatchScoreRing :score="score" />
      <div class="match-row-body">
        <div class="match-row-title-line">
          <span class="match-row-title">{{ result.job.title || 'Untitled job' }}</span>
          <span class="badge" :class="STATUS_BADGES[result.status]">
            {{ STATUS_LABELS[result.status] }}
          </span>
          <span v-if="evalLabel" class="badge badge-grey">{{ evalLabel }}</span>
        </div>
        <div class="match-row-meta">{{ meta }}</div>
        <div v-if="waiting" class="match-row-reason" data-testid="match-row-reason">
          {{ reason }}
        </div>
      </div>
      <div v-if="missing.length" class="match-row-gaps">
        <span v-for="(skill, i) in missing" :key="skill" class="match-gap">
          <template v-if="i === 0">Missing: </template>{{ skill }}
        </span>
      </div>
    </RouterLink>
    <div v-if="waiting && !requested" class="match-row-action">
      <BusyButton
        type="button"
        variant="secondary"
        class="btn-sm"
        :busy="busy"
        busy-label="Queuing…"
        @click="scoreIt"
      >
        Score it<span class="sr-only">: {{ result.job.title || 'this job' }}</span>
      </BusyButton>
    </div>
    <p v-if="error" class="match-row-error" role="alert">{{ error }}</p>
  </div>
</template>
