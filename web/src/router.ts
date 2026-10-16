import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { isSignedIn } from './stores/session'
import AppShell from './components/AppShell.vue'
import SignInView from './views/SignInView.vue'
import SignUpView from './views/SignUpView.vue'
import DashboardView from './views/DashboardView.vue'
import ComingSoonView from './views/ComingSoonView.vue'
import GuestComingSoonView from './views/GuestComingSoonView.vue'
import ApiKeysView from './views/ApiKeysView.vue'
import SettingsView from './views/SettingsView.vue'

// Pages that are in the navigation but not built yet.
const comingSoon = (path: string, name: string, title: string): RouteRecordRaw => ({
  path,
  name,
  component: ComingSoonView,
  meta: { title },
})

const routes: RouteRecordRaw[] = [
  {
    path: '/signin',
    name: 'signin',
    component: SignInView,
    meta: { guest: true, title: 'Sign in' },
  },
  {
    path: '/signup',
    name: 'signup',
    component: SignUpView,
    meta: { guest: true, title: 'Create account' },
  },
  {
    path: '/forgot-password',
    name: 'forgot-password',
    component: GuestComingSoonView,
    meta: { public: true, title: 'Reset your password' },
  },
  {
    path: '/try',
    name: 'guest-quick-match',
    component: GuestComingSoonView,
    meta: { public: true, title: 'Quick match' },
  },
  {
    path: '/',
    component: AppShell,
    meta: { requiresAuth: true },
    children: [
      { path: '', name: 'overview', component: DashboardView, meta: { title: 'Overview' } },
      comingSoon('searches', 'searches', 'Job searches'),
      comingSoon('searches/new', 'search-new', 'New search'),
      comingSoon('matches', 'matches', 'Matches'),
      comingSoon('cvs', 'cvs', 'CVs'),
      comingSoon('quick-match', 'quick-match', 'Quick match'),
      // API keys and settings
      { path: 'api-keys', name: 'api-keys', component: ApiKeysView, meta: { title: 'API keys' } },
      { path: 'settings', name: 'settings', component: SettingsView, meta: { title: 'Settings' } },
    ],
  },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior: () => ({ top: 0 }),
})

router.beforeEach((to) => {
  const signedIn = isSignedIn()
  if (to.matched.some((r) => r.meta.requiresAuth) && !signedIn) {
    return { name: 'signin', query: to.fullPath === '/' ? {} : { redirect: to.fullPath } }
  }
  if (to.meta.guest && signedIn) return { name: 'overview' }
  return true
})

router.afterEach((to) => {
  document.title = to.meta.title ? `${to.meta.title} · CV Evaluator` : 'CV Evaluator'
})
