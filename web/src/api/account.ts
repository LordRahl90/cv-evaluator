// Account management endpoints: PATCH /me, POST /me/password, DELETE /me and
// GET /me/export. Shapes follow internal/servers/account and the "Users"
// section of api/openapi.yaml.
import { request, type RequestOptions } from './client'
import type { Session, User } from './types'

/** Password rules, the same as at signup (auth.MinPasswordLength, MaxPasswordBytes). */
export const PASSWORD_MIN_LENGTH = 8
export const PASSWORD_MAX_BYTES = 72

/** Error code for a wrong current_password (or password, on deletion). A 400, not a 401. */
export const INCORRECT_PASSWORD = 'incorrect_password'

/** The file name the API offers the export as. */
export const EXPORT_FILENAME = 'cv-evaluator-export.json'

/** PATCH /me body: only the fields present change. */
export interface ProfileChanges {
  first_name?: string
  last_name?: string
  email?: string
  phone?: string
}

/** POST /me/password body. */
export interface PasswordChange {
  current_password: string
  new_password: string
}

/** GET /me/export: everything stored about the account. */
export interface AccountExport {
  object: 'account_export'
  exported_at: string
  profile: {
    id: string
    first_name: string
    last_name: string
    email: string
    phone: string
    created_at: string
    updated_at: string
    password_changed_at: string | null
  }
  cvs: ExportedCV[]
  job_searches: ExportedJobSearch[]
  results: ExportedResult[]
  api_keys: ExportedApiKey[]
  portal_suggestions: ExportedPortalSuggestion[]
}

export interface ExportedCV {
  id: string
  extracted_content: string
  sections: { heading: string; content: string }[]
  created_at: string
  updated_at: string
  deleted_at: string | null
}

export interface ExportedJobSearch {
  id: string
  name: string
  query: string
  location: string
  frequency: string
  enabled: boolean
  last_run_at: string | null
  next_run_at: string | null
  created_at: string
  updated_at: string
  deleted_at: string | null
}

export interface ExportedResult {
  id: string
  job_search_id: string
  status: string
  evaluation_status: string
  created_at: string
  updated_at: string
  job: {
    id: string
    provider: string
    title: string
    company: string
    location: string
    posted_at: string
    details_url: string
  } | null
  evaluations: {
    id: string
    cv_id: string
    status: string
    overall_score: number
    scores: { criterion: string; score: number; reason: string }[]
    strengths: string[]
    missing_skills: string[]
    summary: string
    error?: string
    model: string
    prompt_version: string
    created_at: string
    updated_at: string
  }[]
}

export interface ExportedApiKey {
  id: string
  name: string
  prefix: string
  created_at: string
  last_used_at: string | null
  revoked_at: string | null
}

export interface ExportedPortalSuggestion {
  id: string
  name: string
  url: string
  note: string
  status: string
  created_at: string
  updated_at: string
}

export function updateProfile(changes: ProfileChanges, options?: RequestOptions) {
  return request<User>('/me', { ...options, method: 'PATCH', body: changes })
}

/** Changes the password and returns a fresh session; older sessions stop working. */
export function changePassword(change: PasswordChange, options?: RequestOptions) {
  return request<Session>('/me/password', { ...options, method: 'POST', body: change })
}

/** Permanently deletes the account and everything it owns. */
export async function deleteAccount(password: string, options?: RequestOptions): Promise<void> {
  await request<null>('/me', { ...options, method: 'DELETE', body: { password } })
}

export function exportAccount(options?: RequestOptions) {
  return request<AccountExport>('/me/export', options)
}
