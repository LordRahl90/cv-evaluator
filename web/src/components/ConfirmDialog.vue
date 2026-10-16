<script setup lang="ts">
// A modal confirmation built on <dialog>: showModal() traps focus and makes
// the rest of the page inert; Escape cancels. Focus starts on "Cancel" so a
// stray Enter never confirms something destructive.
import { nextTick, onMounted, ref, useId, watch } from 'vue'
import AlertMessage from './AlertMessage.vue'

const props = withDefaults(
  defineProps<{
    open: boolean
    title: string
    confirmLabel: string
    busyLabel?: string
    busy?: boolean
    error?: string
  }>(),
  { busyLabel: 'Working…', busy: false, error: '' },
)

const emit = defineEmits<{ confirm: []; cancel: [] }>()

const titleId = useId()
const dialog = ref<HTMLDialogElement | null>(null)
const cancelButton = ref<HTMLButtonElement | null>(null)
let returnFocus: HTMLElement | null = null

async function sync(open: boolean) {
  const el = dialog.value
  if (!el) return
  if (open && !el.open) {
    returnFocus = document.activeElement as HTMLElement | null
    el.showModal()
    await nextTick()
    cancelButton.value?.focus()
  } else if (!open && el.open) {
    el.close()
    returnFocus?.focus()
    returnFocus = null
  }
}

watch(() => props.open, sync)
onMounted(() => sync(props.open))

function onCancel(event: Event) {
  // Escape: keep the dialog open while the action runs.
  event.preventDefault()
  if (!props.busy) emit('cancel')
}
</script>

<template>
  <dialog ref="dialog" class="confirm-dialog" :aria-labelledby="titleId" @cancel="onCancel">
    <form class="confirm-dialog-body" method="dialog" @submit.prevent="emit('confirm')">
      <h2 :id="titleId" class="confirm-dialog-title">{{ title }}</h2>
      <div class="confirm-dialog-text"><slot /></div>
      <AlertMessage v-if="error">{{ error }}</AlertMessage>
      <div class="confirm-dialog-actions">
        <button
          ref="cancelButton"
          type="button"
          class="btn btn-secondary"
          :disabled="busy"
          @click="emit('cancel')"
        >
          Cancel
        </button>
        <button
          type="submit"
          class="btn confirm-dialog-confirm"
          :class="{ 'is-busy': busy }"
          :disabled="busy"
          :aria-busy="busy ? 'true' : undefined"
        >
          <span v-if="busy" class="spinner" aria-hidden="true" />
          {{ busy ? busyLabel : confirmLabel }}
        </button>
      </div>
    </form>
  </dialog>
</template>
