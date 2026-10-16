<script setup lang="ts">
// The small coloured tile with a portal's initials ("in", "ji") from the
// mockups. Known portals get their tile colour; any other portal a neutral one.
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{ name: string; displayName: string; size?: 'sm' | 'md' }>(),
  { size: 'md' },
)

const KNOWN: Record<string, string> = { linkedin: 'in', jobindex: 'ji' }

const initials = computed(
  () =>
    KNOWN[props.name] ??
    (props.displayName || props.name)
      .replace(/[^\p{L}\p{N}]/gu, '')
      .slice(0, 2)
      .toLowerCase(),
)
const tone = computed(() => (props.name in KNOWN ? props.name : 'other'))
</script>

<template>
  <span
    class="portal-logo"
    :class="[`portal-logo-${size}`, `portal-logo-${tone}`]"
    aria-hidden="true"
  >
    {{ initials }}
  </span>
</template>
