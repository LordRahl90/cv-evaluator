<script setup lang="ts">
import { useRoute } from 'vue-router'
import AuthLayout from '../components/AuthLayout.vue'
import AppIcon from '../components/AppIcon'

const route = useRoute()
</script>

<template>
  <AuthLayout>
    <template #panel>
      <div class="auth-pitch">
        <h1 class="auth-title auth-title-sm">
          Stop reading every job post. Read the ones that fit.
        </h1>
      </div>
    </template>
    <div class="auth-form">
      <div class="auth-form-head">
        <h2 class="auth-form-title">{{ route.meta.title }}</h2>
        <p class="auth-form-sub">This isn't available yet. It's on its way.</p>
      </div>
      <RouterLink to="/signin" class="btn btn-secondary">
        <AppIcon name="arrowLeft" />Back to sign in
      </RouterLink>
    </div>
  </AuthLayout>
</template>
