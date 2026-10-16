<script setup lang="ts">
// New job search (/searches/new) and edit (/searches/:id/edit). Checks the
// same rules as the API before sending, and shows the API's own field
// errors (error.param) under the matching field.
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AlertMessage from '../components/AlertMessage.vue'
import AppIcon from '../components/AppIcon'
import BusyButton from '../components/BusyButton.vue'
import PortalLogo from '../components/PortalLogo.vue'
import SegmentedControl from '../components/SegmentedControl.vue'
import TextField from '../components/TextField.vue'
import { ApiError } from '../api/client'
import { listCVs } from '../api/cvs'
import { createJobSearch, FREQUENCIES, getJobSearch, updateJobSearch } from '../api/jobSearches'
import { listProviders, type Provider } from '../api/providers'
import type { Frequency } from '../api/types'
import { capitalise } from '../utils/format'
import { splitApiError } from '../utils/formErrors'
import {
  JOB_SEARCH_FIELDS,
  validateJobSearch,
  type JobSearchField,
} from '../utils/searchValidation'

const route = useRoute()
const router = useRouter()

const searchId = computed(() => (typeof route.params.id === 'string' ? route.params.id : ''))
const isEdit = computed(() => Boolean(searchId.value))

// Edit mode loads the search first; create mode is ready straight away.
const status = ref<'loading' | 'ready' | 'error'>(isEdit.value ? 'loading' : 'ready')
const loadError = ref('')
const notFound = ref(false)

const form = reactive({ name: '', query: '', location: '', frequency: 'daily' as string })
const runNow = ref(true)
const fieldErrors = reactive<Record<JobSearchField, string>>({
  name: '',
  query: '',
  location: '',
  frequency: '',
})
const formError = ref('')
const busy = ref(false)

const providers = ref<Provider[]>([])
const providersFailed = ref(false)
const hasCV = ref<boolean | null>(null)

const frequencyOptions = FREQUENCIES.map((value) => ({ value, label: capitalise(value) }))

async function loadSearch() {
  status.value = 'loading'
  loadError.value = ''
  notFound.value = false
  try {
    const s = await getJobSearch(searchId.value)
    Object.assign(form, {
      name: s.name,
      query: s.query,
      location: s.location,
      frequency: s.frequency,
    })
    status.value = 'ready'
  } catch (err) {
    notFound.value = err instanceof ApiError && err.status === 404
    loadError.value = err instanceof Error ? err.message : String(err)
    status.value = 'error'
  }
}

onMounted(() => {
  if (isEdit.value) loadSearch()
  listProviders()
    .then((list) => (providers.value = list))
    .catch(() => (providersFailed.value = true))
  listCVs()
    .then((list) => (hasCV.value = list.length > 0))
    .catch(() => (hasCV.value = null))
})

const portalNames = computed(() => {
  const names = providers.value.map((p) => p.display_name)
  if (names.length <= 1) return names.join('')
  return `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`
})

const schedulePhrase: Record<Frequency, string> = {
  hourly: 'every hour',
  daily: 'daily',
  weekly: 'weekly',
}

const fetchStep = computed(() => {
  const freq = schedulePhrase[form.frequency as Frequency] ?? form.frequency
  const from = portalNames.value
    ? `From every portal we support, ${portalNames.value}`
    : 'From every portal we support'
  if (isEdit.value) return `${from}, ${freq}.`
  if (!runNow.value) return `${from}, ${freq}, once you turn the search on.`
  return `${from}, now and then ${freq}.`
})

const scoreStep = computed(() =>
  hasCV.value === false
    ? 'Against your active CV, once you upload one. Until then jobs are collected but not scored.'
    : 'Against your active CV. Results land in Matches.',
)

const runNowHint = computed(() =>
  runNow.value
    ? 'It starts in a moment, then keeps to the schedule.'
    : "It's saved paused. Turn it on from Job searches when you're ready.",
)

async function focusFirstError() {
  await nextTick()
  const first = JOB_SEARCH_FIELDS.find((f) => fieldErrors[f])
  if (!first) return
  const target =
    first === 'frequency'
      ? (document.querySelector<HTMLInputElement>('input[name="frequency"]:checked') ??
        document.querySelector<HTMLInputElement>('input[name="frequency"]'))
      : document.getElementById(first)
  target?.focus()
}

async function submit() {
  if (busy.value) return
  formError.value = ''
  JOB_SEARCH_FIELDS.forEach((f) => (fieldErrors[f] = ''))

  const errors = validateJobSearch(form)
  if (Object.keys(errors).length) {
    Object.assign(fieldErrors, errors)
    await focusFirstError()
    return
  }

  const body = {
    name: form.name.trim(),
    query: form.query.trim(),
    location: form.location.trim(),
    frequency: form.frequency as Frequency,
  }

  busy.value = true
  try {
    let flash: string
    if (isEdit.value) {
      const saved = await updateJobSearch(searchId.value, body)
      flash = `Saved changes to ${saved.name}.`
    } else {
      const { name, ...rest } = body
      const saved = await createJobSearch({
        ...rest,
        ...(name ? { name } : {}),
        enabled: runNow.value,
      })
      flash = saved.enabled
        ? `Created ${saved.name}. Its first run starts in a moment.`
        : `Created ${saved.name}. It's paused until you turn it on.`
    }
    await router.push({ name: 'searches', state: { flash } })
  } catch (err) {
    const { fields, form: message } = splitApiError(err, JOB_SEARCH_FIELDS)
    Object.assign(fieldErrors, fields)
    formError.value = message
    await focusFirstError()
  } finally {
    busy.value = false
  }
}

