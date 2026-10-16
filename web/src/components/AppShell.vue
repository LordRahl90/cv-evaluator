<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import AppIcon, { type IconName } from './AppIcon'
import BrandMark from './BrandMark.vue'
import UserMenu from './UserMenu.vue'
import { loadUser } from '../stores/session'

const route = useRoute()
const menuOpen = ref(false)

interface NavItem {
  to: string
  label: string
  icon: IconName
  exact?: boolean
}

const nav: NavItem[] = [
  { to: '/', label: 'Overview', icon: 'home', exact: true },
  { to: '/searches', label: 'Job searches', icon: 'search' },
  { to: '/matches', label: 'Matches', icon: 'star' },
  { to: '/cvs', label: 'CVs', icon: 'file' },
  { to: '/quick-match', label: 'Quick match', icon: 'bolt' },
  { to: '/api-keys', label: 'API keys', icon: 'key' },
  { to: '/settings', label: 'Settings', icon: 'sliders' },
]

function isActive(item: NavItem): boolean {
  return item.exact ? route.path === item.to : route.path.startsWith(item.to)
}

watch(
  () => route.fullPath,
  () => (menuOpen.value = false),
)

onMounted(() => {
  // A failure here is a 401 (handled globally) or shows in the user menu.
  loadUser().catch(() => {})
})
</script>

<template>
  <div class="app">
    <aside class="sidebar" :class="{ 'is-open': menuOpen }" aria-label="Main navigation">
      <div class="sidebar-top">
        <RouterLink to="/" class="sidebar-brand-link"><BrandMark /></RouterLink>
        <!-- Phones: the account menu sits in this top bar, next to the toggle. -->
        <div class="sidebar-top-actions">
          <UserMenu class="user-menu-mobile" />
          <button
            type="button"
            class="icon-btn sidebar-toggle"
            :aria-expanded="menuOpen ? 'true' : 'false'"
            aria-controls="sidebar-menu"
            :aria-label="menuOpen ? 'Close menu' : 'Open menu'"
            @click="menuOpen = !menuOpen"
          >
            <AppIcon :name="menuOpen ? 'close' : 'menu'" />
          </button>
        </div>
      </div>

      <div id="sidebar-menu" class="sidebar-menu">
        <nav class="sidebar-nav">
          <RouterLink
            v-for="item in nav"
            :key="item.to"
            :to="item.to"
            class="nav-item"
            :class="{ 'is-active': isActive(item) }"
            :aria-current="isActive(item) ? 'page' : undefined"
          >
            <AppIcon :name="item.icon" />{{ item.label }}
          </RouterLink>
        </nav>
      </div>
    </aside>

    <div class="app-body">
      <!-- Wider screens: a slim bar above the page with the account menu. -->
      <header class="topbar">
        <UserMenu class="user-menu-desktop" />
      </header>
      <main class="main">
        <RouterView />
      </main>
    </div>
  </div>
</template>
