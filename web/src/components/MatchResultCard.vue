<script setup lang="ts">
// One match result: score ring and verdict, summary, per-criterion bars,
// strengths and missing skills, and the requirements checklist (when the
// API sends one).
import { computed } from 'vue'
import AppIcon, { type IconName } from './AppIcon'
import ScoreRing from './ScoreRing.vue'
import type { MatchRequirement, MatchResult } from '../api/match'
import { capitalise } from '../utils/format'
import { scoreBand, scoreLabel } from '../utils/score'

const props = defineProps<{ result: MatchResult; title: string; meta: string }>()

const score = computed(() => props.result.overall_score)
const band = computed(() => scoreBand(score.value))
const badgeClass = computed(
  () => ({ high: 'badge-green', mid: 'badge-amber', low: 'badge-red' })[band.value],
)

const breakdown = computed(() => props.result.breakdown ?? [])
const strengths = computed(() => props.result.strengths ?? [])
const missing = computed(() => props.result.missing_skills ?? [])

// Must-haves first, then nice-to-haves; the API's order within each.
const requirements = computed(() => {
  const list = props.result.requirements ?? []
  return [...list].sort((a, b) => Number(a.importance !== 'must') - Number(b.importance !== 'must'))
})
const metCount = computed(() => requirements.value.filter((r) => r.met === 'yes').length)

const clamp = (n: number) => Math.max(0, Math.min(100, Math.round(n)))

interface MetView {
  icon: IconName
  label: string
  tone: string
}

function metView(r: MatchRequirement): MetView {
  if (r.met === 'yes') return { icon: 'check', label: 'Met', tone: 'is-met' }
  if (r.met === 'partial') return { icon: 'partial', label: 'Partly met', tone: 'is-partial' }
  return { icon: 'alert', label: 'Not met', tone: 'is-missing' }
}
</script>

<template>
  <section class="card match-card" aria-labelledby="match-title" data-testid="qm-result">
    <div class="match-head">
      <ScoreRing :score="score" />
      <div class="match-head-text">
        <span class="badge" :class="badgeClass">{{ scoreLabel(score) }}</span>
        <h2 id="match-title" class="match-title">{{ title }}</h2>
        <div class="match-meta">{{ meta }}</div>
      </div>
    </div>

    <div class="match-body">
      <p v-if="result.summary" class="match-summary">{{ result.summary }}</p>

      <ul v-if="breakdown.length" class="metric-list" aria-label="Score breakdown">
        <li v-for="item in breakdown" :key="item.criterion" class="metric">
          <span class="metric-name">{{ capitalise(item.criterion) }}</span>
          <span class="metric-track" aria-hidden="true">
            <span
              class="metric-fill"
              :class="`score-${scoreBand(item.score)}`"
              :style="{ '--value': `${clamp(item.score)}%` }"
            />
          </span>
          <b class="metric-score">
            {{ clamp(item.score) }}<span class="sr-only"> out of 100</span>
          </b>
          <span v-if="item.reason" class="metric-reason">{{ item.reason }}</span>
        </li>
      </ul>

      <div class="match-lists">
        <div class="match-list">
          <h3 class="match-list-title">Strengths</h3>
          <ul v-if="strengths.length" class="match-points">
            <li v-for="s in strengths" :key="s" class="match-point is-strength">
              <AppIcon name="check" />{{ s }}
            </li>
          </ul>
          <p v-else class="match-none">None stood out.</p>
        </div>
        <div class="match-list">
          <h3 class="match-list-title">Missing skills</h3>
          <ul v-if="missing.length" class="match-points">
            <li v-for="s in missing" :key="s" class="match-point is-missing">
              <AppIcon name="alert" />{{ s }}
            </li>
          </ul>
          <p v-else class="match-none">Nothing obvious is missing.</p>
        </div>
      </div>

      <div v-if="requirements.length" class="match-reqs">
        <div class="match-reqs-head">
          <h3 class="match-list-title">Requirements</h3>
          <span class="badge badge-grey">{{ metCount }} of {{ requirements.length }} met</span>
        </div>
        <ul class="req-list" data-testid="qm-requirements">
          <li
            v-for="(r, i) in requirements"
            :key="`${i}-${r.requirement}`"
            class="req"
            :class="metView(r).tone"
          >
            <span class="req-icon"><AppIcon :name="metView(r).icon" /></span>
            <div class="req-body">
              <div class="req-row">
                <span class="req-text">{{ r.requirement }}</span>
                <span class="badge" :class="r.importance === 'must' ? 'badge-amber' : 'badge-grey'">
                  {{ r.importance === 'must' ? 'Must have' : 'Nice to have' }}
                </span>
              </div>
              <div class="req-evidence">
                <span class="req-status">{{ metView(r).label }}</span>
                <template v-if="r.evidence"> · {{ r.evidence }}</template>
              </div>
            </div>
          </li>
        </ul>
      </div>
    </div>
  </section>
</template>