const title = computed(() => (isEdit.value ? 'Edit job search' : 'New job search'))
</script>

<template>
  <div class="page">
    <div class="page-stack search-form-page">
      <RouterLink to="/searches" class="back-link">
        <AppIcon name="chevronLeft" />Job searches
      </RouterLink>
      <div class="page-head">
        <div class="page-head-text">
          <h1 class="page-title">{{ title }}</h1>
          <p class="page-sub">
            We'll check this search on your schedule and score every new job against your active CV.
          </p>
        </div>
      </div>

      <!-- Edit: loading the search -->
      <div v-if="status === 'loading'" class="search-form-columns" role="status" aria-live="polite">
        <span class="sr-only">Loading the search…</span>
        <div
          class="card search-form skeleton-card"
          aria-hidden="true"
          data-testid="search-form-skeleton"
        >
          <span class="skeleton skeleton-line skeleton-w-24" />
          <span class="skeleton skeleton-row" />
          <span class="skeleton skeleton-line skeleton-w-32" />
          <span class="skeleton skeleton-row" />
          <span class="skeleton skeleton-row" />
        </div>
      </div>

      <section v-else-if="status === 'error'" class="card error-card">
        <AlertMessage>
          <template v-if="notFound"
            >We couldn't find this search. It may have been deleted.</template
          >
          <template v-else>We couldn't load this search. {{ loadError }}</template>
        </AlertMessage>
        <div class="page-actions">
          <RouterLink to="/searches" class="btn btn-secondary">
            <AppIcon name="arrowLeft" />Back to job searches
          </RouterLink>
          <button v-if="!notFound" type="button" class="btn btn-secondary" @click="loadSearch">
            <AppIcon name="refresh" />Try again
          </button>
        </div>
      </section>

      <div v-else class="search-form-columns">
        <form class="card search-form" novalidate :aria-label="title" @submit.prevent="submit">
          <AlertMessage v-if="formError">{{ formError }}</AlertMessage>

          <TextField
            id="name"
            v-model="form.name"
            label="Name"
            optional
            autocomplete="off"
            :placeholder="form.query.trim() || 'e.g. Senior Go · Copenhagen'"
            hint="Only you see this. Leave it empty to use the keywords."
            :error="fieldErrors.name"
          />

          <fieldset class="search-form-fieldset">
            <legend class="search-form-legend">What to search for</legend>
            <div class="search-form-grid">
              <TextField
                id="query"
                v-model="form.query"
                label="Keywords"
                autocomplete="off"
                placeholder="e.g. Senior Go Engineer"
                required
                :error="fieldErrors.query"
              />
              <TextField
                id="location"
                v-model="form.location"
                label="Location"
                optional
                autocomplete="off"
                placeholder="e.g. Copenhagen, Denmark"
                :error="fieldErrors.location"
              />
            </div>
            <div class="searched-on" data-testid="searched-on">
              <span class="searched-on-label">Searched on</span>
              <span v-for="p in providers" :key="p.name" class="searched-on-portal">
                <PortalLogo :name="p.name" :display-name="p.display_name" size="sm" />{{
                  p.display_name
                }}
              </span>
              <span v-if="providersFailed" class="searched-on-label">
                every portal we support
              </span>
              <RouterLink :to="{ path: '/searches', hash: '#suggest' }" class="searched-on-suggest">
                Suggest a portal
              </RouterLink>
            </div>
          </fieldset>

          <fieldset class="search-form-fieldset">
            <legend class="search-form-legend">How often</legend>
            <SegmentedControl
              v-model="form.frequency"
              name="frequency"
              :options="frequencyOptions"
              :invalid="Boolean(fieldErrors.frequency)"
              :described-by="fieldErrors.frequency ? 'frequency-error' : undefined"
            />
            <p v-if="fieldErrors.frequency" id="frequency-error" class="field-error">
              {{ fieldErrors.frequency }}
            </p>
          </fieldset>

          <div v-if="!isEdit" class="search-form-check">
            <label class="checkbox">
              <input
                v-model="runNow"
                type="checkbox"
                class="checkbox-input"
                aria-describedby="run-now-hint"
              />
              Run the first search right away
            </label>
            <p id="run-now-hint" class="field-hint">{{ runNowHint }}</p>
          </div>

          <div class="search-form-actions">
            <RouterLink to="/searches" class="btn btn-secondary">Cancel</RouterLink>
            <BusyButton :busy="busy" :busy-label="isEdit ? 'Saving…' : 'Creating…'">
              {{ isEdit ? 'Save changes' : 'Create search' }}
            </BusyButton>
          </div>
        </form>

        <aside class="card next-steps" aria-labelledby="next-title">
          <h2 id="next-title" class="next-steps-title">What happens next</h2>
          <ol class="next-steps-list">
            <li class="next-step">
              <span class="next-step-n" aria-hidden="true">1</span>
              <div>
                <div class="next-step-title">We fetch the listings</div>
                <div class="next-step-text" data-testid="next-step-fetch">{{ fetchStep }}</div>
              </div>
            </li>
            <li class="next-step">
              <span class="next-step-n" aria-hidden="true">2</span>
              <div>
                <div class="next-step-title">New jobs are read once</div>
                <div class="next-step-text">
                  Jobs we've already seen, on any portal, are skipped, so you're never shown a
                  duplicate.
                </div>
              </div>
            </li>
            <li class="next-step">
              <span class="next-step-n" aria-hidden="true">3</span>
              <div>
                <div class="next-step-title">Each one is scored</div>
                <div class="next-step-text">{{ scoreStep }}</div>
              </div>
            </li>
          </ol>
        </aside>
      </div>
    </div>
  </div>
</template>
