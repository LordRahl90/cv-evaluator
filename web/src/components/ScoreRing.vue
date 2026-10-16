<script setup lang="ts">
// A 0-100 score in a ring, coloured by band (strong / partial / weak).
import { computed } from 'vue'
import { scoreBand } from '../utils/score'

const props = withDefaults(defineProps<{ score: number; size?: 'lg' | 'md' | 'sm' }>(), {
  size: 'lg',
})

const value = computed(() => Math.max(0, Math.min(100, Math.round(props.score))))
</script>

<template>
  <span
    class="score-ring"
    :class="[`score-ring-${size}`, `score-${scoreBand(value)}`]"
    :style="{ '--score': `${value}%` }"
    role="img"
    :aria-label="`Score ${value} out of 100`"
  >
    <span class="score-ring-value" aria-hidden="true">{{ value }}</span>
  </span>
</template>
