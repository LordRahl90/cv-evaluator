<script setup lang="ts">
// Shown while POST /cv/ runs. The API does not report progress, so the bar
// is indeterminate and the copy says what is happening, not how far along.
defineProps<{ fileName: string }>()
</script>

<template>
  <div class="upload-progress" role="status" aria-live="polite" data-testid="cv-upload-progress">
    <span class="spinner upload-progress-spinner" aria-hidden="true" />
    <div class="upload-progress-body">
      <div class="upload-progress-title">
        Processing <span class="upload-progress-file">{{ fileName }}</span>
      </div>
      <div class="upload-progress-text">
        Extracting the text and splitting it into sections. This can take a minute; you can use
        other pages meanwhile, but keep this tab open.
      </div>
      <div class="upload-progress-track" aria-hidden="true">
        <span class="upload-progress-fill" />
      </div>
    </div>
  </div>
</template>
