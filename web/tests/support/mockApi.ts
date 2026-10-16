// Network-level API mocks for Playwright. Every call the app makes goes to
// /api/*; these handlers answer it so tests need no Go server or database.
import type { Page, Route } from '@playwright/test'
import type { CVSummary, JobSearch, User } from '../../src/api/types'
import type {
  EvaluationStatus,
  ResultJob,
  ResultStatus,
  ResultsSummary,
  SearchResult,
  SearchResultDetail,
} from '../../src/api/resultTypes'

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
  // No matches unless a test calls mockResults with some (defined below).
  await mockResults(page)
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

// ── Matches (search results) ────────────────────────────────────────────────

const [goSearch, backendSearch, contractSearch] = [
  { id: '01JA2B3C4D5E6F7G8H9J0KS001', name: 'Senior Go · Copenhagen' },
  { id: '01JA2B3C4D5E6F7G8H9J0KS002', name: 'Backend · Remote EU' },
  { id: '01JA2B3C4D5E6F7G8H9J0KS003', name: 'Go contractor · Berlin' },
]

function result(
  n: number,
  search: { id: string; name: string },
  job: Pick<ResultJob, 'title' | 'company' | 'location' | 'provider'>,
  opts: {
    score: number | null
    status?: ResultStatus
    evaluation?: EvaluationStatus
    missing?: string[]
    hoursAgo: number
  },
): SearchResult {
  const found = hoursFromNow(-opts.hoursAgo)
  return {
    object: 'search_result',
    id: `01JA2B3C4D5E6F7G8H9J0KR${String(n).padStart(3, '0')}`,
    status: opts.status ?? 'new',
    evaluation_status: opts.evaluation ?? (opts.score === null ? 'pending' : 'done'),
    created_at: found,
    updated_at: found,
    search,
    job: {
      id: `01JA2B3C4D5E6F7G8H9J0KJ${String(n).padStart(3, '0')}`,
      ...job,
      details_url: `https://example.com/jobs/${n}`,
      posted_at: '2 days ago',
    },
    evaluation:
      opts.score === null
        ? null
        : {
            overall_score: opts.score,
            top_missing_skills: opts.missing ?? [],
            evaluated_at: found,
          },
  }
}

/** Six matches across three searches, best first, like the mockups. */
export function sampleResults(): SearchResult[] {
  return [
    result(
      1,
      goSearch,
      {
        title: 'Senior Go Engineer',
        company: 'Northwind Logistics',
        location: 'Copenhagen',
        provider: 'linkedin',
      },
      { score: 91, missing: ['Terraform'], hoursAgo: 2 },
    ),
    result(
      2,
      backendSearch,
      {
        title: 'Backend Engineer, Payments',
        company: 'Kestrel Pay',
        location: 'Remote (EU)',
        provider: 'linkedin',
      },
      { score: 86, status: 'saved', missing: ['PCI DSS', 'Kotlin'], hoursAgo: 5 },
    ),
    result(
      3,
      goSearch,
      {
        title: 'Staff Software Engineer, Platform',
        company: 'Fjord Analytics',
        location: 'Aarhus',
        provider: 'jobindex',
      },
      { score: 82, status: 'applied', missing: ['Staff-level scope'], hoursAgo: 26 },
    ),
    result(
      4,
      goSearch,
      {
        title: 'Senior Software Engineer, Search',
        company: 'Lindqvist Retail',
        location: 'Copenhagen',
        provider: 'linkedin',
      },
      { score: 78, missing: ['Elasticsearch', 'Java'], hoursAgo: 30 },
    ),
    result(
      5,
      backendSearch,
      {
        title: 'Senior Backend Developer',
        company: 'Hollow Pine Games',
        location: 'Copenhagen',
        provider: 'jobindex',
      },
      { score: 74, missing: ['C#', 'Game backends'], hoursAgo: 50 },
    ),
    result(
      6,
      contractSearch,
      {
        title: 'Go Developer (Contract, 6 months)',
        company: 'Tidewater Media',
        location: 'Berlin',
        provider: 'linkedin',
      },
      { score: 71, evaluation: 'pending', missing: ['German (B2)'], hoursAgo: 75 },
    ),
  ]
}

