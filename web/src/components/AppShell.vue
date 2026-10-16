<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppIcon, { type IconName } from './AppIcon'
import BrandMark from './BrandMark.vue'
import { loadUser, session, signOut } from '../stores/session'
import { initials } from '../utils/format'

const route = useRoute()
const router = useRouter()
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
  { to: '/keys', label: 'API keys', icon: 'key' },
  { to: '/settings', label: 'Settings', icon: 'sliders' },
]

function isActive(item: NavItem): boolean {
  return item.exact ? route.path === item.to : route.path.startsWith(item.to)
}

const user = computed(() => session.user)
const fullName = computed(() =>
  [user.value?.first_name, user.value?.last_name].filter(Boolean).join(' '),
)

watch(
  () => route.fullPath,
  () => (menuOpen.value = false),
)

onMounted(() => {
  // A failure here is a 401 (handled globally) or shows in the footer.
  loadUser().catch(() => {})
})

function handleSignOut() {
  signOut()
  router.replace({ name: 'signin' })
}
</script>

<template>
  <div class="app">
    <aside class="sidebar" :class="{ 'is-open': menuOpen }" aria-label="Main navigation">
      <div class="sidebar-top">
        <RouterLink to="/" class="sidebar-brand-link"><BrandMark /></RouterLink>
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

        <div class="sidebar-foot" data-testid="sidebar-user">
          <template v-if="user">
            <span class="avatar" aria-hidden="true">{{ initials(user) }}</span>
            <div class="sidebar-user">
              <div class="sidebar-user-name" :title="fullName || user.email">
                {{ fullName || user.email }}
              </div>
              <div class="sidebar-user-email" :title="user.email">{{ user.email }}</div>
            </div>
          </template>
          <template v-else-if="session.userError">
            <div class="sidebar-user">
              <div class="sidebar-user-email">Couldn't load your profile.</div>
            </div>
          </template>
          <template v-else>
            <span class="avatar skeleton" aria-hidden="true" />
            <div class="sidebar-user" aria-label="Loading your profile" role="status">
              <span class="skeleton skeleton-line skeleton-w-24" />
              <span class="skeleton skeleton-line skeleton-w-32" />
            </div>
          </template>
          <button
            type="button"
            class="icon-btn sidebar-signout"
            aria-label="Sign out"
            title="Sign out"
            @click="handleSignOut"
          >
            <AppIcon name="logout" />
          </button>
        </div>
      </div>
    </aside>

    <main class="main">
      <RouterView />
    </main>
  </div>
</template>
