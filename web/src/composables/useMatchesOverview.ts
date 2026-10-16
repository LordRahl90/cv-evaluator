// The overview's matches area: result counts (GET /results/summary) and the
// best matches found in the last seven days. Loaded apart from the rest of
// the overview so a failure here only affects this area.
import { ref } from 'vue'
import {
  getResultsSummary,
  listResults,
  type ResultsSummary,
  type SearchResult,
} from '../api/results'

const WEEK_MS = 7 * 24 * 3_600_000
export const TOP_MATCHES = 5

export function useMatchesOverview() {
  const status = ref<'loading' | 'ready' | 'error'>('loading')
  const error = ref<Error | null>(null)
  const summary = ref<ResultsSummary | null>(null)
  const top = ref<SearchResult[]>([])

  async function load(): Promise<void> {
    status.value = 'loading'
    error.value = null
    try {
      const [s, page] = await Promise.all([
        getResultsSummary(),
        listResults({
          since: new Date(Date.now() - WEEK_MS).toISOString(),
          status: ['new', 'saved', 'applied'],
          sort: 'score',
          limit: TOP_MATCHES,
        }),
      ])
      summary.value = s
      // Only scored ones are "matches"; unscored jobs wait on the Matches page.
      top.value = page.data.filter((r) => r.evaluation !== null)
      status.value = 'ready'
    } catch (err) {
      error.value = err instanceof Error ? err : new Error(String(err))
      status.value = 'error'
    }
  }

  return { status, error, summary, top, load }
}
