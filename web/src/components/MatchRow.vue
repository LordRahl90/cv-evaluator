<script setup lang="ts">
// One row of the matches list: score ring, title with status and evaluation
// badges, company · location · portal · found, and the top missing skills.
import { computed } from 'vue'
import type { SearchResult } from '../api/results'
import MatchScoreRing from './MatchScoreRing.vue'
import { formatRelative } from '../utils/format'
import {
  STATUS_BADGES,
  STATUS_LABELS,
  dotted,
  evaluationLabel,
  providerLabel,
} from '../utils/matches'

const props = defineProps<{ result: SearchResult }>()

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
</script>

<template>
  <RouterLink :to="`/matches/${result.id}`" class="match-row" data-testid="match-row">
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
    </div>
    <div v-if="missing.length" class="match-row-gaps">
      <span v-for="(skill, i) in missing" :key="skill" class="match-gap">
        <template v-if="i === 0">Missing: </template>{{ skill }}
      </span>
    </div>
  </RouterLink>
</template>
