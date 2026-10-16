// Network-level API mocks for Playwright. Every call the app makes goes to
// /api/*; these handlers answer it so tests need no Go server or database.
import type { Page, Route } from '@playwright/test'
import type { CVSummary, JobSearch, User } from '../../src/api/types'

export const TOKEN_KEY = 'cv-evaluator.token'

export const jane: User = {
  object: 'user',
  id: '01JA2B3C4D5E6F7G8H9J0K1M2N',
  first_name: 'Jane',
  last_name: 'Doe',
  email: 'jane.doe@example.com',
  phone: '',
}

const hoursFromNow = (h: number) => new Date(Date.now() + h * 3_600_000).toISOString()

export const sampleCV: CVSummary = {
  id: '01JA2B3C4D5E6F7G8H9J0K1CV1',
  created_at: '2026-10-12T08:00:00Z',
  updated_at: '2026-10-12T08:00:00Z',
}

export function sampleSearches(): JobSearch[] {
  const base = {
    object: 'job_search' as const,
    created_at: '2026-10-10T09:00:00Z',
    updated_at: '2026-10-16T07:00:00Z',
  }
  return [
    {
      ...base,
      id: '01JA2B3C4D5E6F7G8H9J0KS001',
      name: 'Senior Go · Copenhagen',
      query: 'senior go engineer',
      location: 'Copenhagen',
      frequency: 'daily',
      enabled: true,
      last_run_at: hoursFromNow(-2),
      next_run_at: hoursFromNow(22),
    },
    {
      ...base,
      id: '01JA2B3C4D5E6F7G8H9J0KS002',
      name: 'Backend · Remote EU',
      query: 'backend engineer',
      location: 'Remote',
      frequency: 'hourly',
      enabled: true,
      last_run_at: null,
      next_run_at: hoursFromNow(0.6),
    },
    {
      ...base,
      id: '01JA2B3C4D5E6F7G8H9J0KS003',
      name: 'Go contractor · Berlin',
      query: 'go contractor',
      location: 'Berlin',
      frequency: 'weekly',
      enabled: false,
      last_run_at: null,
      next_run_at: null,
    },
  ]
}

export interface MockOptions {
  user?: User
  cvs?: CVSummary[]
  searches?: JobSearch[]
  /** Held until resolved, so tests can see loading states. */
  gate?: Promise<void>
  /** Status for every authenticated call, e.g. 401 for an expired session. */
  authStatus?: number
}

const json = (route: Route, status: number, body: unknown) =>
  route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })

/** Mocks GET /me, /cv/ and /job-searches. */
export async function mockApi(page: Page, options: MockOptions = {}) {
  const { user = jane, cvs = [], searches = [], gate, authStatus } = options
  const authed = async (route: Route, body: unknown) => {
    if (gate) await gate
    if (authStatus && authStatus !== 200) {
      return json(route, authStatus, {
        error: {
          type: 'authentication_error',
          code: 'invalid_token',
          message: 'The session token is invalid or has expired.',
        },
      })
    }
    return json(route, 200, body)
  }
  await page.route('**/api/me', (route) => authed(route, user))
  await page.route('**/api/cv/', (route) => authed(route, cvs))
  await page.route('**/api/job-searches', (route) =>
    authed(route, { object: 'list', data: searches }),
  )
}

/**
 * Starts the page with a stored session token, as after a previous sign-in.
 * Seeded once per tab, so signing out and navigating again stays signed out.
 */
export async function signedIn(page: Page, token = 'test-token') {
  await page.addInitScript(
    ([key, value]) => {
      if (window.sessionStorage.getItem('e2e-seeded')) return
      window.sessionStorage.setItem('e2e-seeded', '1')
      window.localStorage.setItem(key, value)
    },
    [TOKEN_KEY, token] as const,
  )
}

/** A promise plus its resolver, for holding mocked responses. */
export function deferred() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => (release = resolve))
  return { promise, release }
}

export function session(user: User = jane) {
  return { object: 'session', token: 'new-token', token_type: 'Bearer', user }
}

export function apiError(type: string, code: string, message: string, param?: string) {
  return { error: { type, code, message, ...(param ? { param } : {}) } }
}
