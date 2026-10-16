<script setup lang="ts">
// The call to action above a strong quick match (80+): write a cover
// letter for it. Once a letter exists the button reopens it instead.
import AppIcon from './AppIcon'

defineProps<{ hasLetter: boolean }>()
const emit = defineEmits<{ write: [] }>()
</script>

<template>
  <section class="cl-banner" aria-labelledby="cl-banner-title" data-testid="cl-banner">
    <span class="cl-banner-icon"><AppIcon name="mail" /></span>
    <div class="cl-banner-text">
      <h2 id="cl-banner-title" class="cl-banner-title">Strong match: worth applying</h2>
      <p class="cl-banner-sub">
        Get a cover letter built from the facts in your CV that this job asks for. No clichés,
        nothing invented.
      </p>
    </div>
    <button type="button" class="btn btn-primary cl-banner-action" @click="emit('write')">
      <AppIcon name="pen" />{{ hasLetter ? 'Open cover letter' : 'Write cover letter' }}
    </button>
  </section>
</template>
