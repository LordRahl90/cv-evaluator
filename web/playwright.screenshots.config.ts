import { defineConfig } from '@playwright/test'
import base from './playwright.config'

// `npm run screenshots` — captures PR screenshots into ./screenshots
// (or $SCREENSHOT_DIR). Not part of `npm run test:e2e`.
export default defineConfig({
  ...base,
  testDir: './tests/screenshots',
  projects: [{ name: 'screenshots' }],
  retries: 0,
  reporter: [['list']],
})
