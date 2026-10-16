<script setup lang="ts">
// The cover letter for one quick match: progress while it is written, then
// an editable subject and letter with Regenerate and Copy. The API is a
// single call, so the three progress steps advance on a timer, not on
// server events. Nothing here is saved.
import { computed, nextTick, ref, useId, watch } from 'vue'
import AlertMessage from './AlertMessage.vue'
import AppIcon from './AppIcon'
import BusyButton from './BusyButton.vue'
import ConfirmDialog from './ConfirmDialog.vue'
import { letterForClipboard } from '../api/coverLetter'
import type { LetterDraft, LetterError } from '../composables/useCoverLetters'
import { copyShortcut, copyText } from '../utils/clipboard'

const props = defineProps<{
  draft: LetterDraft
  title: string
  /** Current time in ms, ticking while the letter is written. */
  now: number
  edited: boolean
}>()

const emit = defineEmits<{
  regenerate: []
  'update:subject': [value: string]
  'update:letter': [value: string]
}>()

const ids = { title: useId(), subject: useId(), letter: useId() }

const elapsed = computed(() => Math.max(0, Math.floor((props.now - props.draft.startedAt) / 1000)))

// Time-based progress: the request is one call with no progress events.
const STEPS = [
  'Picking the requirements you meet',
  'Writing from your CV',
  'Checking tone and facts',
]
const activeStep = computed(() => (elapsed.value < 3 ? 0 : elapsed.value < 12 ? 1 : 2))
function stepState(i: number): 'done' | 'active' | 'todo' {
  if (i < activeStep.value) return 'done'
  return i === activeStep.value ? 'active' : 'todo'
}

const SKELETON = [
  '38',
  'gap',
  '22',
  '96',
  '91',
  '64',
  'gap',
  '94',
  '97',
  '88',
  '52',
  'gap',
  '90',
  '72',
]

const words = computed(() => props.draft.letter.trim().split(/\s+/).filter(Boolean).length)

function errorText(e: LetterError): string {
  switch (e.kind) {
    case 'no-cv':
      return 'You need a CV before we can write a letter.'
    case 'rate-limited':
      return "You've asked for several letters in the last minute. Wait a minute, then try again."
    case 'failed':
      return "We couldn't write a letter that passed our quality checks this time. Try again: each attempt is written from scratch."
    default:
      return `We couldn't write the cover letter. ${e.message}`.trim()
  }
}

// Regenerate replaces the letter, so ask first when the user has edited it.
const confirming = ref(false)
function regenerate() {
  if (props.edited) {
    confirming.value = true
    return
  }
  emit('regenerate')
}
function confirmRegenerate() {
  confirming.value = false
  emit('regenerate')
}

// Copy: the Clipboard API, or else select the text for Ctrl/Cmd+C.
const copied = ref(false)
const fallback = ref('')
const fallbackBox = ref<HTMLTextAreaElement | null>(null)
let copiedTimer: ReturnType<typeof setTimeout> | undefined

async function copy() {
  const text = letterForClipboard(props.draft.subject, props.draft.letter)
  clearTimeout(copiedTimer)
  if (await copyText(text)) {
    fallback.value = ''
    copied.value = true
    copiedTimer = setTimeout(() => (copied.value = false), 2000)
    return
  }
  copied.value = false
  fallback.value = text
  await nextTick()
  fallbackBox.value?.focus()
  fallbackBox.value?.select()
}

// An edit makes "Copied" and the fallback text stale.
watch(
  () => [props.draft.subject, props.draft.letter, props.draft.status],
  () => {
    copied.value = false
    fallback.value = ''
  },
)
</script>

