// The API keys page: empty state, create (secret shown once), list, revoke.
import { expect, test, type Page } from '@playwright/test'
import {
  FAKE_SECRET,
  apiError,
  deferred,
  mockApi,
  mockApiKeys,
  sampleApiKeys,
  signedIn,
} from '../support/mockApi'

const createButton = (page: Page) => page.getByRole('button', { name: 'Create API key' })
const createDialog = (page: Page) => page.getByRole('dialog', { name: 'Create API key' })
const banner = (page: Page) => page.getByTestId('new-key-banner')

test.beforeEach(async ({ page }) => {
  await signedIn(page)
  await mockApi(page)
})

async function openMenuIfCollapsed(page: Page) {
  const toggle = page.getByRole('button', { name: 'Open menu' })
  if (await toggle.isVisible()) await toggle.click()
}

test('the sidebar item opens the page', async ({ page }) => {
  await mockApiKeys(page)
  await page.goto('/')
  await openMenuIfCollapsed(page)
  await page.getByRole('link', { name: 'API keys' }).click()
  await expect(page).toHaveURL('/api-keys')
  await expect(page.getByRole('heading', { level: 1, name: 'API keys' })).toBeVisible()
  await expect(page).toHaveTitle('API keys · CV Evaluator')
  await openMenuIfCollapsed(page)
  await expect(page.getByRole('link', { name: 'API keys' })).toHaveAttribute('aria-current', 'page')
})

test('loading skeleton, then the empty state with a curl example', async ({ page }) => {
  const hold = deferred()
  await mockApiKeys(page, { gate: hold.promise })
  await page.goto('/api-keys')
  await expect(page.getByTestId('api-keys-skeleton')).toBeVisible()
  hold.release()

  await expect(page.getByTestId('api-keys-skeleton')).toBeHidden()
  await expect(page.getByRole('heading', { name: 'No API keys yet' })).toBeVisible()
  await expect(
    page.getByText('Use the API to upload CVs, manage searches and pull matches'),
  ).toBeVisible()
  const example = page.getByRole('region', { name: "How you'll use it" })
  await expect(example).toContainText('/api/job-searches')
  await expect(example).toContainText('Authorization: Bearer <your API key>')
  // No docs page exists on the server, so no link to one.
  await expect(page.getByRole('link', { name: /docs/i })).toHaveCount(0)
  await expect(page.getByRole('table')).toHaveCount(0)
})

test('lists keys with prefix, created and last used; never a secret', async ({ page }) => {
  await mockApiKeys(page, { keys: sampleApiKeys() })
  await page.goto('/api-keys')

  await expect(page.getByTestId('api-keys-count')).toHaveText('2 active keys')
  const rows = page.getByRole('row')
  await expect(rows).toHaveCount(3)
  const zapier = page.getByRole('row', { name: /Zapier/ })
  await expect(zapier).toContainText('sk_live_Hq2v…')
  await expect(zapier).toContainText('2 Oct 2026')
  await expect(zapier).toContainText('12 min ago')
  const laptop = page.getByRole('row', { name: /Laptop CLI/ })
  await expect(laptop).toContainText('18 Sep 2026')
  await expect(laptop).toContainText('9 days ago')
  await expect(page.getByRole('button', { name: 'Revoke Zapier' })).toBeVisible()
  await expect(banner(page)).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'No API keys yet' })).toHaveCount(0)
})

