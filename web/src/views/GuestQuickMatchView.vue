<script setup lang="ts">
// Quick match without an account (/try): pick a PDF CV, paste a job
// description and get the same scored result signed-in users get. The CV
// goes up with the request and is not stored. The API limits guest matches
// per visitor and across all guests, so its 429 messages are shown as they
// are, next to an invitation to sign up.
import { onBeforeUnmount, ref } from 'vue'
import AlertMessage from '../components/AlertMessage.vue'
import AppIcon from '../components/AppIcon'
import BrandMark from '../components/BrandMark.vue'
import BusyButton from '../components/BusyButton.vue'
import CVFilePicker from '../components/CVFilePicker.vue'
import MatchResultCard from '../components/MatchResultCard.vue'
import MatchScoring from '../components/MatchScoring.vue'
import { ApiError } from '../api/client'
import { matchAsGuest, type MatchResult } from '../api/match'
import { cvFileProblem, formatBytes } from '../utils/cvFile'
import { pastedMeta, titleFrom, wordCount } from '../utils/jobText'

interface GuestMatch {
  title: string
  words: number
  result: MatchResult
}

const file = ref<File | null>(null)
const fileError = ref('')
const description = ref('')
const descriptionError = ref('')
const submitError = ref('')
const limited = ref(false)
const scoring = ref(false)
const elapsed = ref(0)
const current = ref<GuestMatch | null>(null)

let timer: ReturnType<typeof setInterval> | undefined
let controller: AbortController | null = null

onBeforeUnmount(() => {
  clearInterval(timer)
  controller?.abort()
})

function selectFile(files: FileList | null) {
  const list = Array.from(files ?? [])
  const [picked] = list
  if (!picked) return
  if (list.length > 1) {
    fileError.value = 'Choose one PDF at a time.'
    return
  }
  const problem = cvFileProblem(picked)
  fileError.value = problem
  file.value = problem ? null : picked
}

/** Puts an API error next to the field it is about, or above the button. */
function showError(err: unknown) {
  if (!(err instanceof ApiError)) {
    submitError.value = "We couldn't score this job. Something went wrong; try again in a moment."
    return
  }
  if (err.status === 429) {
    limited.value = true
    submitError.value = err.message
  } else if (err.status === 413 || err.status === 422 || err.param === 'cv') {
    fileError.value = err.message
    file.value = null
  } else if (err.param === 'job_description') {
    descriptionError.value = err.message
  } else {
    submitError.value = `We couldn't score this job. ${err.message}`
  }
}

async function submit() {
  if (scoring.value) return
  const text = description.value.trim()
  submitError.value = ''
  limited.value = false
  if (!file.value && !fileError.value) fileError.value = 'Choose your CV as a PDF first.'
  if (!text) descriptionError.value = 'Paste a job description first.'
  if (!file.value || !text) return

  scoring.value = true
  elapsed.value = 0
  const started = Date.now()
  timer = setInterval(() => (elapsed.value = Math.floor((Date.now() - started) / 1000)), 1000)
  controller = new AbortController()
  try {
    const result = await matchAsGuest(file.value, text, { signal: controller.signal })
    current.value = { title: titleFrom(text), words: wordCount(text), result }
    description.value = ''
    window.scrollTo({ top: 0 })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') return
    showError(err)
  } finally {
    clearInterval(timer)
    scoring.value = false
    controller = null
  }
}

/** Back to the form, keeping the CV so another job can be scored. */
function scoreAnother() {
  current.value = null
  submitError.value = ''
  limited.value = false
}
</script>

