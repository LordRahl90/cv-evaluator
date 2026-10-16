// Results ("matches") endpoints and their shapes, written by hand from
// api/openapi.yaml (GET/PATCH /results…, GET /results/summary,
// POST /results/:id/re-evaluate, GET /job-searches/:id/results).
import { request, type RequestOptions } from './client'

import type {
  ListResultsParams,
  ResultStatus,
  ResultsSummary,
  SearchResultDetail,
  SearchResultPage,
} from './resultTypes'

export type {
  ResultStatus,
  EvaluationStatus,
  ResultSort,
  ResultSearchRef,
  ResultJob,
  EvaluationSummary,
  SearchResult,
  SearchResultPage,
  CriterionScore,
  Requirement,
  Evaluation,
  SearchResultDetail,
  SearchCount,
  ResultsSummary,
  ListResultsParams,
} from './resultTypes'

export async function listResults(
  params: ListResultsParams = {},
  options?: RequestOptions,
): Promise<SearchResultPage> {
  const query = new URLSearchParams()
  if (params.minScore !== undefined) query.set('min_score', String(params.minScore))
  if (params.status?.length) query.set('status', params.status.join(','))
  if (params.evaluationStatus?.length)
    query.set('evaluation_status', params.evaluationStatus.join(','))
  if (params.since) query.set('since', params.since)
  if (params.sort) query.set('sort', params.sort)
  if (params.limit !== undefined) query.set('limit', String(params.limit))
  if (params.startingAfter) query.set('starting_after', params.startingAfter)

  const base = params.searchId
    ? `/job-searches/${encodeURIComponent(params.searchId)}/results`
    : '/results'
  const qs = query.toString()
  const data = await request<SearchResultPage | null>(qs ? `${base}?${qs}` : base, options)
  return {
    object: 'list',
    data: Array.isArray(data?.data) ? data.data : [],
    has_more: Boolean(data?.has_more),
    total_count: data?.total_count ?? 0,
  }
}

export function getResultsSummary(options?: RequestOptions): Promise<ResultsSummary> {
  return request<ResultsSummary>('/results/summary', options)
}

export function getResult(id: string, options?: RequestOptions): Promise<SearchResultDetail> {
  return request<SearchResultDetail>(`/results/${encodeURIComponent(id)}`, options)
}

export function updateResultStatus(
  id: string,
  status: ResultStatus,
  options?: RequestOptions,
): Promise<SearchResultDetail> {
  return request<SearchResultDetail>(`/results/${encodeURIComponent(id)}`, {
    ...options,
    method: 'PATCH',
    body: { status },
  })
}

/** Queues the result to be scored again; 409 while it already is. */
export function reEvaluateResult(
  id: string,
  options?: RequestOptions,
): Promise<SearchResultDetail> {
  return request<SearchResultDetail>(`/results/${encodeURIComponent(id)}/re-evaluate`, {
    ...options,
    method: 'POST',
  })
}