/** Jobs found but none scored yet: the "scoring in progress" state. */
export function unscoredResults(): SearchResult[] {
  return [
    result(
      11,
      goSearch,
      { title: 'Go Engineer', company: 'Acme', location: 'Copenhagen', provider: 'linkedin' },
      { score: null, evaluation: 'processing', hoursAgo: 1 },
    ),
    result(
      12,
      goSearch,
      {
        title: 'Platform Engineer',
        company: 'Initech',
        location: 'Copenhagen',
        provider: 'jobindex',
      },
      { score: null, hoursAgo: 1 },
    ),
  ]
}

/** The full view of a listed result, with a complete evaluation. */
export function sampleDetail(r: SearchResult = sampleResults()[0]!): SearchResultDetail {
  return {
    ...r,
    job: {
      ...r.job,
      description:
        "We're building the routing engine behind same-day delivery across the Nordics. You'll own Go services that process 40k shipment events a minute, work with Kafka and PostgreSQL, and help us move our infrastructure to Terraform. ".repeat(
          3,
        ),
    },
    evaluation:
      r.evaluation === null
        ? null
        : {
            id: '01JA2B3C4D5E6F7G8H9J0KEV01',
            cv_id: sampleCV.id,
            overall_score: r.evaluation.overall_score,
            breakdown: [
              { criterion: 'skills', score: 94, reason: 'Go, Kafka and PostgreSQL in production.' },
              { criterion: 'experience', score: 90, reason: 'Six years on distributed backends.' },
              { criterion: 'seniority', score: 88, reason: 'Senior scope matches the role.' },
              { criterion: 'domain', score: 76, reason: 'No logistics background.' },
              { criterion: 'education', score: 80, reason: 'Computer science degree.' },
            ],
            strengths: [
              '6 years of production Go',
              'Kafka and PostgreSQL event pipelines',
              'Observability with OpenTelemetry',
            ],
            missing_skills: [...r.evaluation.top_missing_skills],
            summary:
              "A close fit. Your six years of Go on distributed backend systems line up with the role's core, and your Kafka and PostgreSQL experience covers their event pipeline stack. The main gap is infrastructure as code.",
            requirements: [
              {
                requirement: '5+ years of Go',
                importance: 'must',
                met: 'yes',
                evidence: 'Six years of Go at two companies.',
              },
              {
                requirement: 'Kafka in production',
                importance: 'must',
                met: 'partial',
                evidence: 'Used Kafka on one project.',
              },
              { requirement: 'Terraform', importance: 'nice', met: 'no', evidence: '' },
            ],
            model: 'deepseek-chat',
            prompt_version: 'v4',
            evaluated_at: '2026-10-16T07:04:00Z',
          },
  }
}

/** A summary consistent with the given results and searches. */
export function summarize(results: SearchResult[]): ResultsSummary {
  const week = Date.now() - 7 * 24 * 3_600_000
  const fresh = results.filter((r) => new Date(r.created_at).getTime() >= week)
  const strong = results.filter(
    (r) => (r.evaluation?.overall_score ?? 0) >= 80 && r.status !== 'dismissed',
  )
  const count = (s: ResultStatus) => results.filter((r) => r.status === s).length
  return {
    object: 'results_summary',
    week_start: new Date(week).toISOString(),
    total: results.length,
    scored: results.filter((r) => r.evaluation).length,
    evaluating: results.filter((r) => ['pending', 'processing'].includes(r.evaluation_status))
      .length,
    failed_evaluation: results.filter((r) => r.evaluation_status === 'failed').length,
    new_this_week: fresh.length,
    new_this_week_searches: new Set(fresh.map((r) => r.search.id)).size,
    strong_score: 80,
    strong_matches: strong.length,
    strong_matches_new: strong.filter((r) => r.status === 'new').length,
    by_status: {
      new: count('new'),
      saved: count('saved'),
      applied: count('applied'),
      dismissed: count('dismissed'),
    },
    by_search: [goSearch, backendSearch, contractSearch].map((s) => ({
      ...s,
      total: results.filter((r) => r.search.id === s.id).length,
      new_this_week: fresh.filter((r) => r.search.id === s.id).length,
    })),
  }
}

export interface ResultsMockOptions {
  results?: SearchResult[]
  /** Defaults to summarize(results); pass null to make it fail. */
  summary?: ResultsSummary | null
  /** Full views by id; defaults to sampleDetail(listed result). */
  details?: Record<string, SearchResultDetail>
  /** Held until resolved, for loading states. */
  gate?: Promise<void>
}

