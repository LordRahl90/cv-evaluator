// Client-side checks for the job search and portal suggestion forms. They
// mirror the API's rules (internal/services/jobsearches and
// internal/services/portalsuggestions) so most mistakes are caught before a
// round trip; the API stays the source of truth and its field errors are
// shown on the same fields.
import { FREQUENCIES, JOB_SEARCH_LIMITS } from '../api/jobSearches'
import { PORTAL_SUGGESTION_LIMITS } from '../api/portalSuggestions'

/** Length in characters (code points), as the API counts it. */
const length = (value: string) => [...value].length

export interface JobSearchInput {
  name: string
  query: string
  location: string
  frequency: string
}

export type JobSearchField = keyof JobSearchInput
export const JOB_SEARCH_FIELDS: readonly JobSearchField[] = [
  'name',
  'query',
  'location',
  'frequency',
]

export function validateJobSearch(input: JobSearchInput): Partial<Record<JobSearchField, string>> {
  const errors: Partial<Record<JobSearchField, string>> = {}
  const query = input.query.trim()
  if (!query) errors.query = 'Enter what to search for, like "Senior Go Engineer".'
  else if (length(query) > JOB_SEARCH_LIMITS.query)
    errors.query = `Keep the keywords to ${JOB_SEARCH_LIMITS.query} characters or fewer.`
  if (length(input.location.trim()) > JOB_SEARCH_LIMITS.location)
    errors.location = `Keep the location to ${JOB_SEARCH_LIMITS.location} characters or fewer.`
  if (length(input.name.trim()) > JOB_SEARCH_LIMITS.name)
    errors.name = `Keep the name to ${JOB_SEARCH_LIMITS.name} characters or fewer.`
  if (!(FREQUENCIES as readonly string[]).includes(input.frequency))
    errors.frequency = 'Choose how often to run this search.'
  return errors
}

export interface PortalSuggestionInput {
  name: string
  url: string
}

export type PortalSuggestionField = keyof PortalSuggestionInput | 'note'
export const PORTAL_SUGGESTION_FIELDS: readonly PortalSuggestionField[] = ['name', 'url', 'note']

/** Same idea as the API's parseURL: http(s), a scheme is optional, a dotted host. */
export function isPortalUrl(raw: string): boolean {
  const value = raw.includes('://') ? raw : `https://${raw}`
  try {
    const url = new URL(value)
    return (
      (url.protocol === 'http:' || url.protocol === 'https:') &&
      url.hostname.includes('.') &&
      !url.username &&
      !url.password
    )
  } catch {
    return false
  }
}

export function validatePortalSuggestion(
  input: PortalSuggestionInput,
): Partial<Record<PortalSuggestionField, string>> {
  const errors: Partial<Record<PortalSuggestionField, string>> = {}
  const url = input.url.trim()
  if (!url) errors.url = "Enter the portal's website, like stepstone.de."
  else if (url.length > PORTAL_SUGGESTION_LIMITS.url)
    errors.url = `Keep the address to ${PORTAL_SUGGESTION_LIMITS.url} characters or fewer.`
  else if (!isPortalUrl(url)) errors.url = 'Enter a web address, like https://www.stepstone.de.'
  if (length(input.name.trim()) > PORTAL_SUGGESTION_LIMITS.name)
    errors.name = `Keep the name to ${PORTAL_SUGGESTION_LIMITS.name} characters or fewer.`
  return errors
}
