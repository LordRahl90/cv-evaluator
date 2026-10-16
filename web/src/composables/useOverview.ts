// Loads everything the overview needs (profile, CVs, job searches) in
// parallel and derives the setup checklist and stats from real data.
import { computed, ref } from 'vue'
import { loadUser, session } from '../stores/session'
import { listCVs } from '../api/cvs'
import { listJobSearches } from '../api/jobSearches'
import type { CVSummary, JobSearch } from '../api/types'

export type LoadStatus = 'loading' | 'ready' | 'error'

const time = (value: string) => new Date(value).getTime()

export function useOverview() {
  const status = ref<LoadStatus>('loading')
  const error = ref<Error | null>(null)
  const cvs = ref<CVSummary[]>([])
  const searches = ref<JobSearch[]>([])

  async function load(): Promise<void> {
    status.value = 'loading'
    error.value = null
    try {
      const [, cvList, searchList] = await Promise.all([loadUser(), listCVs(), listJobSearches()])
      cvs.value = cvList
      searches.value = searchList
      status.value = 'ready'
    } catch (err) {
      // A 401 is handled globally (sign out and redirect); anything else
      // stays on the page with a retry.
      error.value = err instanceof Error ? err : new Error(String(err))
      status.value = 'error'
    }
  }

  const user = computed(() => session.user)
  const hasCV = computed(() => cvs.value.length > 0)
  const hasSearch = computed(() => searches.value.length > 0)
  const stepsDone = computed(() => 1 + Number(hasCV.value) + Number(hasSearch.value))
  const setupComplete = computed(() => hasCV.value && hasSearch.value)

  const latestCV = computed<CVSummary | null>(
    () => [...cvs.value].sort((a, b) => time(b.updated_at) - time(a.updated_at))[0] ?? null,
  )
  const activeSearches = computed(() => searches.value.filter((s) => s.enabled))
  const pausedCount = computed(() => searches.value.length - activeSearches.value.length)

  const nextRunAt = computed<Date | null>(() => {
    const times = activeSearches.value.flatMap((s) => (s.next_run_at ? [time(s.next_run_at)] : []))
    return times.length ? new Date(Math.min(...times)) : null
  })

  const lastRun = computed<(JobSearch & { last_run_at: string }) | null>(() => {
    const ran = searches.value.filter(
      (s): s is JobSearch & { last_run_at: string } => s.last_run_at !== null,
    )
    ran.sort((a, b) => time(b.last_run_at) - time(a.last_run_at))
    return ran[0] ?? null
  })

  return {
    status,
    error,
    load,
    user,
    cvs,
    searches,
    hasCV,
    hasSearch,
    stepsDone,
    setupComplete,
    latestCV,
    activeSearches,
    pausedCount,
    nextRunAt,
    lastRun,
  }
}
