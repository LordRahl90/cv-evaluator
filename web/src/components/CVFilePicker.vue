<script setup lang="ts">
// A button that opens the file chooser for a PDF. The real <input type=file>
// stays in the page (visually hidden) so it works with assistive tech and
// tests; the button is what keyboard and pointer users interact with.
import { ref, useId } from 'vue'
import AppIcon from './AppIcon'

withDefaults(
  defineProps<{ label?: string; variant?: 'primary' | 'secondary'; disabled?: boolean }>(),
  { label: 'Choose PDF', variant: 'primary', disabled: false },
)

const emit = defineEmits<{ select: [files: FileList | null] }>()

const inputId = useId()
const input = ref<HTMLInputElement | null>(null)

function onChange(event: Event) {
  const target = event.target as HTMLInputElement
  emit('select', target.files)
  // Allow choosing the same file again after an error.
  target.value = ''
}
</script>

<template>
  <span class="file-picker">
    <button
      type="button"
      class="btn"
      :class="`btn-${variant}`"
      :disabled="disabled"
      @click="input?.click()"
    >
      <AppIcon name="upload" />{{ label }}
    </button>
    <input
      :id="inputId"
      ref="input"
      class="sr-only"
      type="file"
      accept="application/pdf,.pdf"
      tabindex="-1"
      aria-label="CV file (PDF)"
      data-testid="cv-file-input"
      :disabled="disabled"
      @change="onChange"
    />
  </span>
</template>