test('creates a key: secret shown once, copied, and cleared on Done', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  const api = await mockApiKeys(page)
  await page.goto('/api-keys')

  await createButton(page).click()
  const dialog = createDialog(page)
  await expect(dialog).toBeVisible()
  await expect(dialog.getByLabel('Name')).toBeFocused()

  // Client-side check first.
  await dialog.getByRole('button', { name: 'Create key' }).click()
  await expect(dialog.getByText('Give the key a name.')).toBeVisible()
  expect(api.created).toHaveLength(0)

  await dialog.getByLabel('Name').fill('  Nightly export script  ')
  await dialog.getByLabel('Name').press('Enter')
  await expect(dialog).toBeHidden()
  expect(api.created).toEqual([{ name: 'Nightly export script' }])

  await expect(banner(page)).toContainText('"Nightly export script" created. Copy the key now.')
  await expect(banner(page)).toContainText('we only show it once')
  await expect(page.getByTestId('new-key-secret')).toHaveText(FAKE_SECRET)
  await expect(page.getByRole('button', { name: 'Copy' })).toBeFocused()

  // The list shows the new key by prefix only.
  const row = page.getByRole('row', { name: /Nightly export script/ })
  await expect(row).toContainText('sk_live_EXAM…')
  await expect(row).toContainText('Just now')
  await expect(row).toContainText('Never')
  await expect(row).not.toContainText(FAKE_SECRET)
  await expect(page.getByTestId('api-keys-count')).toHaveText('1 active key')

  await page.getByRole('button', { name: 'Copy' }).click()
  await expect(page.getByRole('button', { name: 'Copied' })).toBeVisible()
  await expect(banner(page)).toContainText('Key copied to the clipboard.')
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(FAKE_SECRET)

  await page.getByRole('button', { name: 'Done' }).click()
  await expect(banner(page)).toHaveCount(0)
  await expect(page.getByText(FAKE_SECRET)).toHaveCount(0)
  expect(await page.content()).not.toContain(FAKE_SECRET)
  await expect(createButton(page)).toBeFocused()

  // Coming back to the page never shows the secret again.
  await page.reload()
  await expect(page.getByRole('row', { name: /Nightly export script/ })).toBeVisible()
  await expect(banner(page)).toHaveCount(0)
})

test('copy falls back to selecting the key when the clipboard is unavailable', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: () => Promise.reject(new Error('denied')) },
    })
  })
  await mockApiKeys(page)
  await page.goto('/api-keys')
  await createButton(page).click()
  await createDialog(page).getByLabel('Name').fill('CI')
  await createDialog(page).getByRole('button', { name: 'Create key' }).click()

  await page.getByRole('button', { name: 'Copy' }).click()
  await expect(banner(page)).toContainText("Couldn't copy automatically. The key is selected")
  expect(await page.evaluate(() => window.getSelection()?.toString())).toBe(FAKE_SECRET)
})

test('the create dialog closes on Escape and on Cancel, and keeps focus inside', async ({
  page,
}) => {
  const api = await mockApiKeys(page, { keys: sampleApiKeys() })
  await page.goto('/api-keys')

  await createButton(page).click()
  const dialog = createDialog(page)
  await expect(dialog).toBeVisible()
  // Tab cycles through the name field, Cancel and Create key, and wraps.
  await page.keyboard.press('Tab')
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(dialog.getByRole('button', { name: 'Create key' })).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(dialog.getByLabel('Name')).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(dialog.getByRole('button', { name: 'Create key' })).toBeFocused()

  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(createButton(page)).toBeFocused()

  await createButton(page).click()
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toBeHidden()
  expect(api.created).toHaveLength(0)
})

test('create shows the busy state and API errors inline', async ({ page }) => {
  const hold = deferred()
  await mockApiKeys(page, {
    writeGate: hold.promise,
    createError: {
      status: 400,
      body: apiError(
        'invalid_request_error',
        'parameter_invalid',
        'The name parameter must be at most 100 characters long.',
        'name',
      ),
    },
  })
  await page.goto('/api-keys')
  await createButton(page).click()
  const dialog = createDialog(page)
  await dialog.getByLabel('Name').fill('Some name')
  await dialog.getByRole('button', { name: 'Create key' }).click()
  await expect(dialog.getByRole('button', { name: 'Creating…' })).toBeDisabled()
  hold.release()

  await expect(
    dialog.getByText('The name parameter must be at most 100 characters long.'),
  ).toBeVisible()
  await expect(dialog.getByLabel('Name')).toHaveAttribute('aria-invalid', 'true')
  await expect(banner(page)).toHaveCount(0)
})

