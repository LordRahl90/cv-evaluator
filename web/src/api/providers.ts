// GET /providers: the job portals every search runs on. Informational only;
// users never pick a portal.
import { request, type RequestOptions } from './client'
import type { ListResponse } from './types'

export interface Provider {
  object: 'provider'
  /** Stable identifier, e.g. "linkedin". */
  name: string
  display_name: string
  website: string
  /** Country or region the portal lists jobs for, e.g. "Denmark". */
  coverage: string
}

export async function listProviders(options?: RequestOptions): Promise<Provider[]> {
  const data = await request<ListResponse<Provider> | null>('/providers', options)
  return Array.isArray(data?.data) ? data.data : []
}
