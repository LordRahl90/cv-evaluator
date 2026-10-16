<script setup lang="ts">
// A match's overall score as a ring (styled by styles/components/match-ring.css).
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
    class="match-ring"
    :class="[`match-ring-${size}`, `is-${tone}`]"
    :style="{ '--score': score ?? 0 }"
    role="img"
    :aria-label="label"
  >
    <span class="match-ring-inner">
      <span class="match-ring-value">{{ score ?? '–' }}</span>
      <span v-if="caption" class="match-ring-caption">{{ caption }}</span>
    </span>
  </div>
</template>
