<script setup lang="ts">
// One saved search on the Job searches page: what it looks for, its schedule,
// and its actions. The enabled switch is optimistic (the card flips at once
// and rolls back if the API refuses); "Run now", edit and delete (after an
// inline confirmation) talk to the API through api/jobSearches.
import { computed, nextTick, ref } from 'vue'
import { useRouter } from 'vue-router'
import AppIcon from './AppIcon'
import ActionMenu, { type ActionMenuItem } from './ActionMenu.vue'
import AlertMessage from './AlertMessage.vue'
import BusyButton from './BusyButton.vue'
import ToggleSwitch from './ToggleSwitch.vue'
import { ApiError } from '../api/client'
import {
  CODE_JOB_SEARCH_DISABLED,
  deleteJobSearch,
  runJobSearch,
  setJobSearchEnabled,
} from '../api/jobSearches'
import type { JobSearch } from '../api/types'
import { capitalise, formatRunTime, formatUpcoming } from '../utils/format'

const props = defineProps<{ search: JobSearch }>()
const emit = defineEmits<{ update: [search: JobSearch]; deleted: [search: JobSearch] }>()

const router = useRouter()

const toggling = ref(false)
const running = ref(false)
const deleting = ref(false)
const confirmingDelete = ref(false)
const error = ref('')
const notice = ref('')

const menu = ref<InstanceType<typeof ActionMenu> | null>(null)
const cancelDelete = ref<HTMLButtonElement | null>(null)

const titleId = computed(() => `search-${props.search.id}-title`)

const what = computed(() => {
  const q = `"${props.search.query}"`
  return props.search.location ? `${q} in ${props.search.location}` : `${q} · any location`
})

const lastRun = computed(() =>
  props.search.last_run_at ? formatRunTime(props.search.last_run_at) : 'not yet',
)
const nextRun = computed(() =>
  props.search.next_run_at ? formatUpcoming(props.search.next_run_at) : 'waiting for the scheduler',
)

const menuItems: ActionMenuItem[] = [
  { key: 'edit', label: 'Edit', icon: 'pencil' },
  { key: 'delete', label: 'Delete', icon: 'trash', danger: true },
]

function messageOf(err: unknown): string {
  return err instanceof Error && err.message ? err.message : 'Something went wrong. Try again.'
}

async function toggle(enabled: boolean) {
  if (toggling.value) return
  const before = props.search
  error.value = ''
  notice.value = ''
  toggling.value = true
  emit('update', { ...before, enabled })
  try {
    emit('update', await setJobSearchEnabled(before.id, enabled))
  } catch (err) {
    emit('update', before)
    error.value = `Couldn't ${enabled ? 'turn this search on' : 'pause this search'}. ${messageOf(err)}`
  } finally {
    toggling.value = false
  }
}

async function runNow() {
  if (running.value) return
  error.value = ''
  notice.value = ''
  running.value = true
  try {
    emit('update', await runJobSearch(props.search.id))
    notice.value = 'Queued. This search runs on every portal in a moment.'
  } catch (err) {
    if (err instanceof ApiError && err.code === CODE_JOB_SEARCH_DISABLED) {
      // Someone paused it elsewhere; show what the server knows.
      emit('update', { ...props.search, enabled: false, next_run_at: null })
      error.value = "This search is paused, so it can't run. Turn it on first."
    } else {
      error.value = `Couldn't start this search. ${messageOf(err)}`
    }
  } finally {
    running.value = false
  }
}

async function onMenu(key: string) {
  if (key === 'edit') {
    router.push({ name: 'search-edit', params: { id: props.search.id } })
  } else if (key === 'delete') {
    error.value = ''
    notice.value = ''
    confirmingDelete.value = true
    await nextTick()
    cancelDelete.value?.focus()
  }
}

function keep() {
  confirmingDelete.value = false
  menu.value?.focus()
}

async function confirmDelete() {
  if (deleting.value) return
  error.value = ''
  deleting.value = true
  try {
    await deleteJobSearch(props.search.id)
    emit('deleted', props.search)
  } catch (err) {
    error.value = `Couldn't delete this search. ${messageOf(err)}`
    deleting.value = false
  }
}
</script>

<template>
  <article
    class="card search-card"
    :class="{ 'is-paused': !search.enabled }"
    :aria-labelledby="titleId"
    data-testid="search-card"
  >
    <div class="search-card-main">
      <span class="search-card-icon" aria-hidden="true"><AppIcon name="search" /></span>
      <div class="search-card-body">
        <div class="search-card-title-row">
          <h2 :id="titleId" class="search-card-title">{{ search.name }}</h2>
          <span v-if="search.enabled" class="badge badge-green">Running</span>
          <span v-else class="badge badge-grey">Paused</span>
        </div>
        <div class="search-card-meta">
          <span>{{ what }}</span>
          <span>{{ capitalise(search.frequency) }}</span>
          <span>
            Last run <b>{{ lastRun }}</b>
          </span>
          <span v-if="search.enabled">
            Next <b>{{ nextRun }}</b>
          </span>
        </div>
      </div>
      <div class="search-card-actions">
        <BusyButton
          v-if="search.enabled"
          type="button"
          variant="secondary"
          class="btn-sm"
          :busy="running"
          busy-label="Queuing…"
          @click="runNow"
        >
          <AppIcon name="rerun" />Run now
        </BusyButton>
        <ToggleSwitch
          :model-value="search.enabled"
          :label="`Run ${search.name} on schedule`"
          :disabled="toggling"
          :busy="toggling"
          @update:model-value="toggle"
        />
        <ActionMenu
          ref="menu"
          :label="`More actions for ${search.name}`"
          :items="menuItems"
          @select="onMenu"
        />
      </div>
    </div>

    <div
      v-if="confirmingDelete"
      class="search-card-confirm"
      role="group"
      :aria-labelledby="`${titleId}-confirm`"
    >
      <p :id="`${titleId}-confirm`" class="search-card-confirm-text">
        Delete <b>{{ search.name }}</b
        >? It stops running and can't be restored.
      </p>
      <div class="search-card-confirm-actions">
        <button
          ref="cancelDelete"
          type="button"
          class="btn btn-secondary btn-sm"
          :disabled="deleting"
          @click="keep"
        >
          Cancel
        </button>
        <BusyButton
          type="button"
          variant="secondary"
          class="btn-sm btn-danger"
          :busy="deleting"
          busy-label="Deleting…"
          @click="confirmDelete"
        >
          <AppIcon name="trash" />Delete search
        </BusyButton>
      </div>
    </div>

    <div v-if="error" class="search-card-message">
      <AlertMessage>{{ error }}</AlertMessage>
    </div>
    <div v-else-if="notice" class="search-card-message">
      <AlertMessage tone="info">{{ notice }}</AlertMessage>
    </div>
  </article>
</template>