test('a server error on create shows in the dialog', async ({ page }) => {
  await mockApiKeys(page, {
    createError: {
      status: 500,
      body: apiError('api_error', 'internal_error', 'Something went wrong on our side.'),
    },
  })
  await page.goto('/api-keys')
  await createButton(page).click()
  await createDialog(page).getByLabel('Name').fill('CI')
  await createDialog(page).getByRole('button', { name: 'Create key' }).click()
  await expect(createDialog(page).getByRole('alert')).toContainText(
    'Something went wrong on our side.',
  )
})

test('revoke asks first: cancel keeps the key, confirm removes it', async ({ page }) => {
  const api = await mockApiKeys(page, { keys: sampleApiKeys() })
  await page.goto('/api-keys')

  await page.getByRole('button', { name: 'Revoke Zapier' }).click()
  const dialog = page.getByRole('dialog', { name: 'Revoke "Zapier"?' })
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('stop working immediately')
  // The safe choice has focus.
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused()
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toBeHidden()
  expect(api.revoked).toHaveLength(0)
  await expect(page.getByRole('row', { name: /Zapier/ })).toBeVisible()

  await page.getByRole('button', { name: 'Revoke Zapier' }).click()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  expect(api.revoked).toHaveLength(0)

  await page.getByRole('button', { name: 'Revoke Zapier' }).click()
  await dialog.getByRole('button', { name: 'Revoke key' }).click()
  await expect(dialog).toBeHidden()
  expect(api.revoked).toEqual(['01JA2B3C4D5E6F7G8H9J0KAK01'])
  await expect(page.getByRole('row', { name: /Zapier/ })).toHaveCount(0)
  await expect(page.getByTestId('api-keys-count')).toHaveText('1 active key')
  await expect(page.getByRole('status').filter({ hasText: 'Revoked "Zapier".' })).toBeAttached()

  // Revoking the last key brings back the empty state.
  await page.getByRole('button', { name: 'Revoke Laptop CLI' }).click()
  await page
    .getByRole('dialog', { name: 'Revoke "Laptop CLI"?' })
    .getByRole('button', { name: 'Revoke key' })
    .click()
  await expect(page.getByRole('heading', { name: 'No API keys yet' })).toBeVisible()
})

test('a failed revoke keeps the key and shows the error in the dialog', async ({ page }) => {
  const hold = deferred()
  await mockApiKeys(page, {
    keys: sampleApiKeys(),
    writeGate: hold.promise,
    revokeError: {
      status: 500,
      body: apiError('api_error', 'internal_error', 'Something went wrong on our side.'),
    },
  })
  await page.goto('/api-keys')
  await page.getByRole('button', { name: 'Revoke Zapier' }).click()
  const dialog = page.getByRole('dialog', { name: 'Revoke "Zapier"?' })
  await dialog.getByRole('button', { name: 'Revoke key' }).click()
  await expect(dialog.getByRole('button', { name: 'Revoking…' })).toBeDisabled()
  // Escape is ignored while the request runs.
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  hold.release()

  await expect(dialog.getByRole('alert')).toContainText('Something went wrong on our side.')
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(page.getByRole('row', { name: /Zapier/ })).toBeVisible()
})

test('a failed load shows the error and can retry', async ({ page }) => {
  let fail = true
  await page.route('**/api/api-keys', (route) =>
    fail
      ? route.fulfill({
          status: 500,
          json: apiError('api_error', 'internal_error', 'Something went wrong on our side.'),
        })
      : route.fulfill({ status: 200, json: { object: 'list', data: sampleApiKeys() } }),
  )
  await page.goto('/api-keys')
  await expect(page.getByRole('alert')).toContainText("We couldn't load your API keys.")
  fail = false
  await page.getByRole('button', { name: 'Try again' }).click()
  await expect(page.getByRole('row', { name: /Zapier/ })).toBeVisible()
})
