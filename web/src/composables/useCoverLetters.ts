// Cover letter drafts for the quick matches on the page, keyed by match id.
// A draft lives only in memory: nothing is saved, and it is gone when the
// user leaves the page. While a letter is being written a clock ticks once a
// second so the panel can show how long it has taken.
import { onBeforeUnmount, reactive, ref } from 'vue'
import {
  coverLetterErrorKind,
  writeCoverLetter,
  type CoverLetterErrorKind,
} from '../api/coverLetter'
import type { MatchResult } from '../api/match'

export type LetterStatus = 'writing' | 'ready' | 'error'

export interface LetterError {
  kind: CoverLetterErrorKind
  /** The API's message, shown only for unexpected errors. */
  message: string
}

export interface LetterDraft {
  /** 'ready' also covers a failed regeneration: the previous letter stays. */
  status: LetterStatus
  subject: string
  letter: string
  /** What the API last returned, to tell whether the user edited it. */
  generated: { subject: string; letter: string } | null
  error: LetterError | null
  startedAt: number
}

export function useCoverLetters() {
  const drafts = reactive(new Map<number, LetterDraft>())
  const now = ref(Date.now())
  const controllers = new Map<number, AbortController>()
  let clock: ReturnType<typeof setInterval> | undefined

  function tick() {
    if (controllers.size > 0 && clock === undefined) {
      now.value = Date.now()
      clock = setInterval(() => (now.value = Date.now()), 1000)
    } else if (controllers.size === 0 && clock !== undefined) {
      clearInterval(clock)
      clock = undefined
    }
  }

  /** Writes (or rewrites) the letter for match id. */
  async function write(id: number, jobDescription: string, evaluation: MatchResult) {
    controllers.get(id)?.abort()
    const controller = new AbortController()
    controllers.set(id, controller)

    let draft = drafts.get(id)
    if (!draft) {
      drafts.set(id, {
        status: 'writing',
        subject: '',
        letter: '',
        generated: null,
        error: null,
        startedAt: Date.now(),
      })
      draft = drafts.get(id)!
    }
    draft.status = 'writing'
    draft.error = null
    draft.startedAt = Date.now()
    tick()

    try {
      const res = await writeCoverLetter(
        { job_description: jobDescription, evaluation },
        { signal: controller.signal },
      )
      draft.subject = res.subject
      draft.letter = res.letter
      draft.generated = { subject: res.subject, letter: res.letter }
      draft.status = 'ready'
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      draft.error = {
        kind: coverLetterErrorKind(err),
        message: err instanceof Error ? err.message : '',
      }
      draft.status = draft.generated ? 'ready' : 'error'
    } finally {
      if (controllers.get(id) === controller) controllers.delete(id)
      tick()
    }
  }

  function update(id: number, field: 'subject' | 'letter', value: string) {
    const draft = drafts.get(id)
    if (draft) draft[field] = value
  }

  /** Whether the user changed the subject or letter the API returned. */
  function isEdited(id: number): boolean {
    const draft = drafts.get(id)
    if (!draft?.generated) return false
    return draft.subject !== draft.generated.subject || draft.letter !== draft.generated.letter
  }

  onBeforeUnmount(() => {
    controllers.forEach((c) => c.abort())
    controllers.clear()
    clearInterval(clock)
  })

  return { drafts, now, write, update, isEdited }
}
