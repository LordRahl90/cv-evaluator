// Display helpers for matches (search results): score tiers, status and
// evaluation labels, portal and criterion names.
import { ApiError } from '../api/client'
import type { EvaluationStatus, ResultStatus, WaitingReason } from '../api/results'

/** Overall score from which a match counts as strong (the API's strong_score). */
export const STRONG_SCORE = 80

export type ScoreTone = 'strong' | 'fair' | 'none'

/** Green from 80, amber below, grey while unscored, as in the mockups. */
export function scoreTone(score: number | null | undefined): ScoreTone {
  if (score === null || score === undefined) return 'none'
  return score >= STRONG_SCORE ? 'strong' : 'fair'
}

const PROVIDERS: Record<string, string> = { linkedin: 'LinkedIn', jobindex: 'Jobindex' }

export function providerLabel(provider: string): string {
  return (
    PROVIDERS[provider] ?? (provider ? provider.charAt(0).toUpperCase() + provider.slice(1) : '')
  )
}

export function criterionLabel(criterion: string): string {
  return criterion ? criterion.charAt(0).toUpperCase() + criterion.slice(1) : ''
}

export const STATUS_LABELS: Record<ResultStatus, string> = {
  new: 'New',
  saved: 'Saved',
  applied: 'Applied',
  dismissed: 'Dismissed',
}

/** Badge class per status, from the mockups' New / Saved / Applied pills. */
export const STATUS_BADGES: Record<ResultStatus, string> = {
  new: 'badge-new',
  saved: 'badge-green',
  applied: 'badge-blue',
  dismissed: 'badge-grey',
}

export function isEvaluating(status: EvaluationStatus): boolean {
  return status === 'pending' || status === 'processing'
}

/**
 * Whether a result has never been scored and is waiting for its turn (or
 * held), so the user can ask for it to be scored now. A result being
 * re-scored keeps its old score and is not "waiting".
 */
export function isWaiting(status: EvaluationStatus, hasScore: boolean): boolean {
  return !hasScore && (status === 'pending' || status === 'held')
}

/** One line on why a result is not scored yet. */
export const WAITING_REASONS: Record<WaitingReason, string> = {
  queued: 'In the queue. Jobs closest to your CV are scored first.',
  daily_cap: "Today's scoring limit is reached. It will be scored within 24 hours.",
  low_similarity: "Skipped: this job looks far from your CV, so it isn't scored automatically.",
}

export function waitingText(reason: WaitingReason | null | undefined): string {
  return WAITING_REASONS[reason ?? 'queued']
}

/**
 * What a row says about its evaluation, or null when there is nothing to
 * say (scored and settled). A result being re-scored keeps its old score.
 */
export function evaluationLabel(status: EvaluationStatus, hasScore: boolean): string | null {
  if (isWaiting(status, hasScore)) return 'Not scored yet'
  if (isEvaluating(status)) return hasScore ? 'Re-scoring…' : 'Scoring…'
  if (status === 'failed') return "Couldn't score"
  return null
}

/**
 * The message for a failed "Score it" request: the API's own words for a
 * 429 (it says how long to wait), otherwise a sentence around them.
 */
export function scoreItError(err: unknown): string {
  const message = err instanceof Error ? err.message : String(err)
  if (err instanceof ApiError && err.status === 429) return message
  return `We couldn't queue this job for scoring. ${message}`
}

const dateTimeFormat = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric',
  month: 'short',
  hour: '2-digit',
  minute: '2-digit',
})

/** "16 Oct, 07:04" */
export function formatDateTime(value: string): string {
  return dateTimeFormat.format(new Date(value))
}

/** Joins the non-empty parts with " · ". */
export function dotted(...parts: (string | null | undefined)[]): string {
  return parts.filter((p) => p && p.trim()).join(' · ')
}

/**
 * Returns url when it is an absolute http(s) URL, otherwise null. Posting
 * links are scraped from job portals, so a javascript: or data: URL must
 * never become a link.
 */
export function safePostingUrl(url: string | null | undefined): string | null {
  if (!url) return null
  try {
    const parsed = new URL(url)
    return parsed.protocol === 'https:' || parsed.protocol === 'http:' ? parsed.href : null
  } catch {
    return null
  }
}
