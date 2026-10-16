<script setup lang="ts">
// API keys: list (GET /api-keys), create (POST /api-keys, the secret is
// shown once in a banner and dropped from memory on "Done") and revoke
// (DELETE /api-keys/:id, after a confirmation).
import { computed, nextTick, onMounted, ref } from 'vue'
import AlertMessage from '../components/AlertMessage.vue'
import AppIcon from '../components/AppIcon'
import BusyButton from '../components/BusyButton.vue'
import ModalDialog from '../components/ModalDialog.vue'
import TextField from '../components/TextField.vue'
import { API_BASE_URL } from '../api/client'
import {
  API_KEY_NAME_MAX,
  createApiKey,
  listApiKeys,
  revokeApiKey,
  type ApiKey,
  type CreatedApiKey,
} from '../api/apiKeys'
import { formatRelative } from '../utils/format'
import { splitApiError } from '../utils/formErrors'

type LoadStatus = 'loading' | 'ready' | 'error'

const status = ref<LoadStatus>('loading')
const loadError = ref<Error | null>(null)
const keys = ref<ApiKey[]>([])
const announcement = ref('')
const heading = ref<HTMLElement | null>(null)

async function load() {
  status.value = 'loading'
  loadError.value = null
  try {
    keys.value = await listApiKeys()
    status.value = 'ready'
  } catch (err) {
    loadError.value = err instanceof Error ? err : new Error(String(err))
    status.value = 'error'
  }
}

onMounted(load)

const countText = computed(() => {
  const n = keys.value.length
  return `${n} active ${n === 1 ? 'key' : 'keys'}`
})

// The address this app sends API calls to, for the curl example.
const apiRoot = new URL(API_BASE_URL || '/', window.location.origin).href.replace(/\/$/, '')
const curlExample = `curl ${apiRoot}/job-searches \\\n  -H "Authorization: Bearer <your API key>"`

// "2 Oct 2026". Built by hand: Intl's en-GB short month for September is
// "Sept", which the mockups don't use.
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

function formatDate(value: string): string {
  const d = new Date(value)
  return `${d.getDate()} ${MONTHS[d.getMonth()]} ${d.getFullYear()}`
}

function formatCreated(value: string): string {
  const age = Date.now() - new Date(value).getTime()
  return age < 60_000 ? 'Just now' : formatDate(value)
}

function formatLastUsed(value: string | null): string {
  return value ? formatRelative(value) : 'Never'
}

function usedRecently(value: string | null): boolean {
  return value !== null && Date.now() - new Date(value).getTime() < 86_400_000
}

// ---- Create -------------------------------------------------------------

const createOpen = ref(false)
const createName = ref('')
const createNameError = ref('')
const createFormError = ref('')
const creating = ref(false)

function openCreate() {
  createName.value = ''
  createNameError.value = ''
  createFormError.value = ''
  createOpen.value = true
}

function closeCreate() {
  if (!creating.value) createOpen.value = false
}

// The new key with its secret, shown in the banner until "Done".
const created = ref<CreatedApiKey | null>(null)
const copyState = ref<'idle' | 'copied' | 'failed'>('idle')
const secretEl = ref<HTMLElement | null>(null)
const copyButton = ref<HTMLButtonElement | null>(null)
const createButton = ref<HTMLButtonElement | null>(null)

async function submitCreate() {
  const name = createName.value.trim()
  createNameError.value = ''
  createFormError.value = ''
  if (!name) {
    createNameError.value = 'Give the key a name.'
    return
  }
  if (name.length > API_KEY_NAME_MAX) {
    createNameError.value = `Use at most ${API_KEY_NAME_MAX} characters.`
    return
  }

  creating.value = true
  try {
    const key = await createApiKey(name)
    keys.value = [toListed(key), ...keys.value.filter((k) => k.id !== key.id)]
    created.value = key
    copyState.value = 'idle'
    creating.value = false
    createOpen.value = false
    await nextTick()
    copyButton.value?.focus()
  } catch (err) {
    const { fields, form } = splitApiError(err, ['name'] as const)
    createNameError.value = fields.name ?? ''
    createFormError.value = form
    creating.value = false
  }
}

function toListed(key: CreatedApiKey): ApiKey {
  return {
    object: key.object,
    id: key.id,
    name: key.name,
    prefix: key.prefix,
    created_at: key.created_at,
    last_used_at: key.last_used_at,
  }
}

