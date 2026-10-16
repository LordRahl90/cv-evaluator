import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { isSignedIn } from './stores/session'
import AppShell from './components/AppShell.vue'
import SignInView from './views/SignInView.vue'
import SignUpView from './views/SignUpView.vue'
import DashboardView from './views/DashboardView.vue'
import GuestComingSoonView from './views/GuestComingSoonView.vue'
import MatchesView from './views/MatchesView.vue'
import MatchDetailView from './views/MatchDetailView.vue'
import JobSearchesView from './views/JobSearchesView.vue'
import JobSearchFormView from './views/JobSearchFormView.vue'
import CVsView from './views/CVsView.vue'
import QuickMatchView from './views/QuickMatchView.vue'
import ApiKeysView from './views/ApiKeysView.vue'
import SettingsView from './views/SettingsView.vue'

// Job searches: the list (with portal suggestions) and the create/edit form.
const jobSearchRoutes: RouteRecordRaw[] = [
  {
    path: 'searches',
    name: 'searches',
    component: JobSearchesView,
    meta: { title: 'Job searches' },
  },
  {
    path: 'searches/new',
    name: 'search-new',
    component: JobSearchFormView,
    meta: { title: 'New search' },
  },
  {
    path: 'searches/:id/edit',
    name: 'search-edit',
    component: JobSearchFormView,
    meta: { title: 'Edit search' },
  },
]

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
      ...jobSearchRoutes,
      // Matches (search results)
      { path: 'matches', name: 'matches', component: MatchesView, meta: { title: 'Matches' } },
      {
        path: 'matches/:id',
        name: 'match',
        component: MatchDetailView,
        meta: { title: 'Match' },
      },
      { path: 'cvs', name: 'cvs', component: CVsView, meta: { title: 'CVs' } },
      {
        path: 'quick-match',
        name: 'quick-match',
        component: QuickMatchView,
        meta: { title: 'Quick match' },
      },
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
