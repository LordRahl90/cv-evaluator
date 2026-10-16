<script setup lang="ts">
// The dashed upload area from the empty CVs page: drop a PDF on it or use
// the "Choose PDF" button. Checking the file is the parent's job.
import { ref } from 'vue'
import AppIcon from './AppIcon'
import CVFilePicker from './CVFilePicker.vue'

const emit = defineEmits<{ select: [files: FileList | null] }>()

const dragging = ref(false)
// dragenter/dragleave fire for every child; count them to know when the
// pointer really left the zone.
let depth = 0

function onDragEnter() {
  depth += 1
  dragging.value = true
}

function onDragLeave() {
  depth = Math.max(0, depth - 1)
  if (depth === 0) dragging.value = false
}

function onDrop(event: DragEvent) {
  depth = 0
  dragging.value = false
  emit('select', event.dataTransfer?.files ?? null)
}
</script>

<template>
  <section
    class="drop-zone"
    :class="{ 'is-dragging': dragging }"
    aria-label="Upload a CV"
    data-testid="cv-drop-zone"
    @dragenter.prevent="onDragEnter"
    @dragover.prevent
    @dragleave="onDragLeave"
    @drop.prevent="onDrop"
  >
    <span class="drop-zone-icon"><AppIcon name="upload" /></span>
    <h2 class="drop-zone-title">
      {{ dragging ? 'Drop your PDF to upload it' : 'Upload your first CV' }}
    </h2>
    <p class="drop-zone-text">
      Drop a PDF here or choose a file. We extract the text, split it into summary, experience,
      skills and education, and use it to score jobs.
    </p>
    <div class="drop-zone-actions">
      <CVFilePicker @select="emit('select', $event)" />
    </div>
    <div class="drop-zone-hint">PDF only · up to 5 MB · text must be selectable, not a scan</div>
  </section>
</template>
