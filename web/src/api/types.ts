// API shapes, written by hand from api/openapi.yaml and the Go handlers on
// main. Keep these in step with the backend until we add codegen.

/** {"error":{...}} body used by the newer routes (auth, /me, job searches). */
export interface ApiErrorBody {
  type: string
  code: string
  message: string
  param?: string
}

/** User, as returned by GET /me and inside a session. */
export interface User {
  object: 'user'
  id: string
  first_name: string
  last_name: string
  email: string
  phone: string
}

/** POST /auth/signup and POST /auth/signin response. */
export interface Session {
  object: 'session'
  token: string
  token_type: 'Bearer'
  user: User
}

export interface SignUpRequest {
  first_name: string
  last_name: string
  email: string
  phone?: string
  password: string
}

export interface SignInRequest {
  email: string
  password: string
}

/** One entry of GET /cv/ (a bare JSON array). */
export interface CVSummary {
  id: string
  created_at: string
  updated_at: string
}

export type Frequency = 'hourly' | 'daily' | 'weekly'

/** One entry of GET /job-searches. */
export interface JobSearch {
  object: 'job_search'
  id: string
  name: string
  query: string
  location: string
  frequency: Frequency | string
  enabled: boolean
  last_run_at: string | null
  next_run_at: string | null
  created_at: string
  updated_at: string
}

export interface ListResponse<T> {
  object: 'list'
  data: T[]
}
