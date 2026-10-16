// Job search endpoints. GET /job-searches returns {object: "list", data: [...]}.
import { request, type RequestOptions } from './client'
import type { JobSearch, ListResponse } from './types'

export async function listJobSearches(options?: RequestOptions): Promise<JobSearch[]> {
  const data = await request<ListResponse<JobSearch> | null>('/job-searches', options)
  return Array.isArray(data?.data) ? data.data : []
}
