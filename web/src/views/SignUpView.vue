<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import AuthLayout from '../components/AuthLayout.vue'
import TextField from '../components/TextField.vue'
import BusyButton from '../components/BusyButton.vue'
import AlertMessage from '../components/AlertMessage.vue'
import { signUp } from '../stores/session'
import { splitApiError } from '../utils/formErrors'

const router = useRouter()

// The API's parameter names, which it echoes back in error.param.
const FIELDS = ['first_name', 'last_name', 'email', 'phone', 'password'] as const
type Field = (typeof FIELDS)[number]

const form = reactive({ firstName: '', lastName: '', email: '', phone: '', password: '' })
const fieldErrors = reactive<Record<Field, string>>({
  first_name: '',
  last_name: '',
  email: '',
  phone: '',
  password: '',
})
const formError = ref('')
const busy = ref(false)

async function submit() {
  if (busy.value) return
  formError.value = ''
  FIELDS.forEach((f) => (fieldErrors[f] = ''))
  busy.value = true
  try {
    await signUp({
      firstName: form.firstName.trim(),
      lastName: form.lastName.trim(),
      email: form.email.trim(),
      phone: form.phone.trim(),
      password: form.password,
    })
    await router.replace({ name: 'overview' })
  } catch (err) {
    const { fields, form: message } = splitApiError(err, FIELDS)
    Object.assign(fieldErrors, fields)
    formError.value = message
  } finally {
    busy.value = false
  }
}

const steps = [
  { title: 'Create your account', text: "Email and password. That's it." },
  { title: 'Upload your CV', text: 'A PDF; we split it into skills, experience and education.' },
  {
    title: 'Add a job search',
    text: 'A role and a location. We check every portal and new jobs arrive scored.',
  },
]
</script>

<template>
  <AuthLayout>
    <template #panel>
      <div class="auth-steps">
        <h1 class="auth-title auth-title-sm">Three steps to your first scored job</h1>
        <ol class="auth-step-list">
          <li v-for="(step, i) in steps" :key="step.title" class="auth-step">
            <span class="auth-step-n" aria-hidden="true">{{ i + 1 }}</span>
            <div>
              <div class="auth-point-title">{{ step.title }}</div>
              <div class="auth-point-text">{{ step.text }}</div>
            </div>
          </li>
        </ol>
      </div>
    </template>

    <form class="auth-form auth-form-wide" novalidate @submit.prevent="submit">
      <div class="auth-form-head">
        <h2 class="auth-form-title">Create your account</h2>
        <p class="auth-form-sub">Free while in beta.</p>
      </div>

      <AlertMessage v-if="formError">{{ formError }}</AlertMessage>

      <div class="field-row">
        <TextField
          id="first_name"
          v-model="form.firstName"
          label="First name"
          autocomplete="given-name"
          :error="fieldErrors.first_name"
        />
        <TextField
          id="last_name"
          v-model="form.lastName"
          label="Last name"
          autocomplete="family-name"
          :error="fieldErrors.last_name"
        />
      </div>
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
        id="phone"
        v-model="form.phone"
        label="Phone"
        type="tel"
        autocomplete="tel"
        optional
        :error="fieldErrors.phone"
      />
      <TextField
        id="password"
        v-model="form.password"
        label="Password"
        type="password"
        autocomplete="new-password"
        required
        hint="At least 8 characters."
        :error="fieldErrors.password"
      />

      <BusyButton :busy="busy" busy-label="Creating account…">Create account</BusyButton>

      <p class="auth-switch">
        Already have an account? <RouterLink to="/signin" class="link-strong">Sign in</RouterLink>
      </p>
    </form>
  </AuthLayout>
</template>
