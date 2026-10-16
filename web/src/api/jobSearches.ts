// Job search endpoints: list, get, create, update (including the enabled
// switch), delete and "run now". GET /job-searches returns
// {object: "list", data: [...]}.
import { request, type RequestOptions } from './client'
import type { Frequency, JobSearch, ListResponse } from './types'

/** The frequencies the API accepts, in the order the form shows them. */
export const FREQUENCIES: readonly Frequency[] = ['hourly', 'daily', 'weekly']

/** Field limits enforced by the API (counted in characters). */
export const JOB_SEARCH_LIMITS = { name: 100, query: 200, location: 200 } as const

/** 409 code for POST /job-searches/:id/run on a disabled search. */
export const CODE_JOB_SEARCH_DISABLED = 'job_search_disabled'

/** POST /job-searches. Name defaults to the query; enabled defaults to true. */
export interface CreateJobSearchRequest {
  name?: string
  query: string
  location?: string
  frequency?: Frequency
  enabled?: boolean
}

/** PATCH /job-searches/:id. Only the fields given are changed. */
export type UpdateJobSearchRequest = Partial<CreateJobSearchRequest>

/** DELETE /job-searches/:id response. */
export interface DeletedJobSearch {
  object: 'job_search'
  id: string
  deleted: true
}

export async function listJobSearches(options?: RequestOptions): Promise<JobSearch[]> {
  const data = await request<ListResponse<JobSearch> | null>('/job-searches', options)
  return Array.isArray(data?.data) ? data.data : []
}

const path = (id: string) => `/job-searches/${encodeURIComponent(id)}`

export function getJobSearch(id: string, options?: RequestOptions) {
  return request<JobSearch>(path(id), options)
}

export function createJobSearch(body: CreateJobSearchRequest) {
  return request<JobSearch>('/job-searches', { method: 'POST', body })
}

export function updateJobSearch(id: string, body: UpdateJobSearchRequest) {
  return request<JobSearch>(path(id), { method: 'PATCH', body })
}

export function setJobSearchEnabled(id: string, enabled: boolean) {
  return updateJobSearch(id, { enabled })
}

export function deleteJobSearch(id: string) {
  return request<DeletedJobSearch>(path(id), { method: 'DELETE' })
}

/** Makes an enabled search due now (202); 409 job_search_disabled otherwise. */
export function runJobSearch(id: string) {
  return request<JobSearch>(`${path(id)}/run`, { method: 'POST' })
}
