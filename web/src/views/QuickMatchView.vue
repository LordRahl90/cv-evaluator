<script setup lang="ts">
// Quick match: paste a job description, get it scored against the newest
// CV. The call runs the LLM inside the request (5-30 s), so the form locks
// and a placeholder result shows while it runs. Recent results are kept in
// this component only: nothing is saved, and they clear when you leave.
//
// From a result the user can have a cover letter written (a banner offers
// it for strong matches, a secondary button otherwise). The letter replaces
// the result on the page, with a compact summary of the match below it;
// letters are kept per match, in memory only, like the results.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import AlertMessage from '../components/AlertMessage.vue'
import AppIcon from '../components/AppIcon'
import BusyButton from '../components/BusyButton.vue'
import CoverLetterBanner from '../components/CoverLetterBanner.vue'
import CoverLetterPanel from '../components/CoverLetterPanel.vue'
import MatchResultCard from '../components/MatchResultCard.vue'
import MatchSummaryCard from '../components/MatchSummaryCard.vue'
import ScoreRing from '../components/ScoreRing.vue'
import { listCVs } from '../api/cvs'
import { isNoCVError, matchJobDescription, type MatchResult } from '../api/match'
import type { CVSummary } from '../api/types'
import { useCoverLetters } from '../composables/useCoverLetters'
import { formatDate, formatAgo } from '../utils/format'
import { STRONG_SCORE } from '../utils/matches'

interface QuickMatch {
  id: number
  title: string
  /** The pasted description, sent again to write a cover letter. */
  text: string
  words: number
  result: MatchResult
  at: number
  cv: CVSummary | null
}

const RECENT_LIMIT = 5

const cvStatus = ref<'loading' | 'ready' | 'error'>('loading')
const latestCV = ref<CVSummary | null>(null)
const noCV = ref(false)

const description = ref('')
const fieldError = ref('')
const submitError = ref('')
const scoring = ref(false)
const elapsed = ref(0)
const current = ref<QuickMatch | null>(null)
const recent = ref<QuickMatch[]>([])

let timer: ReturnType<typeof setInterval> | undefined
let controller: AbortController | null = null
let nextId = 1

onMounted(async () => {
  try {
    const cvs = await listCVs()
    cvs.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
    latestCV.value = cvs[0] ?? null
    noCV.value = cvs.length === 0
    cvStatus.value = 'ready'
  } catch {
    // Not fatal: the match call itself says (422) when there is no CV.
    cvStatus.value = 'error'
  }
})

onBeforeUnmount(() => {
  clearInterval(timer)
  controller?.abort()
})

const cvLabel = computed(() =>
  latestCV.value ? `your CV from ${formatDate(latestCV.value.created_at)}` : 'your newest CV',
)

function titleFrom(text: string): string {
  const line = text
    .split('\n')
    .map((l) => l.trim())
    .find(Boolean)
  if (!line) return 'Pasted job description'
  if (line.length <= 80) return line
  const cut = line.slice(0, 80)
  return `${cut.slice(0, cut.lastIndexOf(' ') > 40 ? cut.lastIndexOf(' ') : 80)}…`
}

const wordCount = (text: string) => text.trim().split(/\s+/).filter(Boolean).length

async function submit() {
  if (scoring.value) return
  const text = description.value.trim()
  submitError.value = ''
  if (!text) {
    fieldError.value = 'Paste a job description first.'
    return
  }
  fieldError.value = ''

  scoring.value = true
  elapsed.value = 0
  const started = Date.now()
  timer = setInterval(() => (elapsed.value = Math.floor((Date.now() - started) / 1000)), 1000)
  controller = new AbortController()
  try {
    const result = await matchJobDescription(text, { signal: controller.signal })
    const match: QuickMatch = {
      id: nextId++,
      title: titleFrom(text),
      text,
      words: wordCount(text),
      result,
      at: Date.now(),
      cv: latestCV.value,
    }
    current.value = match
    letterOpen.value = false
    recent.value = [match, ...recent.value].slice(0, RECENT_LIMIT)
    description.value = ''
    window.scrollTo({ top: 0 })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') return
    if (isNoCVError(err)) {
      noCV.value = true
      return
    }
    submitError.value = `We couldn't score this job. ${err instanceof Error ? err.message : ''}`
  } finally {
    clearInterval(timer)
    scoring.value = false
    controller = null
  }
}

function scoreAnother() {
  current.value = null
  letterOpen.value = false
  submitError.value = ''
}

function show(match: QuickMatch) {
  current.value = match
  letterOpen.value = false
}

// Cover letters
const letters = useCoverLetters()
const letterOpen = ref(false)
const draft = computed(() =>
  current.value ? (letters.drafts.get(current.value.id) ?? null) : null,
)
const strong = computed(() => (current.value?.result.overall_score ?? 0) >= STRONG_SCORE)

/** Shows the current match's letter, writing it first if there is none. */
function openLetter() {
  const match = current.value
  if (!match) return
  letterOpen.value = true
  window.scrollTo({ top: 0 })
  if (!draft.value || draft.value.status === 'error') regenerate()
}

function regenerate() {
  const match = current.value
  if (match) void letters.write(match.id, match.text, match.result)
}

function updateLetter(field: 'subject' | 'letter', value: string) {
  if (current.value) letters.update(current.value.id, field, value)
}

const subtitle = computed(() => {
  const match = current.value
  if (!match) {
    return 'Found a job somewhere else? Paste its description for an instant score. Nothing is saved.'
  }
  const against = match.cv ? `your CV from ${formatDate(match.cv.created_at)}` : 'your newest CV'
  return `Scored against ${against} ${formatAgo(match.at)}.`
})
</script>

