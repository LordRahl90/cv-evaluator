<script setup lang="ts">
// A match's overall score as a ring (styled by styles/components/score-ring.css).
// The fill comes from the --score custom property; unscored shows a dash.
import { computed } from 'vue'
import { scoreTone } from '../utils/matches'

const props = withDefaults(
  defineProps<{ score: number | null; size?: 'sm' | 'lg'; caption?: string }>(),
  { size: 'sm', caption: '' },
)

const tone = computed(() => scoreTone(props.score))
const label = computed(() =>
  props.score === null ? 'Not scored yet' : `Overall score ${props.score} out of 100`,
)
</script>

<template>
  <div
    class="score-ring"
    :class="[`score-ring-${size}`, `is-${tone}`]"
    :style="{ '--score': score ?? 0 }"
    role="img"
    :aria-label="label"
  >
    <span class="score-ring-inner">
      <span class="score-ring-value">{{ score ?? '–' }}</span>
      <span v-if="caption" class="score-ring-caption">{{ caption }}</span>
    </span>
  </div>
</template>
