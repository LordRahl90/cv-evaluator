<script setup lang="ts">
import BrandMark from './BrandMark.vue'
</script>

<template>
  <div class="auth">
    <section class="auth-panel">
      <BrandMark tone="dark" />
      <div class="auth-panel-body">
        <slot name="panel" />
      </div>
    </section>
    <main class="auth-main">
      <slot />
    </main>
  </div>
</template>
