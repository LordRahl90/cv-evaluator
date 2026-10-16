<script setup lang="ts">
// An on/off switch: a real checkbox with role="switch" inside its label.
// `label` is the accessible name (e.g. "Run Senior Go on schedule"); the
// visible "On"/"Off" text is decoration, the state comes from aria-checked.
withDefaults(defineProps<{ label: string; disabled?: boolean; busy?: boolean }>(), {
  disabled: false,
  busy: false,
})

const model = defineModel<boolean>({ required: true })
</script>

<template>
  <label class="switch" :class="{ 'is-on': model, 'is-disabled': disabled }">
    <input
      v-model="model"
      type="checkbox"
      role="switch"
      class="switch-input"
      :disabled="disabled"
      :aria-checked="model ? 'true' : 'false'"
      :aria-busy="busy ? 'true' : undefined"
    />
    <span class="switch-track" aria-hidden="true"><span class="switch-knob" /></span>
    <span class="sr-only">{{ label }}</span>
    <span class="switch-state" aria-hidden="true">{{ model ? 'On' : 'Off' }}</span>
  </label>
</template>
