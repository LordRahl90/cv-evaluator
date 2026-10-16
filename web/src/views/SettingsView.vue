<script setup lang="ts">
// Settings: edit the profile (PATCH /me), change the password
// (POST /me/password, which returns a fresh session: older ones stop
// working), download everything stored about the account (GET /me/export)
// and delete the account (DELETE /me, after a confirmation that asks for the
// password and a typed confirmation word).
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import AlertMessage from '../components/AlertMessage.vue'
import AppIcon from '../components/AppIcon'
import BusyButton from '../components/BusyButton.vue'
import ModalDialog from '../components/ModalDialog.vue'
import TextField from '../components/TextField.vue'
import { ApiError } from '../api/client'
import {
  EXPORT_FILENAME,
  INCORRECT_PASSWORD,
  PASSWORD_MAX_BYTES,
  PASSWORD_MIN_LENGTH,
  changePassword,
  deleteAccount,
  exportAccount,
  updateProfile,
  type ProfileChanges,
} from '../api/account'
import type { User } from '../api/types'
import { loadUser, renewSession, session, setUser, signOut } from '../stores/session'
import { saveJsonFile } from '../utils/download'
import { splitApiError } from '../utils/formErrors'

const router = useRouter()

function messageOf(err: unknown): string {
  return err instanceof Error && err.message ? err.message : 'Something went wrong. Try again.'
}

function isIncorrectPassword(err: unknown): boolean {
  return err instanceof ApiError && err.code === INCORRECT_PASSWORD
}

// A light check before the API's own (which also rejects display names).
const EMAIL_SHAPE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

// ---- Loading the profile ------------------------------------------------

const loadError = ref<Error | null>(null)
const loading = ref(false)

async function load(force = false) {
  loadError.value = null
  loading.value = true
  try {
    await loadUser({ force })
  } catch (err) {
    loadError.value = err instanceof Error ? err : new Error(String(err))
  } finally {
    loading.value = false
  }
}

onMounted(() => load())

const user = computed(() => session.user)

// ---- Profile --------------------------------------------------------------

const PROFILE_FIELDS = ['first_name', 'last_name', 'email', 'phone'] as const
type ProfileField = (typeof PROFILE_FIELDS)[number]

const profile = reactive<Record<ProfileField, string>>({
  first_name: '',
  last_name: '',
  email: '',
  phone: '',
})
const profileErrors = reactive<Record<ProfileField, string>>({
  first_name: '',
  last_name: '',
  email: '',
  phone: '',
})
const profileFormError = ref('')
const profileNotice = ref('')
const savingProfile = ref(false)

function fillProfile(u: User) {
  for (const field of PROFILE_FIELDS) profile[field] = u[field] ?? ''
}

// Fill the form once the profile is loaded (and after each save).
watch(user, (u) => u && fillProfile(u), { immediate: true })

// A saved message goes away as soon as the form is edited again.
watch(profile, () => (profileNotice.value = ''))

function profileChanges(current: User): ProfileChanges {
  const changes: ProfileChanges = {}
  for (const field of PROFILE_FIELDS) {
    const value = profile[field].trim()
    const before = field === 'email' ? current.email.toLowerCase() : current[field]
    const after = field === 'email' ? value.toLowerCase() : value
    if (after !== before) changes[field] = value
  }
  return changes
}

async function saveProfile() {
  const current = user.value
  if (!current || savingProfile.value) return
  for (const field of PROFILE_FIELDS) profileErrors[field] = ''
  profileFormError.value = ''
  profileNotice.value = ''

  const email = profile.email.trim()
  if (!email) {
    profileErrors.email = 'Enter your email address.'
    return
  }
  if (!EMAIL_SHAPE.test(email)) {
    profileErrors.email = 'Enter a valid email address, like name@example.com.'
    return
  }

  const changes = profileChanges(current)
  if (!Object.keys(changes).length) {
    profileNotice.value = 'Your profile is already up to date.'
    return
  }

  savingProfile.value = true
  try {
    const updated = await updateProfile(changes)
    // Updates the name and email everywhere, e.g. in the account menu.
    setUser(updated)
    fillProfile(updated)
    await nextTick()
    profileNotice.value = 'Profile saved.'
  } catch (err) {
    const { fields, form } = splitApiError(err, PROFILE_FIELDS)
    Object.assign(profileErrors, fields)
    profileFormError.value = form
  } finally {
    savingProfile.value = false
  }
}

// ---- Password -------------------------------------------------------------

const password = reactive({ current: '', next: '', confirm: '' })
const passwordErrors = reactive({ current: '', next: '', confirm: '' })
const passwordFormError = ref('')
const passwordNotice = ref('')
const changingPassword = ref(false)

watch(password, () => (passwordNotice.value = ''))

