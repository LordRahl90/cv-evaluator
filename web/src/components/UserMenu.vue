<script setup lang="ts">
// The account menu at the top right: avatar and name, opening a small
// drop-down with the email and "Sign out". Follows the menu-button pattern:
// focus moves to the first item on open and back to the trigger on close;
// Escape, Tab away and clicks outside close it.
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import { useRouter } from 'vue-router'
import AppIcon from './AppIcon'
import { session, signOut } from '../stores/session'
import { initials } from '../utils/format'

const router = useRouter()
const panelId = useId()

const open = ref(false)
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)

const user = computed(() => session.user)
const fullName = computed(() =>
  [user.value?.first_name, user.value?.last_name].filter(Boolean).join(' '),
)
const displayName = computed(() => fullName.value || user.value?.email || '')

function items(): HTMLElement[] {
  return Array.from(panel.value?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])
}

async function openMenu() {
  open.value = true
  await nextTick()
  items()[0]?.focus()
}

function closeMenu({ restoreFocus = true } = {}) {
  if (!open.value) return
  open.value = false
  if (restoreFocus) trigger.value?.focus()
}

function toggle() {
  if (open.value) closeMenu()
  else openMenu()
}

function onTriggerKeydown(event: KeyboardEvent) {
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    openMenu()
  }
}

function onPanelKeydown(event: KeyboardEvent) {
  const list = items()
  const index = list.indexOf(document.activeElement as HTMLElement)
  switch (event.key) {
    case 'Escape':
      event.preventDefault()
      closeMenu()
      break
    case 'ArrowDown':
      event.preventDefault()
      list[(index + 1) % list.length]?.focus()
      break
    case 'ArrowUp':
      event.preventDefault()
      list[(index - 1 + list.length) % list.length]?.focus()
      break
    case 'Home':
      event.preventDefault()
      list[0]?.focus()
      break
    case 'End':
      event.preventDefault()
      list[list.length - 1]?.focus()
      break
    case 'Tab':
      // Let focus move on naturally; the menu should not stay open behind it.
      closeMenu({ restoreFocus: false })
      break
  }
}

function onDocumentPointerDown(event: PointerEvent) {
  if (root.value && !root.value.contains(event.target as Node)) {
    closeMenu({ restoreFocus: false })
  }
}

watch(open, (isOpen) => {
  if (isOpen) document.addEventListener('pointerdown', onDocumentPointerDown)
  else document.removeEventListener('pointerdown', onDocumentPointerDown)
})

onBeforeUnmount(() => document.removeEventListener('pointerdown', onDocumentPointerDown))

function handleSignOut() {
  open.value = false
  signOut()
  router.replace({ name: 'signin' })
}
</script>

<template>
  <div ref="root" class="user-menu" :class="{ 'is-open': open }">
    <button
      ref="trigger"
      type="button"
      class="user-menu-trigger"
      aria-haspopup="menu"
      :aria-expanded="open ? 'true' : 'false'"
      :aria-controls="panelId"
      aria-label="Account menu"
      data-testid="user-menu-trigger"
      @click="toggle"
      @keydown="onTriggerKeydown"
    >
      <span v-if="user" class="avatar" aria-hidden="true">{{ initials(user) }}</span>
      <span v-else class="avatar skeleton" aria-hidden="true" />
      <span v-if="user" class="user-menu-name">{{ displayName }}</span>
      <span v-else-if="!session.userError" class="user-menu-name">
        <span class="skeleton skeleton-line skeleton-w-24" />
      </span>
      <AppIcon name="chevronDown" />
    </button>

    <div
      v-show="open"
      :id="panelId"
      ref="panel"
      class="user-menu-panel"
      role="menu"
      aria-label="Account"
      @keydown="onPanelKeydown"
    >
      <div class="user-menu-header" data-testid="user-menu-header">
        <template v-if="user">
          <div class="user-menu-header-name">{{ displayName }}</div>
          <div class="user-menu-header-email">{{ user.email }}</div>
        </template>
        <div v-else-if="session.userError" class="user-menu-header-email">
          Couldn't load your profile.
        </div>
        <div v-else class="user-menu-header-email">Loading your profile…</div>
      </div>
      <RouterLink
        to="/settings"
        role="menuitem"
        class="user-menu-item"
        tabindex="-1"
        @click="closeMenu({ restoreFocus: false })"
      >
        <AppIcon name="sliders" />Settings
      </RouterLink>
      <button
        type="button"
        role="menuitem"
        class="user-menu-item"
        tabindex="-1"
        @click="handleSignOut"
      >
        <AppIcon name="logout" />Sign out
      </button>
    </div>
  </div>
</template>
