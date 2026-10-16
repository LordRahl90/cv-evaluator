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
