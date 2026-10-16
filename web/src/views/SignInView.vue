<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AuthLayout from '../components/AuthLayout.vue'
import AppIcon, { type IconName } from '../components/AppIcon'
import TextField from '../components/TextField.vue'
import BusyButton from '../components/BusyButton.vue'
import AlertMessage from '../components/AlertMessage.vue'
import { signIn } from '../stores/session'
import { safeRedirect } from '../utils/redirect'
import { splitApiError } from '../utils/formErrors'

const route = useRoute()
const router = useRouter()

const form = reactive({ email: '', password: '' })
const fieldErrors = reactive({ email: '', password: '' })
const formError = ref('')
const busy = ref(false)

const expired = computed(() => route.query.expired === '1' && !formError.value)

async function submit() {
  if (busy.value) return
  formError.value = ''
  fieldErrors.email = ''
  fieldErrors.password = ''
  busy.value = true
  try {
    await signIn({ email: form.email.trim(), password: form.password })
    await router.replace(safeRedirect(route.query.redirect))
  } catch (err) {
    const { fields, form: message } = splitApiError(err, ['email', 'password'])
    Object.assign(fieldErrors, fields)
    formError.value = message
  } finally {
    busy.value = false
  }
}

const points: { icon: IconName; title: string; text: string }[] = [
  {
    icon: 'file',
    title: 'One CV, many searches',
    text: 'Pick which CV is active; every evaluation uses it.',
  },
  {
    icon: 'search',
    title: 'Searches that run themselves',
    text: 'LinkedIn and Jobindex, hourly, daily or weekly.',
  },
  {
    icon: 'star',
    title: 'Scores with reasons',
    text: 'Strengths, missing skills and a summary for every match.',
  },
]
</script>

<template>
  <AuthLayout>
    <template #panel>
      <div class="auth-pitch">
        <h1 class="auth-title">Stop reading every job post. Read the ones that fit.</h1>
        <p class="auth-lede">
          Upload your CV once, tell us where to look, and every new job comes back scored against
          your experience.
        </p>
      </div>
      <ul class="auth-points">
        <li v-for="point in points" :key="point.title" class="auth-point">
          <span class="auth-point-icon"><AppIcon :name="point.icon" /></span>
          <div>
            <div class="auth-point-title">{{ point.title }}</div>
            <div class="auth-point-text">{{ point.text }}</div>
          </div>
        </li>
      </ul>
    </template>

    <form class="auth-form" novalidate @submit.prevent="submit">
      <div class="auth-form-head">
        <h2 class="auth-form-title">Welcome back</h2>
        <p class="auth-form-sub">Sign in to see your latest matches.</p>
      </div>

      <AlertMessage v-if="formError">{{ formError }}</AlertMessage>
      <AlertMessage v-else-if="expired" tone="info">
        Your session has ended. Sign in again to continue.
      </AlertMessage>

      <TextField
        id="email"
        v-model="form.email"
        label="Email"
        type="email"
        autocomplete="email"
        required
        :error="fieldErrors.email"
      />
      <TextField
        id="password"
        v-model="form.password"
        label="Password"
        type="password"
        autocomplete="current-password"
        required
        :error="fieldErrors.password"
      >
        <template #label-aside>
          <RouterLink to="/forgot-password" class="field-aside-link">Forgot password?</RouterLink>
        </template>
      </TextField>

      <BusyButton :busy="busy" busy-label="Signing in…">Sign in</BusyButton>

      <p class="auth-switch">
        New here? <RouterLink to="/signup" class="link-strong">Create an account</RouterLink>
      </p>
      <div class="auth-guest">
        Just want to check one job?
        <RouterLink to="/try" class="link-strong">Try a quick match without an account</RouterLink>
      </div>
    </form>
  </AuthLayout>
</template>
