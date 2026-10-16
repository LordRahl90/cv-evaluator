<script setup lang="ts">
// A small modal dialog on the native <dialog> element. The parent owns the
// `open` state and closes it on the `close` event. While open, the page
// behind is inert, Tab and Shift+Tab cycle inside the dialog, Escape and a
// click on the backdrop ask to close (unless `locked`, e.g. while a request
// runs), and focus goes back to where it was when the dialog closes.
// Styled by src/styles/components/dialog.css.
import { nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'

const props = withDefaults(
  defineProps<{
    open: boolean
    title: string
    /** Ignore Escape and backdrop clicks, e.g. while a request is running. */
    locked?: boolean
  }>(),
  { locked: false },
)

const emit = defineEmits<{ close: [] }>()

const dialog = ref<HTMLDialogElement | null>(null)
const titleId = useId()
let returnFocus: HTMLElement | null = null

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

function focusables(): HTMLElement[] {
  return Array.from(dialog.value?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? []).filter(
    (el) => el.offsetParent !== null || el === document.activeElement,
  )
}

async function show() {
  const el = dialog.value
  if (!el || el.open) return
  returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  el.showModal()
  await nextTick()
  focusables()[0]?.focus()
}

function hide() {
  const el = dialog.value
  if (!el || !el.open) return
  el.close()
  if (returnFocus?.isConnected) returnFocus.focus()
  returnFocus = null
}

watch(
  () => props.open,
  (open) => (open ? show() : hide()),
  { flush: 'post' },
)

onMounted(() => {
  if (props.open) show()
})

onBeforeUnmount(hide)

function requestClose() {
  if (!props.locked) emit('close')
}

// Escape fires "cancel"; keep the dialog open and let the parent decide.
function onCancel(event: Event) {
  event.preventDefault()
  requestClose()
}

function onKeydown(event: KeyboardEvent) {
  if (event.key !== 'Tab') return
  const list = focusables()
  if (!list.length) return
  const first = list[0]!
  const last = list[list.length - 1]!
  const active = document.activeElement
  if (event.shiftKey && (active === first || !dialog.value?.contains(active))) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && active === last) {
    event.preventDefault()
    first.focus()
  }
}

// The panel fills the <dialog>; a click whose target is the dialog itself
// landed on the backdrop.
function onClick(event: MouseEvent) {
  if (event.target === dialog.value) requestClose()
}
</script>

<template>
  <dialog
    ref="dialog"
    class="dialog"
    :aria-labelledby="titleId"
    @cancel="onCancel"
    @keydown="onKeydown"
    @click="onClick"
  >
    <div class="dialog-panel">
      <h2 :id="titleId" class="dialog-title">{{ title }}</h2>
      <slot />
    </div>
  </dialog>
</template>
