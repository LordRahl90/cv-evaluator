<script setup lang="ts">
// A "more actions" (⋮) menu button. Same menu-button pattern as UserMenu:
// focus moves to the first item on open and back to the trigger on close;
// Escape, Tab away and clicks outside close it; arrow keys move between items.
import { nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import AppIcon, { type IconName } from './AppIcon'

export interface ActionMenuItem {
  key: string
  label: string
  icon?: IconName
  danger?: boolean
}

defineProps<{ label: string; items: readonly ActionMenuItem[] }>()
const emit = defineEmits<{ select: [key: string] }>()

const panelId = useId()
const open = ref(false)
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)

function menuItems(): HTMLElement[] {
  return Array.from(panel.value?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])
}

async function openMenu(focusLast = false) {
  open.value = true
  await nextTick()
  const list = menuItems()
  ;(focusLast ? list[list.length - 1] : list[0])?.focus()
}

function closeMenu({ restoreFocus = true } = {}) {
  if (!open.value) return
  open.value = false
  if (restoreFocus) trigger.value?.focus()
}

function onTriggerKeydown(event: KeyboardEvent) {
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    openMenu(event.key === 'ArrowUp')
  }
}

function onPanelKeydown(event: KeyboardEvent) {
  const list = menuItems()
  const index = list.indexOf(document.activeElement as HTMLElement)
  const focus = (i: number) => list[(i + list.length) % list.length]?.focus()
  switch (event.key) {
    case 'Escape':
      event.preventDefault()
      closeMenu()
      break
    case 'ArrowDown':
      event.preventDefault()
      focus(index + 1)
      break
    case 'ArrowUp':
      event.preventDefault()
      focus(index - 1)
      break
    case 'Home':
      event.preventDefault()
      focus(0)
      break
    case 'End':
      event.preventDefault()
      focus(list.length - 1)
      break
    case 'Tab':
      closeMenu({ restoreFocus: false })
      break
  }
}

function choose(key: string) {
  closeMenu()
  emit('select', key)
}

function onDocumentPointerDown(event: PointerEvent) {
  if (root.value && !root.value.contains(event.target as Node)) closeMenu({ restoreFocus: false })
}

watch(open, (isOpen) => {
  if (isOpen) document.addEventListener('pointerdown', onDocumentPointerDown)
  else document.removeEventListener('pointerdown', onDocumentPointerDown)
})

onBeforeUnmount(() => document.removeEventListener('pointerdown', onDocumentPointerDown))

defineExpose({ focus: () => trigger.value?.focus() })
</script>

<template>
  <div ref="root" class="action-menu" :class="{ 'is-open': open }">
    <button
      ref="trigger"
      type="button"
      class="icon-btn action-menu-trigger"
      aria-haspopup="menu"
      :aria-expanded="open ? 'true' : 'false'"
      :aria-controls="panelId"
      :aria-label="label"
      @click="open ? closeMenu() : openMenu()"
      @keydown="onTriggerKeydown"
    >
      <AppIcon name="more" />
    </button>
    <div
      v-show="open"
      :id="panelId"
      ref="panel"
      class="action-menu-panel"
      role="menu"
      :aria-label="label"
      @keydown="onPanelKeydown"
    >
      <button
        v-for="item in items"
        :key="item.key"
        type="button"
        role="menuitem"
        tabindex="-1"
        class="action-menu-item"
        :class="{ 'is-danger': item.danger }"
        @click="choose(item.key)"
      >
        <AppIcon v-if="item.icon" :name="item.icon" />{{ item.label }}
      </button>
    </div>
  </div>
</template>
