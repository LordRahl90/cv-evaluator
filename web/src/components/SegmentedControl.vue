<script setup lang="ts">
// A segmented choice (e.g. Hourly / Daily / Weekly) built from real radio
// buttons, so arrow keys, labels and form semantics come from the browser.
// Put it inside a <fieldset> with a <legend> naming the choice.
withDefaults(
  defineProps<{
    name: string
    options: readonly { value: string; label: string }[]
    invalid?: boolean
    describedBy?: string
  }>(),
  { invalid: false, describedBy: undefined },
)

const model = defineModel<string>({ required: true })
</script>

<template>
  <div class="segmented" :class="{ 'has-error': invalid }">
    <label
      v-for="option in options"
      :key="option.value"
      class="segmented-option"
      :class="{ 'is-on': model === option.value }"
    >
      <input
        v-model="model"
        type="radio"
        class="segmented-input"
        :name="name"
        :value="option.value"
        :aria-invalid="invalid ? 'true' : undefined"
        :aria-describedby="describedBy"
      />
      {{ option.label }}
    </label>
  </div>
</template>
