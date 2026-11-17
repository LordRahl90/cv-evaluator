<script setup lang="ts">
// The placeholder result shown while a quick match is scored (5-30 s):
// what is being scored, the seconds so far and skeleton lines.
defineProps<{ against: string; elapsed: number }>()
</script>

<template>
  <section class="card qm-scoring" role="status" aria-live="polite" data-testid="qm-scoring">
    <div class="qm-scoring-head">
      <span class="skeleton qm-ring-skeleton" aria-hidden="true" />
      <div class="qm-scoring-text">
        <div class="qm-scoring-title">Scoring this job against {{ against }}…</div>
        <div class="qm-scoring-sub">
          Reading the requirements and comparing them with your experience. This usually takes 5 to
          30 seconds<template v-if="elapsed >= 1"> ({{ elapsed }} s so far)</template>.
        </div>
      </div>
    </div>
    <div class="qm-scoring-body" aria-hidden="true">
      <span class="skeleton skeleton-line" />
      <span class="skeleton skeleton-line skeleton-w-56" />
      <span v-for="n in 4" :key="n" class="skeleton skeleton-bar" />
    </div>
  </section>
</template>