async function copySecret() {
  const secret = created.value?.secret
  if (!secret) return
  try {
    await navigator.clipboard.writeText(secret)
    copyState.value = 'copied'
  } catch {
    // No clipboard access (insecure origin, permission denied): select the
    // key so a keyboard shortcut copies it.
    copyState.value = 'failed'
    const el = secretEl.value
    const selection = window.getSelection()
    if (el && selection) {
      const range = document.createRange()
      range.selectNodeContents(el)
      selection.removeAllRanges()
      selection.addRange(range)
    }
  }
}

async function dismissSecret() {
  created.value = null
  copyState.value = 'idle'
  window.getSelection()?.removeAllRanges()
  await nextTick()
  createButton.value?.focus()
}

// ---- Revoke -------------------------------------------------------------

const revokeTarget = ref<ApiKey | null>(null)
const revokeOpen = ref(false)
const revoking = ref(false)
const revokeError = ref('')

function askRevoke(key: ApiKey) {
  revokeTarget.value = key
  revokeError.value = ''
  revokeOpen.value = true
}

const revokeTitle = computed(() => `Revoke "${revokeTarget.value?.name ?? ''}"?`)

function closeRevoke() {
  if (!revoking.value) revokeOpen.value = false
}

async function confirmRevoke() {
  const key = revokeTarget.value
  if (!key) return
  revoking.value = true
  revokeError.value = ''
  try {
    await revokeApiKey(key.id)
    keys.value = keys.value.filter((k) => k.id !== key.id)
    if (created.value?.id === key.id) created.value = null
    announcement.value = `Revoked "${key.name}".`
    revoking.value = false
    revokeOpen.value = false
    await nextTick()
    // The row (and its button) is gone; keep focus on the page.
    heading.value?.focus()
  } catch (err) {
    revokeError.value = err instanceof Error && err.message ? err.message : 'Something went wrong.'
    revoking.value = false
  }
}
</script>