<template>
  <div class="page">
    <div class="page-stack qm-page">
      <div class="page-head">
        <div class="page-head-text">
          <h1 class="page-title">Quick match</h1>
          <p class="page-sub">{{ noCV ? 'Score any job against your CV.' : subtitle }}</p>
        </div>
        <div v-if="current && !noCV && !letterOpen" class="page-actions">
          <button
            v-if="!strong"
            type="button"
            class="btn btn-secondary"
            data-testid="cl-write"
            @click="openLetter"
          >
            <AppIcon name="pen" />{{ draft ? 'Open cover letter' : 'Write cover letter' }}
          </button>
          <button type="button" class="btn btn-secondary" @click="scoreAnother">
            <AppIcon name="plus" />Score another job
          </button>
        </div>
      </div>

      <!-- No CV yet -->
      <section v-if="noCV" class="card empty-state empty-state-page" data-testid="qm-no-cv">
        <span class="empty-state-icon"><AppIcon name="file" /></span>
        <h2 class="empty-state-title">Upload a CV first</h2>
        <p class="empty-state-text">
          Quick match scores a job against your CV. Upload one as a PDF, then come back and paste
          the job description.
        </p>
        <RouterLink to="/cvs" class="btn btn-primary">
          <AppIcon name="upload" />Upload CV
        </RouterLink>
      </section>

      <!-- Cover letter for the current match -->
      <template v-else-if="letterOpen && current && draft">
        <CoverLetterPanel
          :draft="draft"
          :title="current.title"
          :now="letters.now.value"
          :edited="letters.isEdited(current.id)"
          @regenerate="regenerate"
          @update:subject="updateLetter('subject', $event)"
          @update:letter="updateLetter('letter', $event)"
        />
        <MatchSummaryCard
          :result="current.result"
          :title="current.title"
          @show-result="letterOpen = false"
        />
      </template>

      <CoverLetterBanner
        v-if="!noCV && !letterOpen && current && strong"
        :has-letter="!!draft"
        @write="openLetter"
      />

      <div v-if="!noCV && !(letterOpen && current && draft)" class="qm-layout">
        <div class="qm-main">
          <MatchResultCard
            v-if="current"
            :result="current.result"
            :title="current.title"
            :meta="`Pasted description · ${current.words} ${current.words === 1 ? 'word' : 'words'}`"
          />

          <template v-else>
            <form class="card qm-form" novalidate @submit.prevent="submit">
              <label for="qm-description" class="field-label qm-label">Job description</label>
              <textarea
                id="qm-description"
                v-model="description"
                class="textarea"
                :class="{ 'has-error': fieldError }"
                name="job_description"
                placeholder="Paste the full job description here: title, responsibilities, requirements, location…"
                :readonly="scoring"
                :aria-invalid="fieldError ? 'true' : undefined"
                :aria-describedby="fieldError ? 'qm-description-error' : undefined"
                @input="fieldError = ''"
              />
              <p v-if="fieldError" id="qm-description-error" class="field-error">
                {{ fieldError }}
              </p>
              <AlertMessage v-if="submitError" data-testid="qm-error">{{
                submitError
              }}</AlertMessage>
              <div class="qm-form-foot">
                <span class="qm-form-note">
                  Scored against
                  <span
                    v-if="cvStatus === 'loading'"
                    class="skeleton skeleton-line qm-note-skeleton"
                  />
                  <RouterLink v-else to="/cvs" class="link-strong">{{ cvLabel }}</RouterLink>
                  · takes 5 to 30 seconds
                </span>
                <BusyButton :busy="scoring" busy-label="Scoring…">
                  <AppIcon name="bolt" />Score this job
                </BusyButton>
              </div>
            </form>

            <section
              v-if="scoring"
              class="card qm-scoring"
              role="status"
              aria-live="polite"
              data-testid="qm-scoring"
            >
              <div class="qm-scoring-head">
                <span class="skeleton qm-ring-skeleton" aria-hidden="true" />
                <div class="qm-scoring-text">
                  <div class="qm-scoring-title">Scoring this job against {{ cvLabel }}…</div>
                  <div class="qm-scoring-sub">
                    Reading the requirements and comparing them with your experience. This usually
                    takes 5 to 30 seconds<template v-if="elapsed >= 1">
                      ({{ elapsed }} s so far)</template
                    >.
                  </div>
                </div>
              </div>
              <div class="qm-scoring-body" aria-hidden="true">
                <span class="skeleton skeleton-line" />
                <span class="skeleton skeleton-line skeleton-w-56" />
                <span v-for="n in 4" :key="n" class="skeleton skeleton-bar" />
              </div>
            </section>
          </template>
        </div>

        <aside class="card qm-recent" aria-labelledby="qm-recent-title">
          <h2 id="qm-recent-title" class="qm-recent-title">Recent quick matches</h2>
          <div v-if="recent.length === 0" class="qm-recent-empty">
            <span class="qm-recent-empty-icon"><AppIcon name="clock" /></span>
            <div class="qm-recent-empty-title">Nothing yet</div>
            <div class="qm-recent-empty-text">
              Your last few quick matches show up here while you're on this page. They aren't saved.
            </div>
          </div>
          <ul v-else class="qm-recent-list">
            <li v-for="match in recent" :key="match.id">
              <button
                type="button"
                class="qm-recent-item"
                :class="{ 'is-current': current?.id === match.id }"
                :aria-current="current?.id === match.id ? 'true' : undefined"
                @click="show(match)"
              >
                <ScoreRing :score="match.result.overall_score" size="sm" />
                <span class="qm-recent-body">
                  <span class="qm-recent-name">{{ match.title }}</span>
                  <span class="qm-recent-time">{{ formatAgo(match.at) }}</span>
                </span>
              </button>
            </li>
          </ul>
        </aside>
      </div>
    </div>
  </div>
</template>
