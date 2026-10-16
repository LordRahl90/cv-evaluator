// The one place that talks HTTP to the Go API: base URL, auth header and
// error decoding. Feature modules (auth.ts, cvs.ts, …) build on request().
import { getToken } from './token'
import type { ApiErrorBody } from './types'

// In dev and preview Vite proxies /api to the Go server (vite.config.ts).
// A deployment that serves the API elsewhere sets VITE_API_BASE_URL.
export const API_BASE_URL = (import.meta.env.VITE_API_BASE_URL ?? '/api').replace(/\/$/, '')

interface ApiErrorInit {
  status: number
  message: string
  type?: string | null
  code?: string | null
  param?: string | null
}

export class ApiError extends Error {
  readonly status: number
  readonly type: string | null
  readonly code: string | null
  readonly param: string | null

  constructor({ status, message, type = null, code = null, param = null }: ApiErrorInit) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.type = type
    this.code = code
    this.param = param
  }
}

type UnauthorizedHandler = (error: ApiError) => void
let unauthorizedHandler: UnauthorizedHandler | null = null

/**
 * Registers what happens when an authenticated call comes back 401
 * (expired or revoked token). main.ts signs the user out and redirects.
 */
export function onUnauthorized(handler: UnauthorizedHandler): void {
  unauthorizedHandler = handler
}

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE'
  body?: unknown
  /**
   * Send the session token and treat a 401 as an expired session.
   * Sign-in and sign-up pass false so a 401 there is shown as a form error.
   */
  auth?: boolean
  signal?: AbortSignal
}

/** Sends a request to the API and returns the decoded JSON body (or null). */
export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const response = await send(path, options)
  return (await readJson(response)) as T
}

/** Like request(), for endpoints that answer with a file (e.g. a download). */
export async function requestBlob(path: string, options: RequestOptions = {}): Promise<Blob> {
  const response = await send(path, options)
  return response.blob()
}

/**
 * Sends the request and turns failures into ApiError. A FormData body goes
 * out as multipart (the browser sets the boundary); anything else as JSON.
 */
async function send(
  path: string,
  { method = 'GET', body, auth = true, signal }: RequestOptions,
): Promise<Response> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  const isForm = body instanceof FormData
  if (body !== undefined && !isForm) headers['Content-Type'] = 'application/json'
  const token = auth ? getToken() : null
  if (token) headers.Authorization = `Bearer ${token}`

  let response: Response
  try {
    response = await fetch(API_BASE_URL + path, {
      method,
      headers,
      body: body === undefined ? undefined : isForm ? body : JSON.stringify(body),
      signal,
    })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err
    throw new ApiError({
      status: 0,
      message: "Can't reach the server. Check your connection and try again.",
    })
  }

  if (!response.ok) {
    const error = toApiError(response.status, await readJson(response))
    if (response.status === 401 && auth && unauthorizedHandler) unauthorizedHandler(error)
    throw error
  }
  return response
}

async function readJson(response: Response): Promise<unknown> {
  const text = await response.text()
  if (!text) return null
  try {
    return JSON.parse(text)
  } catch {
    return null
  }
}

/**
 * The newer routes answer {"error":{"type","code","message","param"}};
 * some older ones answer {"error":"message"}. Both become an ApiError.
 */
export function toApiError(status: number, data: unknown): ApiError {
  const err = (data as { error?: unknown } | null)?.error
  if (err && typeof err === 'object') {
    const body = err as Partial<ApiErrorBody>
    return new ApiError({
      status,
      message: body.message || fallbackMessage(status),
      type: body.type ?? null,
      code: body.code ?? null,
      param: body.param || null,
    })
  }
  if (typeof err === 'string' && err) {
    return new ApiError({ status, message: err.charAt(0).toUpperCase() + err.slice(1) })
  }
  return new ApiError({ status, message: fallbackMessage(status) })
}

function fallbackMessage(status: number): string {
  if (status === 401) return 'Your session has expired. Sign in again.'
  if (status === 404) return 'We could not find what you asked for.'
  if (status >= 500) return 'Something went wrong on our side. Try again in a moment.'
  return 'The request could not be completed.'
}
