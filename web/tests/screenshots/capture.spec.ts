// Captures the screens shown in pull requests. Run with `npm run screenshots`.
import { expect, test, type Page } from '@playwright/test'
import path from 'node:path'
import { apiError, deferred, mockApi, sampleCV, sampleSearches, signedIn } from '../support/mockApi'
import { mockApiKeys, sampleApiKeys } from '../support/mockApi'

const OUT = process.env.SCREENSHOT_DIR ?? 'screenshots'
const desktop = { width: 1280, height: 860 }
const phone = { width: 375, height: 812 }

async function shoot(page: Page, name: string, fullPage = true) {
  await page.mouse.move(0, 0)
  await page.evaluate(() => document.fonts.ready)
  await page.screenshot({ path: path.join(OUT, `${name}.png`), fullPage, animations: 'disabled' })
}

test.use({ deviceScaleFactor: 2 })

test('sign in', async ({ page }) => {
  await page.setViewportSize(desktop)
  await page.goto('/signin')
  await page.getByLabel('Email').fill('jane.doe@example.com')
  await page.getByLabel('Password').fill('password123')
  await shoot(page, 'signin')
})

test('sign up with an error', async ({ page }) => {
  await page.setViewportSize(desktop)
  await page.route('**/api/auth/signup', (route) =>
    route.fulfill({
      status: 409,
      json: apiError(
        'invalid_request_error',
        'email_already_used',
        'An account with this email already exists.',
        'email',
      ),
    }),
  )
  await page.goto('/signup')
  await page.getByLabel('First name').fill('Jane')
  await page.getByLabel('Last name').fill('Doe')
  await page.getByLabel('Email').fill('jane.doe@example.com')
  await page.getByLabel('Password').fill('password123')
  await page.getByRole('button', { name: 'Create account' }).click()
  await expect(page.getByText('An account with this email already exists.')).toBeVisible()
  await shoot(page, 'signup-error')
})

test('dashboard loading', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  const hold = deferred()
  await mockApi(page, { gate: hold.promise })
  await page.goto('/')
  await expect(page.getByTestId('overview-skeleton')).toBeVisible()
  await shoot(page, 'dashboard-loading', false)
  hold.release()
})

test('dashboard, new account', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 940 })
  await signedIn(page)
  await mockApi(page)
  await page.goto('/')
  await expect(page.getByText('1 of 3 done')).toBeVisible()
  await shoot(page, 'dashboard-new-account')
})

test('dashboard with a CV and searches', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 940 })
  await signedIn(page)
  await mockApi(page, { cvs: [sampleCV], searches: sampleSearches() })
  await page.goto('/')
  await expect(page.getByTestId('matches-empty')).toBeVisible()
  await shoot(page, 'dashboard-active')
  await page.getByRole('button', { name: 'Account menu' }).click()
  await expect(page.getByRole('menu', { name: 'Account' })).toBeVisible()
  await shoot(page, 'dashboard-user-menu', false)
})

test('mobile sign in', async ({ page }) => {
  await page.setViewportSize(phone)
  await page.goto('/signin')
  await shoot(page, 'mobile-signin')
})

test('mobile dashboard', async ({ page }) => {
  await page.setViewportSize(phone)
  await signedIn(page)
  await mockApi(page)
  await page.goto('/')
  await expect(page.getByText('1 of 3 done')).toBeVisible()
  await shoot(page, 'mobile-dashboard')
  await page.getByRole('button', { name: 'Open menu' }).click()
  await shoot(page, 'mobile-dashboard-menu', false)
  await page.getByRole('button', { name: 'Close menu' }).click()
  await page.getByRole('button', { name: 'Account menu' }).click()
  await expect(page.getByRole('menu', { name: 'Account' })).toBeVisible()
  await shoot(page, 'mobile-user-menu', false)
})

// ---- API keys and settings ------------------------------------------------

test('api keys, empty', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  await mockApi(page)
  await mockApiKeys(page)
  await page.goto('/api-keys')
  await expect(page.getByRole('heading', { name: 'No API keys yet' })).toBeVisible()
  await shoot(page, 'api-keys-empty')
})

test('api keys, loading', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  await mockApi(page)
  const hold = deferred()
  await mockApiKeys(page, { gate: hold.promise })
  await page.goto('/api-keys')
  await expect(page.getByTestId('api-keys-skeleton')).toBeVisible()
  await shoot(page, 'api-keys-loading', false)
  hold.release()
})

test('api keys, create dialog, new secret and list', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.setViewportSize(desktop)
  await signedIn(page)
  await mockApi(page)
  await mockApiKeys(page, { keys: sampleApiKeys() })
  await page.goto('/api-keys')
  await expect(page.getByRole('row', { name: /Zapier/ })).toBeVisible()
  await shoot(page, 'api-keys-list')

  await page.getByRole('button', { name: 'Create API key' }).click()
  const dialog = page.getByRole('dialog', { name: 'Create API key' })
  await dialog.getByLabel('Name').fill('Nightly export script')
  await shoot(page, 'api-keys-create-dialog', false)

  await dialog.getByRole('button', { name: 'Create key' }).click()
  await expect(page.getByTestId('new-key-banner')).toBeVisible()
  await page.getByRole('button', { name: 'Copy' }).click()
  await expect(page.getByRole('button', { name: 'Copied' })).toBeVisible()
  await shoot(page, 'api-keys-new-secret')

  await page.getByRole('button', { name: 'Revoke Zapier' }).click()
  await expect(page.getByRole('dialog', { name: 'Revoke "Zapier"?' })).toBeVisible()
  await shoot(page, 'api-keys-revoke-confirm', false)
})

test('settings', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  await mockApi(page, {
    user: {
      object: 'user',
      id: '01JA2B3C4D5E6F7G8H9J0K1M2N',
      first_name: 'Jane',
      last_name: 'Doe',
      email: 'jane.doe@example.com',
      phone: '+1 555 0100',
    },
  })
  await page.goto('/settings')
  await expect(page.getByTestId('profile-email')).toBeVisible()
  await shoot(page, 'settings')
  await page.getByRole('button', { name: 'Account menu' }).click()
  await expect(page.getByRole('menu', { name: 'Account' })).toBeVisible()
  await shoot(page, 'settings-user-menu', false)
})

test('settings, loading', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  const hold = deferred()
  await mockApi(page, { gate: hold.promise })
  await page.goto('/settings')
  await expect(page.getByTestId('profile-skeleton').first()).toBeVisible()
  await shoot(page, 'settings-loading', false)
  hold.release()
})

test('mobile api keys and settings', async ({ page }) => {
  await page.setViewportSize(phone)
  await signedIn(page)
  await mockApi(page)
  await mockApiKeys(page)
  await page.goto('/api-keys')
  await expect(page.getByRole('heading', { name: 'No API keys yet' })).toBeVisible()
  await shoot(page, 'mobile-api-keys-empty')

  await page.getByRole('button', { name: 'Create API key' }).click()
  await page
    .getByRole('dialog', { name: 'Create API key' })
    .getByLabel('Name')
    .fill('Nightly export script')
  await shoot(page, 'mobile-api-keys-create-dialog', false)
  await page.getByRole('button', { name: 'Create key' }).click()
  await expect(page.getByTestId('new-key-banner')).toBeVisible()
  await shoot(page, 'mobile-api-keys-new-secret')

  await page.goto('/settings')
  await expect(page.getByTestId('profile-email')).toBeVisible()
  await shoot(page, 'mobile-settings')
})
