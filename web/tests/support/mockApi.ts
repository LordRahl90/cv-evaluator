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

// ---------------------------------------------------------------------------
// Job searches, providers and portal suggestions (stateful). Register after
// mockApi(): Playwright matches the most recently added route first.

export const sampleProviders = [
  {
    object: 'provider',
    name: 'jobindex',
    display_name: 'Jobindex',
    website: 'https://www.jobindex.dk',
    coverage: 'Denmark',
  },
  {
    object: 'provider',
    name: 'linkedin',
    display_name: 'LinkedIn',
    website: 'https://www.linkedin.com/jobs',
    coverage: 'Worldwide',
  },
]

export function sampleSuggestions() {
  return [
    {
      object: 'portal_suggestion',
      id: '01JA2B3C4D5E6F7G8H9J0KPS01',
      name: 'StepStone',
      url: 'https://stepstone.de',
      note: '',
      status: 'planned',
      created_at: hoursFromNow(-24 * 6),
      updated_at: hoursFromNow(-24 * 2),
    },
    {
      object: 'portal_suggestion',
      id: '01JA2B3C4D5E6F7G8H9J0KPS02',
      name: 'The Hub',
      url: 'https://thehub.io',
      note: '',
      status: 'submitted',
      created_at: hoursFromNow(-48),
      updated_at: hoursFromNow(-48),
    },
  ]
}

export interface MockReply {
  status: number
  body: unknown
}

export interface JobSearchMockOptions {
  searches?: JobSearch[]
  providers?: unknown[]
  suggestions?: unknown[]
  /** Override any call: return a reply to send instead of the default. */
  override?: (method: string, path: string, body: unknown) => MockReply | undefined
  /** Held until resolved (GETs under /job-searches only), to show loading states. */
  gate?: Promise<void>
}

export interface JobSearchMock {
  searches: JobSearch[]
  suggestions: unknown[]
  /** Every non-GET call: method, path and JSON body. */
  calls: { method: string; path: string; body: unknown }[]
}

let nextId = 100

