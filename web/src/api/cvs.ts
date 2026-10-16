// CV endpoints. GET /cv/ returns a bare array of {id, created_at, updated_at}.
import { request, type RequestOptions } from './client'
import type { CVSummary } from './types'

export async function listCVs(options?: RequestOptions): Promise<CVSummary[]> {
  const data = await request<CVSummary[] | null>('/cv/', options)
  return Array.isArray(data) ? data : []
}
