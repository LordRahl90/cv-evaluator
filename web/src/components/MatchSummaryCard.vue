<script setup lang="ts">
// A one-line summary of a quick match (ring, title, strengths and missing
// skills) shown under the cover letter, with a way back to the full result.
import { computed } from 'vue'
import ScoreRing from './ScoreRing.vue'
import type { MatchResult } from '../api/match'
import { scoreBand, scoreLabel } from '../utils/score'

const props = defineProps<{ result: MatchResult; title: string }>()
const emit = defineEmits<{ 'show-result': [] }>()

const badgeClass = computed(
  () =>
    ({ high: 'badge-green', mid: 'badge-amber', low: 'badge-red' })[
      scoreBand(props.result.overall_score)
    ],
)

const details = computed(() => {
  const parts: string[] = []
  const strengths = (props.result.strengths ?? []).slice(0, 3)
  const missing = (props.result.missing_skills ?? []).slice(0, 2)
  if (strengths.length) parts.push(`Strengths: ${strengths.join(', ')}`)
  if (missing.length) parts.push(`Missing: ${missing.join(', ')}`)
  return parts.join(' · ')
})
</script>

<template>
  <section class="card cl-summary" aria-label="Match summary" data-testid="cl-summary">
    <ScoreRing :score="result.overall_score" size="md" />
    <div class="cl-summary-text">
      <span class="badge" :class="badgeClass">{{ scoreLabel(result.overall_score) }}</span>
      <div class="cl-summary-title">{{ title }}</div>
      <div v-if="details" class="cl-summary-details">{{ details }}</div>
    </div>
    <button type="button" class="link-button cl-summary-link" @click="emit('show-result')">
      Show full result
    </button>
  </section>
</template>
