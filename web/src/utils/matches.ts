// Display helpers for matches (search results): score tiers, status and
// evaluation labels, portal and criterion names.
import type { EvaluationStatus, ResultStatus } from '../api/results'

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
 * What a row says about its evaluation, or null when there is nothing to
 * say (scored and settled). A result being re-scored keeps its old score.
 */
export function evaluationLabel(status: EvaluationStatus, hasScore: boolean): string | null {
  if (isEvaluating(status)) return hasScore ? 'Re-scoring…' : 'Scoring…'
  if (status === 'failed') return "Couldn't score"
  return null
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