/** Mocks /job-searches(/:id(/run)), /providers and /portal-suggestions. */
export async function mockJobSearchesApi(
  page: Page,
  options: JobSearchMockOptions = {},
): Promise<JobSearchMock> {
  const state: JobSearchMock = {
    searches: [...(options.searches ?? [])],
    suggestions: [...(options.suggestions ?? [])],
    calls: [],
  }
  const providers = options.providers ?? sampleProviders
  const notFound = apiError(
    'invalid_request_error',
    'resource_missing',
    'No such job search.',
    'id',
  )

  const handle = (method: string, path: string, body: unknown): MockReply => {
    const overridden = options.override?.(method, path, body)
    if (overridden) return overridden

    if (path === '/providers') return { status: 200, body: { object: 'list', data: providers } }
    if (path === '/portal-suggestions') {
      if (method === 'GET')
        return { status: 200, body: { object: 'list', data: state.suggestions } }
      const req = body as { name?: string; url: string }
      const now = new Date().toISOString()
      const created = {
        object: 'portal_suggestion',
        id: `01JA2B3C4D5E6F7G8H9J0KP${nextId++}`,
        name: req.name || req.url.replace(/^https?:\/\//, '').replace(/^www\./, ''),
        url: req.url,
        note: '',
        status: 'submitted',
        created_at: now,
        updated_at: now,
      }
      state.suggestions.unshift(created)
      return { status: 201, body: created }
    }
    if (path === '/job-searches') {
      if (method === 'GET') return { status: 200, body: { object: 'list', data: state.searches } }
      const req = body as Partial<JobSearch> & { query: string }
      const enabled = req.enabled ?? true
      const now = new Date().toISOString()
      const created: JobSearch = {
        object: 'job_search',
        id: `01JA2B3C4D5E6F7G8H9J0KS${nextId++}`,
        name: req.name || req.query,
        query: req.query,
        location: req.location ?? '',
        frequency: req.frequency ?? 'daily',
        enabled,
        last_run_at: null,
        next_run_at: enabled ? now : null,
        created_at: now,
        updated_at: now,
      }
      state.searches.unshift(created)
      return { status: 201, body: created }
    }

    const match = path.match(/^\/job-searches\/([^/]+)(\/run)?$/)
    const index = match ? state.searches.findIndex((s) => s.id === match[1]) : -1
    if (!match || index < 0) return { status: 404, body: notFound }
    const current = state.searches[index]

    if (match[2]) {
      if (!current.enabled) {
        return {
          status: 409,
          body: apiError(
            'invalid_request_error',
            'job_search_disabled',
            'The job search is disabled. Enable it before running it.',
          ),
        }
      }
      state.searches[index] = { ...current, next_run_at: new Date().toISOString() }
      return { status: 202, body: state.searches[index] }
    }
    if (method === 'GET') return { status: 200, body: current }
    if (method === 'DELETE') {
      state.searches.splice(index, 1)
      return { status: 200, body: { object: 'job_search', id: current.id, deleted: true } }
    }
    // PATCH
    const patch = body as Partial<JobSearch>
    const updated: JobSearch = { ...current, ...patch, updated_at: new Date().toISOString() }
    if (!updated.name) updated.name = updated.query
    if (patch.enabled === false) updated.next_run_at = null
    if (patch.enabled === true && !current.enabled) updated.next_run_at = new Date().toISOString()
    state.searches[index] = updated
    return { status: 200, body: updated }
  }

  await page.route(/\/api\/(job-searches(\/.*)?|providers|portal-suggestions)$/, async (route) => {
    const request = route.request()
    const method = request.method()
    const path = new URL(request.url()).pathname.replace(/^\/api/, '')
    const body = request.postData() ? request.postDataJSON() : undefined
    if (method !== 'GET') state.calls.push({ method, path, body })
    if (method === 'GET' && options.gate && path.startsWith('/job-searches')) await options.gate
    const reply = handle(method, path, body)
    return json(route, reply.status, reply.body)
  })

  return state
}

// ---------------------------------------------------------------------------
// CVs (POST/GET /cv/, GET /cv/download/:id, DELETE /cv/:id)

/** Three CVs, newest first: the first is the one matching uses. */
export function sampleCVs(): CVSummary[] {
  return [
    sampleCV,
    {
      id: '01JA2B3C4D5E6F7G8H9J0K1CV0',
      created_at: '2026-09-03T14:20:00Z',
      updated_at: '2026-09-03T14:20:00Z',
    },
    {
      id: '01JA2B3C4D5E6F7G8H9J0K1CVZ',
      created_at: '2026-08-21T10:05:00Z',
      updated_at: '2026-08-21T10:05:00Z',
    },
  ]
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

interface Reply {
  status: number
  body?: unknown
}

export interface CVStoreOptions {
  cvs?: CVSummary[]
  /** Held until resolved, so tests can see the "processing" state. */
  uploadGate?: Promise<void>
  /** Answer POST /cv/ with this instead of 202. */
  uploadReply?: Reply
  deleteReply?: Reply
  downloadReply?: Reply
  downloadText?: string
}

/**
 * A stateful CV API on top of mockApi(): uploads add a CV, deletes remove
 * one, and GET /cv/ reflects both. Call after mockApi() so these routes win.
 */
export async function mockCVStore(page: Page, options: CVStoreOptions = {}) {
  const state = {
    cvs: [...(options.cvs ?? [])],
    uploads: [] as { fileName: string; field: string }[],
    deleted: [] as string[],
  }
  let n = 0

  await page.route('**/api/cv/', async (route) => {
    const request = route.request()
    if (request.method() === 'GET') return json(route, 200, state.cvs)
    if (request.method() !== 'POST') return route.fallback()
    const raw = request.postDataBuffer()?.toString('latin1') ?? ''
    const match = raw.match(/name="([^"]+)"; filename="([^"]*)"/)
    state.uploads.push({ field: match?.[1] ?? '', fileName: match?.[2] ?? '' })
    if (options.uploadGate) await options.uploadGate
    if (options.uploadReply) {
      return json(route, options.uploadReply.status, options.uploadReply.body ?? {})
    }
    n += 1
    const now = new Date(Date.UTC(2026, 9, 16, 9, n)).toISOString()
    state.cvs.unshift({ id: `01JA2B3C4D5E6F7G8H9J0KNEW${n}`, created_at: now, updated_at: now })
    return route.fulfill({ status: 202, body: '' })
  })

  await page.route('**/api/cv/download/*', (route) => {
    if (options.downloadReply) {
      return json(route, options.downloadReply.status, options.downloadReply.body ?? {})
    }
    return route.fulfill({
      status: 200,
      contentType: 'text/plain; charset=utf-8',
      headers: { 'Content-Disposition': 'attachment; filename="cv.txt"' },
      body: options.downloadText ?? 'Jane Doe\nSenior Go engineer',
    })
  })

  await page.route('**/api/cv/*', (route) => {
    if (route.request().method() !== 'DELETE') return route.fallback()
    if (options.deleteReply) {
      return json(route, options.deleteReply.status, options.deleteReply.body ?? {})
    }
    const id = route.request().url().split('/').pop() ?? ''
    state.deleted.push(id)
    state.cvs = state.cvs.filter((cv) => cv.id !== id)
    return route.fulfill({ status: 204, body: '' })
  })

  return state
}

/** A small valid-looking PDF for upload tests. */
export function pdfFile(name = 'jane-doe-2026.pdf', bytes = 2048) {
  const header = Buffer.from('%PDF-1.4\n')
  return {
    name,
    mimeType: 'application/pdf',
    buffer: Buffer.concat([header, Buffer.alloc(Math.max(0, bytes - header.length), 0x20)]),
  }
}

// ---------------------------------------------------------------------------
// Quick match (POST /match/job-description)

export const sampleMatch = {
  requirements: [
    {
      requirement: '5+ years building backend services in Go',
      importance: 'must',
      met: 'yes',
      evidence: 'Six years of Go at Lunar and Pleo, owning payment services.',
    },
    {
      requirement: 'React and TypeScript product work',
      importance: 'must',
      met: 'no',
      evidence: 'No frontend work is mentioned in the CV.',
    },
    {
      requirement: 'PostgreSQL in production',
      importance: 'must',
      met: 'yes',
      evidence: 'Ran and tuned PostgreSQL clusters for the ledger service.',
    },
    {
      requirement: 'Experience with consumer products',
      importance: 'nice',
      met: 'partial',
      evidence: 'B2B fintech products only, though some were user facing.',
    },
  ],
  breakdown: [
    { criterion: 'skills', score: 62, reason: 'Strong Go and SQL; no React or TypeScript.' },
    { criterion: 'experience', score: 80, reason: 'Six years of relevant backend work.' },
    { criterion: 'seniority', score: 84, reason: 'Senior roles with service ownership.' },
    { criterion: 'domain', score: 45, reason: 'Fintech, while this is a consumer app.' },
  ],
  overall_score: 68,
  strengths: ['Go APIs and PostgreSQL', 'Senior ownership of services'],
  missing_skills: ['React and TypeScript', 'Consumer product experience'],
  summary:
    "Your Go backend depth is a strong fit, but this role is half frontend. Expect to be asked about React and TypeScript product work, which your CV doesn't show.",
}

export const sampleJobDescription = `Product Engineer (Go, TypeScript)

We're looking for a product engineer to build features end to end: Go services, a PostgreSQL database and a React + TypeScript web app. You'll own features from idea to production.

Requirements: 5+ years with Go, React and TypeScript, PostgreSQL. Consumer product experience is a plus.`

export interface QuickMatchOptions {
  gate?: Promise<void>
  reply?: Reply
}

/** Mocks POST /match/job-description; returns the request bodies it saw. */
export async function mockQuickMatch(page: Page, options: QuickMatchOptions = {}) {
  const bodies: unknown[] = []
  await page.route('**/api/match/job-description', async (route) => {
    bodies.push(route.request().postDataJSON())
    if (options.gate) await options.gate
    const reply = options.reply ?? { status: 200, body: sampleMatch }
    return json(route, reply.status, reply.body)
  })
  return bodies
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
