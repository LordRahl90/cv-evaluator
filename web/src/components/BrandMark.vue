<script setup lang="ts">
// "light" sits on the white sidebar, "dark" on the green auth panel.
withDefaults(defineProps<{ tone?: 'light' | 'dark' }>(), { tone: 'light' })
</script>

<template>
  <div class="brand" :class="`brand-${tone}`">
    <span class="brand-mark" aria-hidden="true">CV</span>
    <span class="brand-name">CV Evaluator</span>
  </div>
</template>
