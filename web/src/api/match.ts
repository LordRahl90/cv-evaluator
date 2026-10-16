// Matching endpoints. POST /match/job-description scores a pasted job
// description against the user's newest CV; it calls the LLM inside the
// request, so it usually takes 5-30 seconds. A 422 means there is no CV yet.
import { ApiError, request, type RequestOptions } from './client'

export type Importance = 'must' | 'nice'
export type Met = 'yes' | 'partial' | 'no'

/** One requirement read from the job, and whether the CV meets it. */
export interface MatchRequirement {
  requirement: string
  importance: Importance | string
  met: Met | string
  evidence: string
}

/** One scored criterion (skills, experience, …), 0-100. */
export interface CriterionScore {
  criterion: string
  score: number
  reason: string
}

/** POST /match/job-description response. */
export interface MatchResult {
  /** Newer than the rest of the response; older servers leave it out. */
  requirements?: MatchRequirement[] | null
  breakdown: CriterionScore[] | null
  overall_score: number
  strengths: string[] | null
  missing_skills: string[] | null
  summary: string
}

export function matchJobDescription(jobDescription: string, options?: RequestOptions) {
  return request<MatchResult>('/match/job-description', {
    ...options,
    method: 'POST',
    body: { job_description: jobDescription },
  })
}

/** The 422 the API returns when the user has no CV to match against. */
export function isNoCVError(err: unknown): boolean {
  return err instanceof ApiError && err.status === 422
}