<template>
  <div class="guest">
    <header class="guest-bar">
      <RouterLink to="/signin" class="guest-brand" aria-label="CV Evaluator home">
        <BrandMark />
      </RouterLink>
      <nav class="guest-nav" aria-label="Account">
        <RouterLink to="/signin" class="btn btn-secondary">Sign in</RouterLink>
        <RouterLink to="/signup" class="btn btn-primary guest-nav-signup"
          >Create account</RouterLink
        >
      </nav>
    </header>

    <main class="main">
      <div class="page-stack qm-page">
        <div class="page-head">
          <div class="page-head-text">
            <h1 class="page-title">Quick match</h1>
            <p class="page-sub">
              Score one job against your CV, no account needed. Your CV is read to score this job
              and is not stored.
            </p>
          </div>
          <div v-if="current" class="page-actions">
            <button type="button" class="btn btn-secondary" @click="scoreAnother">
              <AppIcon name="plus" />Score another job
            </button>
          </div>
        </div>

        <section
          v-if="current"
          class="guest-cta"
          aria-labelledby="guest-cta-title"
          data-testid="guest-cta"
        >
          <span class="guest-cta-icon"><AppIcon name="star" /></span>
          <div class="guest-cta-text">
            <h2 id="guest-cta-title" class="guest-cta-title">
              Create an account to save your CV and get matches automatically
            </h2>
            <p class="guest-cta-sub">
              Upload your CV once, set up a job search, and every new job is scored for you like
              this one.
            </p>
          </div>
          <RouterLink to="/signup" class="btn btn-primary guest-cta-action">
            Create an account
          </RouterLink>
        </section>

        <div class="qm-layout">
          <div class="qm-main">
            <MatchResultCard
              v-if="current"
              :result="current.result"
              :title="current.title"
              :meta="pastedMeta(current.words)"
            />

            <template v-else>
              <form
                class="card qm-form"
                novalidate
                aria-label="Quick match"
                @submit.prevent="submit"
              >
                <div class="guest-field">
                  <span id="guest-cv-label" class="field-label qm-label">Your CV</span>
                  <div
                    class="guest-file"
                    :class="{ 'has-error': fileError }"
                    role="group"
                    aria-labelledby="guest-cv-label"
                    :aria-describedby="fileError ? 'guest-cv-error' : 'guest-cv-hint'"
                  >
                    <span class="guest-file-icon"><AppIcon name="file" /></span>
                    <span v-if="file" class="guest-file-body" data-testid="guest-file">
                      <span class="guest-file-name">{{ file.name }}</span>
                      <span class="guest-file-size">{{ formatBytes(file.size) }}</span>
                    </span>
                    <span v-else class="guest-file-body">
                      <span class="guest-file-empty">No PDF chosen yet</span>
                    </span>
                    <CVFilePicker
                      :label="file ? 'Change' : 'Choose PDF'"
                      :variant="file ? 'secondary' : 'primary'"
                      :disabled="scoring"
                      @select="selectFile"
                    />
                  </div>
                  <p v-if="fileError" id="guest-cv-error" class="field-error">{{ fileError }}</p>
                  <p v-else id="guest-cv-hint" class="field-hint guest-file-hint">
                    PDF only · up to 5 MB · text must be selectable, not a scan
                  </p>
                </div>

                <div class="guest-field">
                  <label for="guest-description" class="field-label qm-label">
                    Job description
                  </label>
                  <textarea
                    id="guest-description"
                    v-model="description"
                    class="textarea"
                    :class="{ 'has-error': descriptionError }"
                    name="job_description"
                    placeholder="Paste the full job description here: title, responsibilities, requirements, location…"
                    :readonly="scoring"
                    :aria-invalid="descriptionError ? 'true' : undefined"
                    :aria-describedby="descriptionError ? 'guest-description-error' : undefined"
                    @input="descriptionError = ''"
                  />
                  <p v-if="descriptionError" id="guest-description-error" class="field-error">
                    {{ descriptionError }}
                  </p>
                </div>

                <AlertMessage v-if="submitError" data-testid="guest-error">
                  {{ submitError }}
                  <template v-if="limited">
                    <RouterLink to="/signup" class="link-strong">Create an account</RouterLink>
                  </template>
                </AlertMessage>

                <div class="qm-form-foot">
                  <span class="qm-form-note"> Your CV is not stored · takes 5 to 30 seconds </span>
                  <BusyButton :busy="scoring" busy-label="Scoring…">
                    <AppIcon name="bolt" />Score this job
                  </BusyButton>
                </div>
              </form>

              <MatchScoring v-if="scoring" against="your CV" :elapsed="elapsed" />
            </template>
          </div>

          <aside class="card guest-pitch" aria-labelledby="guest-pitch-title">
            <h2 id="guest-pitch-title" class="qm-recent-title">With an account</h2>
            <ul class="guest-pitch-list">
              <li>
                <AppIcon name="file" /><span>Your CV is kept, so you never upload it again.</span>
              </li>
              <li>
                <AppIcon name="search" /><span>Job searches find new jobs and score each one.</span>
              </li>
              <li>
                <AppIcon name="pen" /><span
                  >Cover letters written from your CV for strong matches.</span
                >
              </li>
            </ul>
            <RouterLink to="/signup" class="btn btn-secondary">Create an account</RouterLink>
            <p class="guest-pitch-note">
              Already have one? <RouterLink to="/signin" class="link-strong">Sign in</RouterLink>
            </p>
          </aside>
        </div>
      </div>
    </main>
  </div>
</template>
