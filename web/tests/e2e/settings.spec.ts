// The settings page: the profile from GET /me, read-only for now.
import { expect, test, type Page } from '@playwright/test'
import { apiError, deferred, jane, mockApi, signedIn } from '../support/mockApi'

test.beforeEach(async ({ page }) => {
  await signedIn(page)
})

async function openMenuIfCollapsed(page: Page) {
  const toggle = page.getByRole('button', { name: 'Open menu' })
  if (await toggle.isVisible()) await toggle.click()
}

test('shows a loading state, then the profile', async ({ page }) => {
  const hold = deferred()
  await mockApi(page, { user: { ...jane, phone: '+1 555 0100' }, gate: hold.promise })
  await page.goto('/settings')
  await expect(page.getByRole('heading', { level: 1, name: 'Settings' })).toBeVisible()
  await expect(page.getByTestId('profile-skeleton').first()).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: 'Loading your profile' })).toBeAttached()
  hold.release()

  await expect(page.getByTestId('profile-skeleton')).toHaveCount(0)
  await expect(page.getByTestId('profile-first-name')).toHaveText('Jane')
  await expect(page.getByTestId('profile-last-name')).toHaveText('Doe')
  await expect(page.getByTestId('profile-email')).toHaveText('jane.doe@example.com')
  await expect(page.getByTestId('profile-phone')).toHaveText('+1 555 0100')
})

test('is read-only: no forms or buttons for things the API cannot do yet', async ({ page }) => {
  await mockApi(page)
  await page.goto('/settings')
  await expect(page.getByTestId('profile-email')).toHaveText('jane.doe@example.com')
  // An empty phone number is shown as such.
  await expect(page.getByTestId('profile-phone')).toHaveText('Not set')

  const main = page.getByRole('main')
  await expect(main.getByRole('textbox')).toHaveCount(0)
  await expect(main.getByRole('button')).toHaveCount(0)
  await expect(main.getByRole('button', { name: /save|change password|delete/i })).toHaveCount(0)
  await expect(page.getByTestId('settings-note')).toContainText(
    "Editing your profile, changing your password and deleting your account aren't available yet.",
  )
})

test('a failed profile load shows the error and can retry', async ({ page }) => {
  let fail = true
  await page.route('**/api/me', (route) =>
    fail
      ? route.fulfill({
          status: 500,
          json: apiError('api_error', 'internal_error', 'Something went wrong on our side.'),
        })
      : route.fulfill({ status: 200, json: jane }),
  )
  await page.goto('/settings')
  await expect(page.getByRole('main').getByRole('alert')).toContainText(
    "We couldn't load your profile.",
  )
  fail = false
  await page.getByRole('button', { name: 'Try again' }).click()
  await expect(page.getByTestId('profile-first-name')).toHaveText('Jane')
})

test('the sidebar and the account menu both lead here', async ({ page }) => {
  await mockApi(page)
  await page.goto('/')
  await openMenuIfCollapsed(page)
  await page.getByRole('link', { name: 'Settings' }).click()
  await expect(page).toHaveURL('/settings')
  await expect(page).toHaveTitle('Settings · CV Evaluator')

  await page.goto('/')
  await page.getByRole('button', { name: 'Account menu' }).click()
  await page.getByRole('menuitem', { name: 'Settings' }).click()
  await expect(page).toHaveURL('/settings')
  await expect(page.getByRole('menu', { name: 'Account' })).toBeHidden()
  await expect(page.getByTestId('profile-email')).toHaveText('jane.doe@example.com')
})
