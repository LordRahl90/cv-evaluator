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

// ---- API keys (GET/POST /api-keys, DELETE /api-keys/:id) ----------------

export interface MockApiKey {
  object: 'api_key'
  id: string
  name: string
  prefix: string
  created_at: string
  last_used_at: string | null
}

/**
 * A clearly fake secret, safe to show in screenshots. Joined at runtime so
 * secret scanners don't mistake the literal for a real (Stripe-style) key.
 */
export const FAKE_SECRET = ['sk', 'live', 'EXAMPLE0nlyN0tARealKey000000000000000000'].join('_')

export function sampleApiKeys(): MockApiKey[] {
  const ago = (ms: number) => new Date(Date.now() - ms).toISOString()
  return [
    {
      object: 'api_key',
      id: '01JA2B3C4D5E6F7G8H9J0KAK01',
      name: 'Zapier',
      prefix: 'sk_live_Hq2v',
      created_at: '2026-10-02T09:00:00Z',
      last_used_at: ago(12 * 60_000),
    },
    {
      object: 'api_key',
      id: '01JA2B3C4D5E6F7G8H9J0KAK02',
      name: 'Laptop CLI',
      prefix: 'sk_live_p0Lm',
      created_at: '2026-09-18T09:00:00Z',
      last_used_at: ago(9 * 86_400_000),
    },
  ]
}

export interface MockApiKeysOptions {
  keys?: MockApiKey[]
  /** Held until resolved, so tests can see the loading state. */
  gate?: Promise<void>
  /** Status for GET /api-keys (e.g. 500). */
  listStatus?: number
  /** Error answer for POST /api-keys instead of creating a key. */
  createError?: { status: number; body: unknown }
  /** Error answer for DELETE /api-keys/:id instead of revoking. */
  revokeError?: { status: number; body: unknown }
  /** Held until resolved before answering POST and DELETE. */
  writeGate?: Promise<void>
}

/**
 * A small in-memory API key store behind /api-keys. Returns the live list
 * and the requests seen, so tests can assert on both.
 */
export async function mockApiKeys(page: Page, options: MockApiKeysOptions = {}) {
  const keys = [...(options.keys ?? [])]
  const created: { name: string }[] = []
  const revoked: string[] = []

  await page.route('**/api/api-keys', async (route) => {
    const method = route.request().method()
    if (method === 'GET') {
      if (options.gate) await options.gate
      if (options.listStatus && options.listStatus !== 200) {
        return json(
          route,
          options.listStatus,
          apiError('api_error', 'internal_error', 'Something went wrong on our side.'),
        )
      }
      return json(route, 200, { object: 'list', data: keys })
    }
    if (method === 'POST') {
      if (options.writeGate) await options.writeGate
      const body = route.request().postDataJSON() as { name: string }
      created.push(body)
      if (options.createError) {
        return json(route, options.createError.status, options.createError.body)
      }
      const key: MockApiKey = {
        object: 'api_key',
        id: `01JA2B3C4D5E6F7G8H9J0KNEW${created.length}`,
        name: body.name,
        prefix: FAKE_SECRET.slice(0, 12),
        created_at: new Date().toISOString(),
        last_used_at: null,
      }
      keys.unshift(key)
      return json(route, 201, { ...key, secret: FAKE_SECRET })
    }
    return route.fallback()
  })

  await page.route('**/api/api-keys/*', async (route) => {
    if (route.request().method() !== 'DELETE') return route.fallback()
    if (options.writeGate) await options.writeGate
    const id = decodeURIComponent(route.request().url().split('/').pop() ?? '')
    revoked.push(id)
    if (options.revokeError) {
      return json(route, options.revokeError.status, options.revokeError.body)
    }
    const index = keys.findIndex((k) => k.id === id)
    if (index === -1) {
      return json(
        route,
        404,
        apiError('invalid_request_error', 'resource_missing', 'No such API key.', 'id'),
      )
    }
    keys.splice(index, 1)
    return json(route, 200, { object: 'api_key', id, deleted: true })
  })

  return { keys, created, revoked }
}

// ---- Account (PATCH /me, POST /me/password, DELETE /me, GET /me/export) --

/** The password the mocked account accepts, and the token a change returns. */
export const ACCOUNT_PASSWORD = 'password123'
export const RENEWED_TOKEN = 'renewed-token'