<template>
  <section
    class="card cl-panel"
    :aria-labelledby="ids.title"
    :aria-busy="draft.status === 'writing' ? 'true' : undefined"
    data-testid="cl-panel"
  >
    <div class="cl-head">
      <div class="cl-head-text">
        <h2 :id="ids.title" class="cl-title">Cover letter</h2>
        <p class="cl-sub">{{ title }}</p>
      </div>
      <div class="cl-actions">
        <BusyButton
          v-if="draft.status === 'writing'"
          type="button"
          variant="secondary"
          busy
          :busy-label="`Writing… ${elapsed} s`"
        />
        <template v-else-if="draft.status === 'ready'">
          <button type="button" class="btn btn-secondary" @click="regenerate">
            <AppIcon name="rerun" />Regenerate
          </button>
          <button type="button" class="btn btn-primary" aria-live="polite" @click="copy">
            <template v-if="copied"><AppIcon name="check" />Copied</template>
            <template v-else><AppIcon name="copy" />Copy</template>
          </button>
        </template>
        <button
          v-else-if="draft.error?.kind !== 'no-cv'"
          type="button"
          class="btn btn-secondary"
          @click="emit('regenerate')"
        >
          <AppIcon name="rerun" />Try again
        </button>
      </div>
    </div>

    <!-- Writing -->
    <template v-if="draft.status === 'writing'">
      <ol class="cl-steps" role="status" aria-label="Progress">
        <li v-for="(step, i) in STEPS" :key="step" class="cl-step" :class="`is-${stepState(i)}`">
          <span class="cl-step-dot" aria-hidden="true">
            <AppIcon v-if="stepState(i) === 'done'" name="check" />
            <span v-else-if="stepState(i) === 'active'" class="cl-step-spinner" />
          </span>
          {{ step }}
          <span class="sr-only">{{
            { done: '(done)', active: '(in progress)', todo: '' }[stepState(i)]
          }}</span>
        </li>
      </ol>
      <div class="cl-skeleton" aria-hidden="true">
        <template v-for="(w, i) in SKELETON" :key="i">
          <span v-if="w === 'gap'" class="cl-skeleton-gap" />
          <span v-else class="skeleton skeleton-line" :class="`cl-w-${w}`" />
        </template>
      </div>
      <p class="cl-note">Usually 10–20 seconds. You can keep reading your match below.</p>
    </template>

    <!-- Failed before any letter was written -->
    <template v-else-if="draft.status === 'error' && draft.error">
      <AlertMessage data-testid="cl-error">
        {{ errorText(draft.error) }}
        <RouterLink v-if="draft.error.kind === 'no-cv'" to="/cvs" class="link-strong">
          Upload a CV
        </RouterLink>
      </AlertMessage>
    </template>

    <!-- Ready -->
    <template v-else>
      <AlertMessage v-if="draft.error" data-testid="cl-error">
        {{ errorText(draft.error) }} Your current letter is unchanged.
      </AlertMessage>

      <ul class="cl-chips" aria-label="Checks passed">
        <li class="cl-chip"><AppIcon name="check" />Every claim is from your CV</li>
        <li class="cl-chip"><AppIcon name="check" />Formal tone, no clichés</li>
        <li class="cl-chip is-count" data-testid="cl-words">
          {{ words }} {{ words === 1 ? 'word' : 'words' }}
        </li>
      </ul>

      <div class="field">
        <label :for="ids.subject" class="field-label">Subject</label>
        <input
          :id="ids.subject"
          class="input"
          type="text"
          :value="draft.subject"
          @input="emit('update:subject', ($event.target as HTMLInputElement).value)"
        />
      </div>

      <div class="field">
        <label :for="ids.letter" class="field-label">
          Letter <span class="field-optional">(edit freely before copying)</span>
        </label>
        <textarea
          :id="ids.letter"
          class="textarea cl-letter"
          :value="draft.letter"
          @input="emit('update:letter', ($event.target as HTMLTextAreaElement).value)"
        />
      </div>

      <div v-if="fallback" class="cl-copy-fallback" role="status" data-testid="cl-copy-fallback">
        <p class="cl-copy-fallback-text">
          Your browser didn't let us copy automatically. The letter is selected below: press
          {{ copyShortcut() }} to copy it.
        </p>
        <textarea
          ref="fallbackBox"
          class="textarea cl-copy-fallback-box"
          readonly
          aria-label="Letter to copy"
          :value="fallback"
        />
      </div>

      <div class="cl-warning">
        <AppIcon name="alert" />
        <span>
          This letter isn't saved. Copy it before you leave this page; Regenerate replaces your
          edits.
        </span>
      </div>
    </template>

    <ConfirmDialog
      :open="confirming"
      title="Replace your edits?"
      confirm-label="Regenerate"
      @confirm="confirmRegenerate"
      @cancel="confirming = false"
    >
      <p class="confirm-dialog-paragraph">
        Regenerate writes a new letter and replaces the changes you made to this one. Copy your
        edits first if you want to keep them.
      </p>
    </ConfirmDialog>
  </section>
</template>
