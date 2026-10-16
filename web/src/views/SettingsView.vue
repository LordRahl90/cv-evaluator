<script setup lang="ts">
// Settings: the profile from GET /me, read-only. The API has no endpoints
// yet to update the profile, change the password or delete the account, so
// the page says so instead of showing forms that cannot be saved.
import { computed, onMounted, ref } from 'vue'
import AlertMessage from '../components/AlertMessage.vue'
import AppIcon from '../components/AppIcon'
import { loadUser, session } from '../stores/session'

const error = ref<Error | null>(null)
const loading = ref(false)

async function load(force = false) {
  error.value = null
  loading.value = true
  try {
    await loadUser({ force })
  } catch (err) {
    error.value = err instanceof Error ? err : new Error(String(err))
  } finally {
    loading.value = false
  }
}

onMounted(() => load())

const user = computed(() => session.user)

const fields = computed(() => {
  const u = user.value
  if (!u) return []
  return [
    { key: 'first-name', label: 'First name', value: u.first_name },
    { key: 'last-name', label: 'Last name', value: u.last_name },
    { key: 'email', label: 'Email', value: u.email },
    { key: 'phone', label: 'Phone', value: u.phone },
  ]
})
</script>

<template>
  <div class="page">
    <div class="page-stack settings-stack">
      <div class="page-head">
        <div class="page-head-text">
          <h1 class="page-title">Settings</h1>
        </div>
      </div>

      <section class="card settings-section" aria-labelledby="profile-title">
        <h2 id="profile-title" class="section-title">Profile</h2>

        <dl v-if="user" class="profile-grid" data-testid="profile">
          <div v-for="field in fields" :key="field.key" class="profile-item">
            <dt class="profile-label">{{ field.label }}</dt>
            <dd
              class="profile-value"
              :class="{ 'is-empty': !field.value }"
              :data-testid="`profile-${field.key}`"
            >
              {{ field.value || 'Not set' }}
            </dd>
          </div>
        </dl>

        <div v-else-if="error" class="settings-error">
          <AlertMessage>We couldn't load your profile. {{ error.message }}</AlertMessage>
          <button type="button" class="btn btn-secondary" :disabled="loading" @click="load(true)">
            <AppIcon name="refresh" />Try again
          </button>
        </div>

        <div v-else class="profile-grid" role="status" aria-live="polite">
          <span class="sr-only">Loading your profile…</span>
          <div
            v-for="n in 4"
            :key="n"
            class="profile-item"
            aria-hidden="true"
            data-testid="profile-skeleton"
          >
            <span class="skeleton skeleton-line skeleton-w-24" />
            <span class="skeleton profile-skeleton-value" />
          </div>
        </div>

        <p class="settings-note" data-testid="settings-note">
          Editing your profile, changing your password and deleting your account aren't available
          yet. They're on the way; until then these details are read-only.
        </p>
      </section>
    </div>
  </div>
</template>
