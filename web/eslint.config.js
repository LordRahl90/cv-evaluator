import pluginVue from 'eslint-plugin-vue'
import { defineConfigWithVueTs, vueTsConfigs } from '@vue/eslint-config-typescript'
import prettier from 'eslint-config-prettier'
import globals from 'globals'

export default defineConfigWithVueTs(
  { ignores: ['dist/', 'node_modules/', 'test-results/', 'playwright-report/', 'screenshots/'] },
  pluginVue.configs['flat/recommended'],
  vueTsConfigs.recommended,
  {
    languageOptions: { globals: { ...globals.browser } },
    rules: {
      // Styling lives in src/styles; components must not carry their own CSS.
      'vue/no-restricted-block': ['error', { element: 'style', message: 'Put CSS in src/styles.' }],
    },
  },
  {
    files: ['*.config.{js,ts}', 'tests/**/*.ts'],
    languageOptions: { globals: { ...globals.node } },
  },
  prettier,
)