function validatePassword(): boolean {
  if (!password.current) passwordErrors.current = 'Enter your current password.'
  if (!password.next) {
    passwordErrors.next = 'Choose a new password.'
  } else if ([...password.next].length < PASSWORD_MIN_LENGTH) {
    passwordErrors.next = `Use at least ${PASSWORD_MIN_LENGTH} characters.`
  } else if (new TextEncoder().encode(password.next).length > PASSWORD_MAX_BYTES) {
    passwordErrors.next = 'That password is too long. Use a shorter one.'
  }
  if (password.next && password.confirm !== password.next) {
    passwordErrors.confirm = "The passwords don't match."
  }
  return !passwordErrors.current && !passwordErrors.next && !passwordErrors.confirm
}

async function submitPassword() {
  if (changingPassword.value) return
  passwordErrors.current = ''
  passwordErrors.next = ''
  passwordErrors.confirm = ''
  passwordFormError.value = ''
  passwordNotice.value = ''
  if (!validatePassword()) return

  changingPassword.value = true
  try {
    const fresh = await changePassword({
      current_password: password.current,
      new_password: password.next,
    })
    // The old token no longer works; keep this tab signed in with the new one.
    renewSession(fresh)
    password.current = ''
    password.next = ''
    password.confirm = ''
    await nextTick()
    passwordNotice.value =
      'Password changed. You stay signed in here; other devices need to sign in again.'
  } catch (err) {
    if (isIncorrectPassword(err)) {
      passwordErrors.current = "That isn't your current password."
    } else {
      const { fields, form } = splitApiError(err, ['current_password', 'new_password'] as const)
      passwordErrors.current = fields.current_password ?? ''
      passwordErrors.next = fields.new_password ?? ''
      passwordFormError.value = form
    }
  } finally {
    changingPassword.value = false
  }
}

// ---- Download my data -----------------------------------------------------

const exporting = ref(false)
const exportError = ref('')
const exportNotice = ref('')

async function downloadData() {
  if (exporting.value) return
  exporting.value = true
  exportError.value = ''
  exportNotice.value = ''
  try {
    saveJsonFile(EXPORT_FILENAME, await exportAccount())
    exportNotice.value = `Your data was downloaded as ${EXPORT_FILENAME}.`
  } catch (err) {
    exportError.value = `We couldn't prepare your data. ${messageOf(err)}`
  } finally {
    exporting.value = false
  }
}

// ---- Delete account -------------------------------------------------------

const CONFIRM_WORD = 'DELETE'

const deleteOpen = ref(false)
const deleting = ref(false)
const deleteForm = reactive({ password: '', confirm: '' })
const deleteErrors = reactive({ password: '', confirm: '' })
const deleteFormError = ref('')

function openDelete() {
  deleteForm.password = ''
  deleteForm.confirm = ''
  deleteErrors.password = ''
  deleteErrors.confirm = ''
  deleteFormError.value = ''
  deleteOpen.value = true
}

function closeDelete() {
  if (!deleting.value) deleteOpen.value = false
}

async function confirmDelete() {
  if (deleting.value) return
  deleteErrors.password = deleteForm.password ? '' : 'Enter your password.'
  deleteErrors.confirm =
    deleteForm.confirm.trim() === CONFIRM_WORD ? '' : `Type ${CONFIRM_WORD} to confirm.`
  deleteFormError.value = ''
  if (deleteErrors.password || deleteErrors.confirm) return

  deleting.value = true
  try {
    await deleteAccount(deleteForm.password)
    deleting.value = false
    deleteOpen.value = false
    signOut()
    await router.replace({ name: 'signin', query: { deleted: '1' } })
  } catch (err) {
    if (isIncorrectPassword(err)) {
      deleteErrors.password = "That password isn't right."
    } else {
      deleteFormError.value = messageOf(err)
    }
    deleting.value = false
  }
}

const deletedItems = [
  'Your profile and sign-in',
  'Every CV you uploaded',
  'Your job searches and all the matches they found',
  'Your API keys (scripts using them stop working)',
  'Your portal suggestions',
]
</script>