/**
 * Mocks the results API over an in-memory list: GET /results (and
 * /job-searches/:id/results) filters, sorts and pages it like the server,
 * GET/PATCH /results/:id and POST /results/:id/re-evaluate read and change
 * it. Returns the list's request URLs and the write calls for assertions.
 */
export async function mockResults(page: Page, options: ResultsMockOptions = {}) {
  const results = (options.results ?? []).map((r) => ({ ...r }))
  const details = { ...options.details }
  const listCalls: URL[] = []
  const writes: { method: string; path: string; body: unknown }[] = []
  const hold = async () => {
    if (options.gate) await options.gate
  }

  const list = async (route: Route) => {
    await hold()
    const url = new URL(route.request().url())
    listCalls.push(url)
    const q = url.searchParams
    const searchId = url.pathname.match(/job-searches\/([^/]+)\/results/)?.[1]
    let rows = results.filter((r) => !searchId || r.search.id === searchId)
    const min = q.get('min_score')
    if (min) rows = rows.filter((r) => (r.evaluation?.overall_score ?? -1) >= Number(min))
    const statuses = q.get('status')?.split(',').filter(Boolean)
    if (statuses?.length) rows = rows.filter((r) => statuses.includes(r.status))
    const since = q.get('since')
    if (since) rows = rows.filter((r) => r.created_at >= since)
    const score = (r: SearchResult) => r.evaluation?.overall_score ?? -1
    rows.sort((a, b) =>
      q.get('sort') === 'newest'
        ? b.created_at.localeCompare(a.created_at)
        : score(b) - score(a) || b.id.localeCompare(a.id),
    )
    const total = rows.length
    const after = q.get('starting_after')
    if (after) rows = rows.slice(rows.findIndex((r) => r.id === after) + 1)
    const limit = Number(q.get('limit') ?? 20)
    return json(route, 200, {
      object: 'list',
      data: rows.slice(0, limit),
      has_more: rows.length > limit,
      total_count: total,
    })
  }

  const detailFor = (r: SearchResult): SearchResultDetail => {
    const base = details[r.id] ?? sampleDetail(r)
    return { ...base, status: r.status, evaluation_status: r.evaluation_status }
  }

  await page.route(
    (url) => /\/api\/(results|job-searches\/[^/]+\/results)$/.test(url.pathname),
    list,
  )
  await page.route('**/api/results/summary', async (route) => {
    await hold()
    if (options.summary === null) {
      return json(route, 500, apiError('api_error', 'internal_error', 'Summary failed.'))
    }
    return json(route, 200, options.summary ?? summarize(results))
  })
  await page.route(/\/api\/results\/(?!summary)[^/?]+(\/re-evaluate)?$/, async (route) => {
    await hold()
    const req = route.request()
    const [, id, action] = new URL(req.url()).pathname.match(/results\/([^/]+)(\/re-evaluate)?$/)!
    const r = results.find((x) => x.id === id) ?? (details[id!] as SearchResult | undefined)
    if (!r) {
      return json(
        route,
        404,
        apiError('invalid_request_error', 'resource_missing', 'No such result.', 'id'),
      )
    }
    if (req.method() === 'PATCH') {
      const body = req.postDataJSON() as { status: ResultStatus }
      writes.push({ method: 'PATCH', path: `/results/${id}`, body })
      r.status = body.status
      if (details[id!]) details[id!] = { ...details[id!]!, status: body.status }
    } else if (req.method() === 'POST' && action) {
      writes.push({ method: 'POST', path: `/results/${id}/re-evaluate`, body: null })
      r.evaluation_status = 'pending'
      if (details[id!]) details[id!] = { ...details[id!]!, evaluation_status: 'pending' }
      return json(route, 202, detailFor(r))
    }
    return json(route, 200, detailFor(r))
  })
  return { listCalls, writes, results }
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

// ---------------------------------------------------------------------------
// Cover letters (POST /match/cover-letter)

/** A quick match strong enough (80+) for the cover letter banner. */
export const sampleStrongMatch = {
  requirements: [
    {
      requirement: '5+ years of backend development, at least 3 in Go',
      importance: 'must',
      met: 'yes',
      evidence: 'Eight years of backend work, the last six in Go.',
    },
    {
      requirement: 'Kafka or another event streaming platform',
      importance: 'must',
      met: 'yes',
      evidence: 'Go services processing 30,000 shipment events per minute on Kafka.',
    },
    {
      requirement: 'Experience with observability (tracing, metrics)',
      importance: 'must',
      met: 'yes',
      evidence: 'Introduced OpenTelemetry tracing; incident resolution time down 40%.',
    },
    {
      requirement: 'Terraform',
      importance: 'nice',
      met: 'no',
      evidence: '',
    },
  ],
  breakdown: [
    { criterion: 'skills', score: 90, reason: 'Go, Kafka and PostgreSQL in production.' },
    { criterion: 'experience', score: 88, reason: 'Six years owning event-driven Go services.' },
    { criterion: 'seniority', score: 85, reason: 'Senior role, mentors three engineers.' },
    { criterion: 'domain', score: 72, reason: 'Logistics, though not routing itself.' },
    { criterion: 'education', score: 80, reason: 'BSc Computer Science.' },
  ],
  overall_score: 86,
  strengths: [
    '6 years of production Go',
    'Kafka pipelines at 30k events a minute',
    'OpenTelemetry tracing rollout',
  ],
  missing_skills: ['Terraform (nice to have)'],
  summary:
    'You meet every must-have: six years of Go on event pipelines, Kafka and PostgreSQL in production, and hands-on tracing work. Lead with your event-sourcing migration; be ready to talk about Terraform.',
}

export const sampleStrongJobDescription = `Senior Backend Engineer (Go)

Northwind Logistics builds the routing engine behind same-day delivery. You will own Go services processing shipment events.

Requirements: 5+ years of backend development, at least 3 in Go. Kafka or another event streaming platform. PostgreSQL. Experience with observability (tracing, metrics). Terraform is a plus.`

export const sampleCoverLetter = {
  subject: 'Application for Senior Backend Engineer (Go)',
  letter: `Dear Hiring Manager,

I am applying for the Senior Backend Engineer (Go) position at Northwind Logistics. For the past six years I have built and operated Go services that process high volumes of events, which is the core of the role you describe.

At Parcelflow I own the Go microservices that process 30,000 shipment events per minute on Kafka and PostgreSQL. I led the move of these services to event sourcing, which lets us replay shipment state without downtime. Your posting asks for production experience with Kafka and PostgreSQL; I have run both at this scale for over four years.

You also ask for experience with observability. I introduced OpenTelemetry tracing across our services, which cut the time to resolve incidents by 40%. I would apply the same approach to your routing engine, where a delayed event affects same-day delivery.

I have not yet used Terraform in production. I maintain our CI/CD pipelines in GitHub Actions and our AWS resources, and I would expect to work in your infrastructure-as-code setup within my first months.

I would welcome the opportunity to discuss how my experience with event-driven Go services fits your team's plans.

Kind regards,

Jane Doe`,
  word_count: 236,
}

export interface CoverLetterOptions {
  /** Held until resolved, so tests can see the writing state. */
  gate?: Promise<void>
  /** Answers in turn; the last one repeats. Default: sampleCoverLetter. */
  replies?: Reply[]
}

/** Mocks POST /match/cover-letter; returns the request bodies it saw. */
export async function mockCoverLetter(page: Page, options: CoverLetterOptions = {}) {
  const bodies: unknown[] = []
  const replies = options.replies ?? [{ status: 200, body: sampleCoverLetter }]
  await page.route('**/api/match/cover-letter', async (route) => {
    const reply = replies[Math.min(bodies.length, replies.length - 1)]
    bodies.push(route.request().postDataJSON())
    if (options.gate) await options.gate
    return json(route, reply.status, reply.body)
  })
  return bodies
}

/** The API's cover letter errors, as the Go handler writes them. */
export const coverLetterErrors = {
  noCV: {
    status: 422,
    body: apiError(
      'invalid_request_error',
      'cv_missing',
      'Upload a CV before writing a cover letter.',
    ),
  },
  rateLimited: {
    status: 429,
    body: apiError(
      'rate_limit_error',
      'rate_limited',
      'Too many cover letters in a short time (at most 5 per minute). Try again in 42 seconds.',
    ),
  },
  failed: {
    status: 502,
    body: apiError(
      'api_error',
      'cover_letter_failed',
      'We could not write a cover letter that meets our quality checks. Please try again.',
    ),
  },
} satisfies Record<string, Reply>
