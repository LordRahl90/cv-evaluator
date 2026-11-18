// Shapes of the results ("matches") endpoints, written by hand from
// api/openapi.yaml. No imports, so tests can use them too; results.ts
// re-exports them with the calls.

/** The user's own triage of a result. Every result starts as new. */
export type ResultStatus = 'new' | 'saved' | 'applied' | 'dismissed'

/**
 * pending → processing → done, or failed after every retry. held: the job is
 * too far from the CV to be scored automatically (the worker's similarity
 * floor); it is scored only when the user asks.
 */
export type EvaluationStatus = 'pending' | 'processing' | 'done' | 'failed' | 'held'

/**
 * Why a pending or held result is not scored yet: queued (in turn, closest
 * to the CV first), daily_cap (the daily scoring limit is reached) or
 * low_similarity (held).
 */
export type WaitingReason = 'queued' | 'daily_cap' | 'low_similarity'

export type ResultSort = 'score' | 'newest'

export interface ResultSearchRef {
  id: string
  name: string
}

export interface ResultJob {
  id: string
  title: string
  company: string
  location: string
  /** The portal, e.g. "linkedin". */
  provider: string
  details_url: string
  /** As the portal phrases it ("2 days ago" or a date). */
  posted_at: string
  /** Only on a single result. */
  description?: string
}

export interface EvaluationSummary {
  overall_score: number
  top_missing_skills: string[]
  evaluated_at: string
}

/** One entry of GET /results. */
export interface SearchResult {
  object: 'search_result'
  id: string
  status: ResultStatus
  evaluation_status: EvaluationStatus
  /** When the search found the job. */
  created_at: string
  updated_at: string
  search: ResultSearchRef
  job: ResultJob
  /** null until the result has a completed evaluation. */
  evaluation: EvaluationSummary | null
  /** Set while the result is pending or held, null otherwise. */
  waiting_reason: WaitingReason | null
}

export interface SearchResultPage {
  object: 'list'
  data: SearchResult[]
  has_more: boolean
  total_count: number
}

export interface CriterionScore {
  criterion: string
  score: number
  reason: string
}

export interface Requirement {
  requirement: string
  importance: 'must' | 'nice'
  met: 'yes' | 'partial' | 'no'
  evidence: string
}

export interface Evaluation {
  id: string
  cv_id: string
  overall_score: number
  breakdown: CriterionScore[]
  strengths: string[]
  missing_skills: string[]
  summary: string
  /** null for evaluations made before requirements were asked for. */
  requirements: Requirement[] | null
  model: string
  prompt_version: string
  evaluated_at: string
}

/** GET /results/:id, and the answer to PATCH and re-evaluate. */
export interface SearchResultDetail extends Omit<SearchResult, 'evaluation'> {
  job: ResultJob & { description: string }
  evaluation: Evaluation | null
}

export interface SearchCount {
  id: string
  name: string
  total: number
  new_this_week: number
}

/** GET /results/summary. */
export interface ResultsSummary {
  object: 'results_summary'
  week_start: string
  total: number
  scored: number
  evaluating: number
  failed_evaluation: number
  /** Not scored and not being scored yet (pending or held). */
  waiting: number
  /** The daily scoring limit is reached while results wait. */
  daily_cap_reached: boolean
  new_this_week: number
  new_this_week_searches: number
  strong_score: number
  strong_matches: number
  strong_matches_new: number
  by_status: Record<ResultStatus, number>
  by_search: SearchCount[]
}

export interface ListResultsParams {
  /** Limit to one search (GET /job-searches/:id/results). */
  searchId?: string
  minScore?: number
  status?: ResultStatus[]
  evaluationStatus?: EvaluationStatus[]
  /** ISO timestamp: only results found since then. */
  since?: string
  sort?: ResultSort
  limit?: number
  /** The id of the last result of the previous page. */
  startingAfter?: string
}
