// Personal API key endpoints: GET /api-keys, POST /api-keys and
// DELETE /api-keys/:id. Shapes follow internal/servers/apikeys and the
// "API keys" section of api/openapi.yaml.
import { request, type RequestOptions } from './client'
import type { ListResponse } from './types'

/** Longest name the API accepts (apikeys.MaxNameLength). */
export const API_KEY_NAME_MAX = 100

/** One key as listed. The secret is never returned here, only `prefix`. */
export interface ApiKey {
  object: 'api_key'
  id: string
  name: string
  /** The first characters of the secret, e.g. "sk_live_7Qm2". */
  prefix: string
  created_at: string
  last_used_at: string | null
}

/** POST /api-keys response: the only time the full secret is sent. */
export interface CreatedApiKey extends ApiKey {
  secret: string
}

export interface DeletedApiKey {
  object: 'api_key'
  id: string
  deleted: true
}

export async function listApiKeys(options?: RequestOptions): Promise<ApiKey[]> {
  const data = await request<ListResponse<ApiKey> | null>('/api-keys', options)
  return Array.isArray(data?.data) ? data.data : []
}

export function createApiKey(name: string, options?: RequestOptions) {
  return request<CreatedApiKey>('/api-keys', { ...options, method: 'POST', body: { name } })
}

export function revokeApiKey(id: string, options?: RequestOptions) {
  return request<DeletedApiKey>(`/api-keys/${encodeURIComponent(id)}`, {
    ...options,
    method: 'DELETE',
  })
}
