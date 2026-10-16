<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    id: string
    label: string
    type?: string
    autocomplete?: string
    hint?: string
    error?: string
    optional?: boolean
    required?: boolean
    placeholder?: string
  }>(),
  {
    type: 'text',
    autocomplete: undefined,
    hint: '',
    error: '',
    optional: false,
    required: false,
    placeholder: undefined,
  },
)

const model = defineModel<string>({ default: '' })

const describedBy = computed(
  () =>
    [props.error && `${props.id}-error`, props.hint && `${props.id}-hint`]
      .filter(Boolean)
      .join(' ') || undefined,
)
</script>

<template>
  <div class="field" :class="{ 'has-error': error }">
    <div class="field-label-row">
      <label :for="id" class="field-label">
        {{ label }} <span v-if="optional" class="field-optional">(optional)</span>
      </label>
      <slot name="label-aside" />
    </div>
    <input
      :id="id"
      v-model="model"
      class="input"
      :type="type"
      :name="id"
      :autocomplete="autocomplete"
      :required="required"
      :placeholder="placeholder"
      :aria-invalid="error ? 'true' : undefined"
      :aria-describedby="describedBy"
    />
    <p v-if="error" :id="`${id}-error`" class="field-error">{{ error }}</p>
    <p v-if="hint" :id="`${id}-hint`" class="field-hint">{{ hint }}</p>
  </div>
</template>
