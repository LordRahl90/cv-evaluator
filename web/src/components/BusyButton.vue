<script setup lang="ts">
withDefaults(
  defineProps<{
    busy?: boolean
    busyLabel?: string
    type?: 'submit' | 'button'
    variant?: 'primary' | 'secondary'
  }>(),
  { busy: false, busyLabel: 'Working…', type: 'submit', variant: 'primary' },
)
</script>

<template>
  <button
    :type="type"
    class="btn"
    :class="[`btn-${variant}`, { 'is-busy': busy }]"
    :disabled="busy"
    :aria-busy="busy ? 'true' : undefined"
  >
    <span v-if="busy" class="spinner" aria-hidden="true" />
    <template v-if="busy">{{ busyLabel }}</template>
    <slot v-else />
  </button>
</template>
