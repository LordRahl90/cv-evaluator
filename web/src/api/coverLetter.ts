// Cover letters. POST /match/cover-letter writes a letter for a pasted job
// from the user's newest CV, signed with the name on their profile. It runs
// the LLM inside the request (usually 10-20 seconds, longer when the server
// has to rewrite a letter that failed its checks). Nothing is stored.
import { ApiError, request, type RequestOptions } from './client'
import type { MatchResult } from './match'

/** POST /match/cover-letter request body. */
export interface CoverLetterRequest {
  job_description: string
  /** The quick-match result the client already has; used as data only. */
  evaluation?: MatchResult
}

/** POST /match/cover-letter response. */
export interface CoverLetter {
  subject: string
  /** Plain text with \n line breaks, a blank line between paragraphs. */
  letter: string
  word_count: number
}

/** Error codes this endpoint adds to the shared ones. */
export const COVER_LETTER_ERRORS = {
  /** 422: the user has not uploaded a CV. */
  noCV: 'cv_missing',
  /** 429: too many letters in a short time. */
  rateLimited: 'rate_limited',
  /** 502: no letter passed the server's quality checks. */
  failed: 'cover_letter_failed',
} as const

export function writeCoverLetter(body: CoverLetterRequest, options?: RequestOptions) {
  return request<CoverLetter>('/match/cover-letter', { ...options, method: 'POST', body })
}

export type CoverLetterErrorKind = 'no-cv' | 'rate-limited' | 'failed' | 'other'

/** Sorts a failed call into the cases the UI explains differently. */
export function coverLetterErrorKind(err: unknown): CoverLetterErrorKind {
  if (!(err instanceof ApiError)) return 'other'
  if (err.status === 422 || err.code === COVER_LETTER_ERRORS.noCV) return 'no-cv'
  if (err.status === 429 || err.code === COVER_LETTER_ERRORS.rateLimited) return 'rate-limited'
  if (err.code === COVER_LETTER_ERRORS.failed) return 'failed'
  return 'other'
}

/** The text a user copies: the subject line, a blank line, then the letter. */
export function letterForClipboard(subject: string, letter: string): string {
  const s = subject.trim()
  return s ? `Subject: ${s}\n\n${letter.trim()}` : letter.trim()
}
