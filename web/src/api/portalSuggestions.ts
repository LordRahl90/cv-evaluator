// Portal suggestions: GET /portal-suggestions (the caller's own, newest
// first) and POST /portal-suggestions {name?, url, note?}.
import { request, type RequestOptions } from './client'
import type { ListResponse } from './types'

export type PortalSuggestionStatus = 'submitted' | 'planned' | 'added' | 'declined'

export interface PortalSuggestion {
  object: 'portal_suggestion'
  id: string
  name: string
  /** Normalized by the API: https, lower-case host without www., no trailing slash. */
  url: string
  note: string
  status: PortalSuggestionStatus | string
  created_at: string
  updated_at: string
}

export interface CreatePortalSuggestionRequest {
  /** Defaults to the site's host name. */
  name?: string
  /** A scheme is optional: "stepstone.de" works. */
  url: string
  note?: string
}

/** Field limits enforced by the API. */
export const PORTAL_SUGGESTION_LIMITS = { name: 100, url: 2048, note: 1000 } as const

/** 409 codes for POST /portal-suggestions (both with param "url"). */
export const CODE_PORTAL_ALREADY_SUGGESTED = 'portal_already_suggested'
export const CODE_PORTAL_ALREADY_SUPPORTED = 'portal_already_supported'

export async function listPortalSuggestions(options?: RequestOptions): Promise<PortalSuggestion[]> {
  const data = await request<ListResponse<PortalSuggestion> | null>('/portal-suggestions', options)
  return Array.isArray(data?.data) ? data.data : []
}

export function createPortalSuggestion(body: CreatePortalSuggestionRequest) {
  return request<PortalSuggestion>('/portal-suggestions', { method: 'POST', body })
}
