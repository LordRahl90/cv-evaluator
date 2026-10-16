<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AppIcon from '../components/AppIcon'

const route = useRoute()
const title = computed(() => route.meta.title ?? 'This page')
</script>

<template>
  <div class="page">
    <div class="page-stack">
      <div class="page-head">
        <div class="page-head-text">
          <h1 class="page-title">{{ title }}</h1>
          <p class="page-sub">This page is on its way.</p>
        </div>
      </div>
      <section class="card empty-state empty-state-page">
        <span class="empty-state-icon"><AppIcon name="sliders" /></span>
        <div class="empty-state-title">Coming soon</div>
        <p class="empty-state-text">
          We're still building {{ title.toLowerCase() }}. Your overview has everything that's ready
          today.
        </p>
        <RouterLink to="/" class="btn btn-secondary">
          <AppIcon name="arrowLeft" />Back to overview
        </RouterLink>
      </section>
    </div>
  </div>
</template>