<template>
  <div class="page">
    <p class="sr-only" role="status" aria-live="polite">{{ announcement }}</p>

    <!-- Loading -->
    <div v-if="status === 'loading'" class="page-stack" role="status" aria-live="polite">
      <span class="sr-only">Loading your API keys…</span>
      <div class="page-head" aria-hidden="true">
        <div class="page-head-text">
          <span class="skeleton skeleton-title" />
          <span class="skeleton skeleton-line skeleton-w-32" />
        </div>
      </div>
      <div class="card skeleton-card" aria-hidden="true" data-testid="api-keys-skeleton">
        <span class="skeleton skeleton-row" />
        <span class="skeleton skeleton-row" />
        <span class="skeleton skeleton-row" />
      </div>
    </div>

    <!-- Error -->
    <div v-else-if="status === 'error'" class="page-stack">
      <div class="page-head">
        <div class="page-head-text">
          <h1 class="page-title">API keys</h1>
        </div>
      </div>
      <section class="card error-card">
        <AlertMessage>We couldn't load your API keys. {{ loadError?.message }}</AlertMessage>
        <button type="button" class="btn btn-secondary" @click="load">
          <AppIcon name="refresh" />Try again
        </button>
      </section>
    </div>

    <!-- Ready -->
    <div v-else class="page-stack">
      <div class="page-head">
        <div class="page-head-text">
          <h1 ref="heading" class="page-title keys-heading" tabindex="-1">API keys</h1>
          <p v-if="keys.length" class="page-sub" data-testid="api-keys-count">{{ countText }}</p>
          <p v-else class="page-sub">
            Use the API to upload CVs, manage searches and pull matches from your own scripts.
          </p>
        </div>
        <div v-if="keys.length" class="page-actions">
          <button ref="createButton" type="button" class="btn btn-primary" @click="openCreate">
            <AppIcon name="plus" />Create API key
          </button>
        </div>
      </div>

      <!-- The new secret, shown once -->
      <section
        v-if="created"
        class="card secret-banner"
        aria-labelledby="new-key-title"
        data-testid="new-key-banner"
      >
        <div class="secret-banner-head">
          <AppIcon name="check" />
          <h2 id="new-key-title" class="secret-banner-title">
            "{{ created.name }}" created. Copy the key now.
          </h2>
        </div>
        <p class="secret-banner-text">
          For your security we only show it once. If you lose it, revoke it and create a new one.
        </p>
        <div class="secret-banner-row">
          <code
            ref="secretEl"
            class="secret-value"
            tabindex="0"
            aria-label="New API key"
            data-testid="new-key-secret"
            >{{ created.secret }}</code
          >
          <button ref="copyButton" type="button" class="btn btn-secondary" @click="copySecret">
            <AppIcon :name="copyState === 'copied' ? 'check' : 'copy'" />{{
              copyState === 'copied' ? 'Copied' : 'Copy'
            }}
          </button>
          <button type="button" class="btn btn-secondary" @click="dismissSecret">Done</button>
        </div>
        <p class="secret-banner-feedback" role="status" aria-live="polite">
          <template v-if="copyState === 'copied'">Key copied to the clipboard.</template>
          <template v-else-if="copyState === 'failed'">
            Couldn't copy automatically. The key is selected: press Ctrl+C (⌘C on a Mac) to copy it.
          </template>
        </p>
      </section>

      <!-- Empty -->
      <template v-if="!keys.length">
        <section class="card" aria-labelledby="no-keys-title">
          <div class="keys-empty">
            <span class="keys-empty-icon"><AppIcon name="key" /></span>
            <h2 id="no-keys-title" class="keys-empty-title">No API keys yet</h2>
            <p class="keys-empty-text">
              Create a key for each script or integration, so you can revoke one without breaking
              the others.
            </p>
            <button type="button" class="btn btn-primary keys-empty-action" @click="openCreate">
              <AppIcon name="plus" />Create API key
            </button>
          </div>
        </section>

        <section class="card keys-usage" aria-labelledby="usage-title">
          <h2 id="usage-title" class="keys-usage-title">How you'll use it</h2>
          <pre class="code-block" tabindex="0" aria-label="Example request">{{ curlExample }}</pre>
        </section>
      </template>

      <!-- List -->
      <template v-else>
        <section class="card table-card" aria-label="API keys">
          <div class="table-scroll" tabindex="0" aria-label="API keys table, scrolls sideways">
            <table class="data-table keys-table">
              <thead>
                <tr>
                  <th scope="col">Name</th>
                  <th scope="col">Key</th>
                  <th scope="col">Created</th>
                  <th scope="col">Last used</th>
                  <th scope="col" class="data-table-actions">
                    <span class="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="key in keys" :key="key.id" :data-testid="`api-key-${key.id}`">
                  <td class="keys-name">{{ key.name }}</td>
                  <td class="keys-prefix">{{ key.prefix }}…</td>
                  <td class="data-table-muted">{{ formatCreated(key.created_at) }}</td>
                  <td :class="{ 'data-table-muted': !usedRecently(key.last_used_at) }">
                    {{ formatLastUsed(key.last_used_at) }}
                  </td>
                  <td class="data-table-actions">
                    <button
                      type="button"
                      class="btn btn-sm btn-danger-outline"
                      :aria-label="`Revoke ${key.name}`"
                      @click="askRevoke(key)"
                    >
                      Revoke
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
        <p class="keys-footnote">
          Keys act as you: anyone with a key can read your matches and change your searches.
          Revoking takes effect immediately.
        </p>
      </template>
    </div>

    <ModalDialog :open="createOpen" title="Create API key" :locked="creating" @close="closeCreate">
      <form class="dialog-form" novalidate @submit.prevent="submitCreate">
        <p class="dialog-text">
          Name it after the script or integration that will use it. You'll see the key once, right
          after you create it.
        </p>
        <TextField
          id="api-key-name"
          v-model="createName"
          label="Name"
          hint="For example: Nightly export script"
          autocomplete="off"
          :error="createNameError"
        />
        <AlertMessage v-if="createFormError">{{ createFormError }}</AlertMessage>
        <div class="dialog-actions">
          <button type="button" class="btn btn-secondary" :disabled="creating" @click="closeCreate">
            Cancel
          </button>
          <BusyButton :busy="creating" busy-label="Creating…">Create key</BusyButton>
        </div>
      </form>
    </ModalDialog>

    <ModalDialog :open="revokeOpen" :title="revokeTitle" :locked="revoking" @close="closeRevoke">
      <div class="dialog-form">
        <p class="dialog-text">
          Scripts and integrations using this key stop working immediately. This can't be undone.
        </p>
        <AlertMessage v-if="revokeError">{{ revokeError }}</AlertMessage>
        <div class="dialog-actions">
          <button type="button" class="btn btn-secondary" :disabled="revoking" @click="closeRevoke">
            Cancel
          </button>
          <BusyButton
            type="button"
            variant="danger"
            :busy="revoking"
            busy-label="Revoking…"
            @click="confirmRevoke"
          >
            Revoke key
          </BusyButton>
        </div>
      </div>
    </ModalDialog>
  </div>
</template>