<template>
  <div class="page">
    <div class="page-stack settings-stack">
      <div class="page-head">
        <div class="page-head-text">
          <h1 class="page-title">Settings</h1>
        </div>
      </div>

      <!-- Profile -->
      <form
        v-if="user"
        class="card settings-section"
        aria-labelledby="profile-title"
        novalidate
        data-testid="profile-form"
        @submit.prevent="saveProfile"
      >
        <h2 id="profile-title" class="section-title">Profile</h2>
        <div class="settings-grid">
          <TextField
            id="first-name"
            v-model="profile.first_name"
            label="First name"
            autocomplete="given-name"
            :error="profileErrors.first_name"
          />
          <TextField
            id="last-name"
            v-model="profile.last_name"
            label="Last name"
            autocomplete="family-name"
            :error="profileErrors.last_name"
          />
          <TextField
            id="email"
            v-model="profile.email"
            label="Email"
            type="email"
            autocomplete="email"
            required
            :error="profileErrors.email"
          />
          <TextField
            id="phone"
            v-model="profile.phone"
            label="Phone"
            type="tel"
            autocomplete="tel"
            optional
            :error="profileErrors.phone"
          />
        </div>
        <AlertMessage v-if="profileFormError">{{ profileFormError }}</AlertMessage>
        <AlertMessage v-if="profileNotice" tone="success">{{ profileNotice }}</AlertMessage>
        <div class="settings-actions">
          <BusyButton :busy="savingProfile" busy-label="Saving…">Save profile</BusyButton>
        </div>
      </form>

      <section v-else class="card settings-section" aria-labelledby="profile-title">
        <h2 id="profile-title" class="section-title">Profile</h2>

        <div v-if="loadError" class="settings-error">
          <AlertMessage>We couldn't load your profile. {{ loadError.message }}</AlertMessage>
          <button type="button" class="btn btn-secondary" :disabled="loading" @click="load(true)">
            <AppIcon name="refresh" />Try again
          </button>
        </div>

        <div v-else class="settings-grid" role="status" aria-live="polite">
          <span class="sr-only">Loading your profile…</span>
          <div
            v-for="n in 4"
            :key="n"
            class="settings-skeleton-field"
            aria-hidden="true"
            data-testid="profile-skeleton"
          >
            <span class="skeleton skeleton-line skeleton-w-24" />
            <span class="skeleton settings-skeleton-input" />
          </div>
        </div>
      </section>

      <!-- Password -->
      <form
        class="card settings-section"
        aria-labelledby="password-title"
        novalidate
        data-testid="password-form"
        @submit.prevent="submitPassword"
      >
        <div class="settings-section-head">
          <h2 id="password-title" class="section-title">Password</h2>
          <p class="settings-text">
            Changing it signs you out on every other device. API keys keep working; revoke them on
            the API keys page.
          </p>
        </div>
        <!-- Lets password managers tie the new password to the account. -->
        <input
          class="sr-only"
          type="email"
          name="username"
          autocomplete="username"
          tabindex="-1"
          aria-hidden="true"
          :value="user?.email ?? ''"
          readonly
        />
        <div class="settings-grid">
          <TextField
            id="current-password"
            v-model="password.current"
            label="Current password"
            type="password"
            autocomplete="current-password"
            required
            :error="passwordErrors.current"
          />
          <TextField
            id="new-password"
            v-model="password.next"
            class="settings-grid-row-start"
            label="New password"
            type="password"
            autocomplete="new-password"
            :hint="`At least ${PASSWORD_MIN_LENGTH} characters.`"
            required
            :error="passwordErrors.next"
          />
          <TextField
            id="confirm-password"
            v-model="password.confirm"
            label="Confirm new password"
            type="password"
            autocomplete="new-password"
            required
            :error="passwordErrors.confirm"
          />
        </div>
        <AlertMessage v-if="passwordFormError">{{ passwordFormError }}</AlertMessage>
        <AlertMessage v-if="passwordNotice" tone="success">{{ passwordNotice }}</AlertMessage>
        <div class="settings-actions">
          <BusyButton variant="secondary" :busy="changingPassword" busy-label="Changing…">
            Change password
          </BusyButton>
        </div>
      </form>

      <!-- Danger zone -->
      <section class="card settings-section settings-danger" aria-labelledby="danger-title">
        <h2 id="danger-title" class="section-title settings-danger-title">Delete account</h2>
        <p class="settings-text">
          Deletes your account, CVs, searches, matches and API keys for good. This can't be undone.
        </p>
        <AlertMessage v-if="exportError">{{ exportError }}</AlertMessage>
        <AlertMessage v-if="exportNotice" tone="success">{{ exportNotice }}</AlertMessage>
        <div class="settings-danger-actions">
          <BusyButton
            type="button"
            variant="secondary"
            :busy="exporting"
            busy-label="Preparing your data…"
            @click="downloadData"
          >
            Download my data first
          </BusyButton>
          <button type="button" class="btn btn-danger" @click="openDelete">Delete account</button>
        </div>
      </section>
    </div>

    <ModalDialog
      :open="deleteOpen"
      title="Delete your account?"
      :locked="deleting"
      @close="closeDelete"
    >
      <form class="dialog-form" novalidate @submit.prevent="confirmDelete">
        <p class="dialog-text">This permanently deletes:</p>
        <ul class="delete-list">
          <li v-for="item in deletedItems" :key="item">{{ item }}</li>
        </ul>
        <p class="dialog-text">
          It can't be undone. If you want a copy, download your data before you continue.
        </p>
        <TextField
          id="delete-password"
          v-model="deleteForm.password"
          label="Your password"
          type="password"
          autocomplete="current-password"
          required
          :error="deleteErrors.password"
        />
        <TextField
          id="delete-confirm"
          v-model="deleteForm.confirm"
          :label="`Type ${CONFIRM_WORD} to confirm`"
          autocomplete="off"
          required
          :error="deleteErrors.confirm"
        />
        <AlertMessage v-if="deleteFormError">{{ deleteFormError }}</AlertMessage>
        <div class="dialog-actions">
          <button type="button" class="btn btn-secondary" :disabled="deleting" @click="closeDelete">
            Cancel
          </button>
          <BusyButton variant="danger" :busy="deleting" busy-label="Deleting…">
            Delete account
          </BusyButton>
        </div>
      </form>
    </ModalDialog>
  </div>
</template>
