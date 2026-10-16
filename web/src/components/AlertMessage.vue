<script setup lang="ts">
import AppIcon from './AppIcon'

withDefaults(defineProps<{ tone?: 'error' | 'info' | 'success' }>(), { tone: 'error' })
</script>

<template>
  <div class="alert" :class="`alert-${tone}`" :role="tone === 'error' ? 'alert' : 'status'">
    <AppIcon :name="tone === 'success' ? 'check' : 'alert'" />
    <div class="alert-body"><slot /></div>
  </div>
</template>
