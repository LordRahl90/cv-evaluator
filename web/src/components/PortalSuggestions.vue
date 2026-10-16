<script setup lang="ts">
// "Job portals" section of the Job searches page (id="suggest", linked from
// the empty state and the search form): the portals every search runs on,
// the user's own suggestions with their review status, and a form to
// suggest another portal.
import { computed, onMounted, reactive, ref } from 'vue'
import AlertMessage from './AlertMessage.vue'
import AppIcon from './AppIcon'
import BusyButton from './BusyButton.vue'
import TextField from './TextField.vue'
import { ApiError } from '../api/client'
import type { Provider } from '../api/providers'
import {
  CODE_PORTAL_ALREADY_SUGGESTED,
  CODE_PORTAL_ALREADY_SUPPORTED,
  createPortalSuggestion,
  listPortalSuggestions,
  type PortalSuggestion,
} from '../api/portalSuggestions'
import { splitApiError } from '../utils/formErrors'
import { formatRelative } from '../utils/format'
import {
  PORTAL_SUGGESTION_FIELDS,
  validatePortalSuggestion,
  type PortalSuggestionField,
} from '../utils/searchValidation'

type LoadStatus = 'loading' | 'ready' | 'error'

const props = defineProps<{
  providers: readonly Provider[]
  providersStatus: LoadStatus
}>()
const emit = defineEmits<{ retryProviders: [] }>()

const status = ref<LoadStatus>('loading')
const loadError = ref('')
const suggestions = ref<PortalSuggestion[]>([])

async function load() {
  status.value = 'loading'
  loadError.value = ''
  try {
    suggestions.value = await listPortalSuggestions()
    status.value = 'ready'
  } catch (err) {
    loadError.value = err instanceof Error ? err.message : String(err)
    status.value = 'error'
  }
}

onMounted(load)

function retry() {
  if (props.providersStatus === 'error') emit('retryProviders')
  if (status.value === 'error') load()
}

const loading = computed(() => props.providersStatus === 'loading' || status.value === 'loading')
const failed = computed(() => props.providersStatus === 'error' || status.value === 'error')

interface Chip {
  key: string
  text: string
  tone: 'green' | 'amber' | 'grey'
}

const providerNames = computed(
  () => new Set(props.providers.map((p) => p.display_name.toLowerCase())),
)

function suggestionChip(s: PortalSuggestion): Chip {
  const base = { key: s.id }
  switch (s.status) {
    case 'planned':
      return { ...base, text: `${s.name} · planned, you suggested it`, tone: 'amber' }
    case 'added':
      return { ...base, text: `${s.name} · added, you suggested it`, tone: 'green' }
    case 'declined':
      return { ...base, text: `${s.name} · declined`, tone: 'grey' }
    default:
      return {
        ...base,
        text: `${s.name} · submitted ${formatRelative(s.created_at)}`,
        tone: 'grey',
      }
  }
}

const chips = computed<Chip[]>(() => [
  ...props.providers.map<Chip>((p) => ({
    key: `provider-${p.name}`,
    text: `${p.display_name} · live`,
    tone: 'green',
  })),
  // An accepted suggestion is already listed as a live portal.
  ...suggestions.value
    .filter((s) => !(s.status === 'added' && providerNames.value.has(s.name.toLowerCase())))
    .map(suggestionChip),
])

// Suggest form
const form = reactive({ name: '', url: '' })
const fieldErrors = reactive<Record<PortalSuggestionField, string>>({ name: '', url: '', note: '' })
const formError = ref('')
const notice = ref('')
const busy = ref(false)

function friendlyConflict(err: unknown): string | null {
  if (!(err instanceof ApiError)) return null
  if (err.code === CODE_PORTAL_ALREADY_SUGGESTED)
    return "You've already suggested this portal. It's in your list above."
  if (err.code === CODE_PORTAL_ALREADY_SUPPORTED)
    return 'We already search this portal, so every search includes it.'
  return null
}

async function submit() {
  if (busy.value) return
  formError.value = ''
  notice.value = ''
  PORTAL_SUGGESTION_FIELDS.forEach((f) => (fieldErrors[f] = ''))

  const errors = validatePortalSuggestion(form)
  if (Object.keys(errors).length) {
    Object.assign(fieldErrors, errors)
    document.getElementById(errors.url ? 'portal-url' : 'portal-name')?.focus()
    return
  }

  busy.value = true
  try {
    const name = form.name.trim()
    const created = await createPortalSuggestion({
      url: form.url.trim(),
      ...(name ? { name } : {}),
    })
    suggestions.value = [created, ...suggestions.value.filter((s) => s.id !== created.id)]
    if (status.value !== 'ready') status.value = 'ready'
    form.name = ''
    form.url = ''
    notice.value = `Thanks. We've added ${created.name} to your suggestions; its status shows here as we review it.`
  } catch (err) {
    const conflict = friendlyConflict(err)
    if (conflict) {
      fieldErrors.url = conflict
    } else {
      const { fields, form: message } = splitApiError(err, PORTAL_SUGGESTION_FIELDS)
      Object.assign(fieldErrors, fields)
      formError.value = message
    }
    document.getElementById(fieldErrors.name ? 'portal-name' : 'portal-url')?.focus()
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section id="suggest" class="card portals" aria-labelledby="portals-title" tabindex="-1">
    <div>
      <h2 id="portals-title" class="section-title">Job portals</h2>
      <p class="portals-sub">
        Every search runs on all of these. Missing one? Suggest it and we'll build support for it.
      </p>
    </div>

    <div v-if="loading" class="portals-chips" role="status" aria-live="polite">
      <span class="sr-only">Loading job portals…</span>
      <span v-for="n in 3" :key="n" class="skeleton portals-chip-skeleton" aria-hidden="true" />
    </div>
    <div v-else-if="failed" class="portals-error">
      <AlertMessage>We couldn't load the job portals. {{ loadError }}</AlertMessage>
      <button type="button" class="btn btn-secondary btn-sm" @click="retry">
        <AppIcon name="refresh" />Try again
      </button>
    </div>
    <ul v-else class="portals-chips" aria-label="Portals and your suggestions">
      <li
        v-for="chip in chips"
        :key="chip.key"
        class="badge portals-chip"
        :class="`badge-${chip.tone}`"
      >
        {{ chip.text }}
      </li>
    </ul>

    <form class="portals-form" novalidate aria-label="Suggest a portal" @submit.prevent="submit">
      <AlertMessage v-if="formError" class="portals-form-alert">{{ formError }}</AlertMessage>
      <AlertMessage v-if="notice" tone="info" class="portals-form-alert">{{ notice }}</AlertMessage>
      <TextField
        id="portal-name"
        v-model="form.name"
        class="portals-field-name"
        label="Portal name"
        optional
        autocomplete="off"
        placeholder="e.g. Indeed"
        :error="fieldErrors.name"
      />
      <TextField
        id="portal-url"
        v-model="form.url"
        class="portals-field-url"
        label="Website"
        type="url"
        autocomplete="url"
        placeholder="https://"
        required
        :error="fieldErrors.url"
      />
      <BusyButton variant="secondary" class="portals-submit" :busy="busy" busy-label="Sending…">
        Suggest portal
      </BusyButton>
    </form>
  </section>
</template>