export interface MockAccountOptions {
  user?: User
  /** Error answer for PATCH /me instead of saving. */
  updateError?: { status: number; body: unknown }
  /** Error answer for GET /me/export. */
  exportError?: { status: number; body: unknown }
  /** Error answer for DELETE /me (after the password check passes). */
  deleteError?: { status: number; body: unknown }
  /** Held until resolved before answering every account write and the export. */
  gate?: Promise<void>
}

export function sampleExport(user: User = jane) {
  return {
    object: 'account_export',
    exported_at: '2026-10-16T10:00:00Z',
    profile: {
      id: user.id,
      first_name: user.first_name,
      last_name: user.last_name,
      email: user.email,
      phone: user.phone,
      created_at: '2026-09-01T09:00:00Z',
      updated_at: '2026-10-16T09:00:00Z',
      password_changed_at: null,
    },
    cvs: [],
    job_searches: [],
    results: [],
    api_keys: [],
    portal_suggestions: [],
  }
}

const incorrectPassword = (param: string) =>
  apiError('invalid_request_error', 'incorrect_password', 'The password is incorrect.', param)

/**
 * An in-memory account behind GET/PATCH/DELETE /me, POST /me/password and
 * GET /me/export, with the same rules as internal/servers/account: a wrong
 * password is a 400 incorrect_password, a password change returns a new
 * session, and after deletion every call is a 401. Register it after
 * mockApi. Returns the live user and the requests seen.
 */
export async function mockAccount(page: Page, options: MockAccountOptions = {}) {
  const state = {
    user: { ...(options.user ?? jane) },
    password: ACCOUNT_PASSWORD,
    token: 'test-token',
    deleted: false,
    updates: [] as Record<string, string>[],
    passwordChanges: [] as { current_password: string; new_password: string }[],
    deletions: [] as { password: string }[],
    exports: 0,
    /** Authorization header of every call, in order. */
    authHeaders: [] as string[],
  }

  // Answers like the auth middleware: 401 without the current token.
  const authorized = (route: Route) => {
    const header = route.request().headers()['authorization'] ?? ''
    state.authHeaders.push(header)
    if (state.deleted || header !== `Bearer ${state.token}`) {
      void json(
        route,
        401,
        apiError(
          'authentication_error',
          'invalid_token',
          'The session is no longer valid. Sign in again.',
        ),
      )
      return false
    }
    return true
  }

  await page.route('**/api/me', async (route) => {
    const method = route.request().method()
    if (method === 'GET') {
      if (!authorized(route)) return
      return json(route, 200, state.user)
    }
    if (options.gate) await options.gate
    if (!authorized(route)) return
    if (method === 'PATCH') {
      const body = route.request().postDataJSON() as Record<string, string>
      state.updates.push(body)
      if (options.updateError) {
        return json(route, options.updateError.status, options.updateError.body)
      }
      for (const [key, value] of Object.entries(body)) {
        const v = value.trim()
        ;(state.user as unknown as Record<string, string>)[key] =
          key === 'email' ? v.toLowerCase() : v
      }
      return json(route, 200, state.user)
    }
    if (method === 'DELETE') {
      const body = route.request().postDataJSON() as { password: string }
      state.deletions.push(body)
      if (body.password !== state.password) return json(route, 400, incorrectPassword('password'))
      if (options.deleteError) {
        return json(route, options.deleteError.status, options.deleteError.body)
      }
      state.deleted = true
      return route.fulfill({ status: 204 })
    }
    return route.fallback()
  })

  await page.route('**/api/me/password', async (route) => {
    if (options.gate) await options.gate
    if (!authorized(route)) return
    const body = route.request().postDataJSON() as {
      current_password: string
      new_password: string
    }
    state.passwordChanges.push(body)
    if (body.current_password !== state.password) {
      return json(route, 400, incorrectPassword('current_password'))
    }
    state.password = body.new_password
    state.token = RENEWED_TOKEN
    return json(route, 200, { ...session(state.user), token: RENEWED_TOKEN })
  })

  await page.route('**/api/me/export', async (route) => {
    if (options.gate) await options.gate
    if (!authorized(route)) return
    state.exports++
    if (options.exportError) {
      return json(route, options.exportError.status, options.exportError.body)
    }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      headers: { 'Content-Disposition': 'attachment; filename="cv-evaluator-export.json"' },
      body: JSON.stringify(sampleExport(state.user)),
    })
  })

  return state
}
