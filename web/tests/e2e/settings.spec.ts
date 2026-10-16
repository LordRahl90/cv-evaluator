// The settings page: edit the profile, change the password, download your
// data and delete the account. The API is mocked by mockApi + mockAccount.
import { readFile } from 'node:fs/promises'
import { expect, test, type Page } from '@playwright/test'
import {
  ACCOUNT_PASSWORD,
  RENEWED_TOKEN,
  TOKEN_KEY,
  apiError,
  deferred,
  jane,
  mockAccount,
  mockApi,
  signedIn,
} from '../support/mockApi'

test.beforeEach(async ({ page }) => {
  await signedIn(page)
})

async function openMenuIfCollapsed(page: Page) {
  const toggle = page.getByRole('button', { name: 'Open menu' })
  if (await toggle.isVisible()) await toggle.click()
}

async function openSettings(page: Page) {
  await page.goto('/settings')
  await expect(page.getByLabel('Email')).toHaveValue(jane.email)
}

const storedToken = (page: Page) =>
  page.evaluate((key) => window.localStorage.getItem(key), TOKEN_KEY)

// ---- Profile ----------------------------------------------------------------

test('shows a loading state, then the profile in an editable form', async ({ page }) => {
  const hold = deferred()
  await mockApi(page, { user: { ...jane, phone: '+1 555 0100' }, gate: hold.promise })
  await page.goto('/settings')
  await expect(page.getByRole('heading', { level: 1, name: 'Settings' })).toBeVisible()
  await expect(page.getByTestId('profile-skeleton').first()).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: 'Loading your profile' })).toBeAttached()
  hold.release()

  await expect(page.getByTestId('profile-skeleton')).toHaveCount(0)
  await expect(page.getByLabel('First name')).toHaveValue('Jane')
  await expect(page.getByLabel('Last name')).toHaveValue('Doe')
  await expect(page.getByLabel('Email')).toHaveValue('jane.doe@example.com')
  await expect(page.getByLabel('Phone')).toHaveValue('+1 555 0100')
  await expect(page.getByRole('button', { name: 'Save profile' })).toBeVisible()
})

test('saves only what changed and updates the name everywhere', async ({ page }) => {
  await mockApi(page)
  const account = await mockAccount(page)
  await openSettings(page)

  await page.getByLabel('First name').fill('  Janet ')
  await page.getByLabel('Email').fill('Janet.Doe@Example.com')
  await page.getByRole('button', { name: 'Save profile' }).click()

  await expect(page.getByRole('status').filter({ hasText: 'Profile saved.' })).toBeVisible()
  expect(account.updates).toEqual([{ first_name: 'Janet', email: 'Janet.Doe@Example.com' }])
  // The form shows what the API stored (trimmed, email lowercased).
  await expect(page.getByLabel('First name')).toHaveValue('Janet')
  await expect(page.getByLabel('Email')).toHaveValue('janet.doe@example.com')

  // The account menu shows the new name and email straight away.
  await page.getByRole('button', { name: 'Account menu' }).click()
  const header = page.getByRole('menu', { name: 'Account' }).getByTestId('user-menu-header')
  await expect(header).toContainText('Janet Doe')
  await expect(header).toContainText('janet.doe@example.com')
  await page.keyboard.press('Escape')

  // Editing again clears the saved message.
  await page.getByLabel('Last name').fill('Smith')
  await expect(page.getByText('Profile saved.')).toHaveCount(0)
})

test('maps API errors to the field they are about', async ({ page }) => {
  await mockApi(page)
  await mockAccount(page, {
    updateError: {
      status: 409,
      body: apiError(
        'invalid_request_error',
        'email_already_used',
        'An account with this email already exists.',
        'email',
      ),
    },
  })
  await openSettings(page)

  await page.getByLabel('Email').fill('taken@example.com')
  await page.getByRole('button', { name: 'Save profile' }).click()

  const email = page.getByLabel('Email')
  await expect(email).toHaveAttribute('aria-invalid', 'true')
  await expect(page.locator('#email-error')).toHaveText(
    'An account with this email already exists.',
  )
  await expect(email).toHaveAttribute('aria-describedby', 'email-error')
  await expect(page.getByText('Profile saved.')).toHaveCount(0)
})

test('a phone conflict lands on the phone field, other errors on the form', async ({ page }) => {
  await mockApi(page)
  let body: unknown = apiError(
    'invalid_request_error',
    'phone_already_used',
    'An account with this phone number already exists.',
    'phone',
  )
  await mockAccount(page)
  await page.route('**/api/me', (route) =>
    route.request().method() === 'PATCH'
      ? route.fulfill({ status: body ? 409 : 500, json: body })
      : route.fallback(),
  )
  await openSettings(page)

  await page.getByLabel('Phone').fill('+45 1234')
  await page.getByRole('button', { name: 'Save profile' }).click()
  await expect(page.locator('#phone-error')).toHaveText(
    'An account with this phone number already exists.',
  )

  body = apiError('api_error', 'internal_error', 'Something went wrong on our side.')
  await page.route('**/api/me', (route) =>
    route.request().method() === 'PATCH'
      ? route.fulfill({ status: 500, json: body })
      : route.fallback(),
  )
  await page.getByRole('button', { name: 'Save profile' }).click()
  await expect(page.getByTestId('profile-form').getByRole('alert')).toHaveText(
    'Something went wrong on our side.',
  )
  await expect(page.locator('#phone-error')).toHaveCount(0)
})

test('checks the email before sending, and skips saving when nothing changed', async ({ page }) => {
  await mockApi(page)
  const account = await mockAccount(page)
  await openSettings(page)

  await page.getByRole('button', { name: 'Save profile' }).click()
  await expect(page.getByText('Your profile is already up to date.')).toBeVisible()

  await page.getByLabel('Email').fill('')
  await page.getByRole('button', { name: 'Save profile' }).click()
  await expect(page.locator('#email-error')).toHaveText('Enter your email address.')

  await page.getByLabel('Email').fill('not-an-email')
  await page.getByRole('button', { name: 'Save profile' }).click()
  await expect(page.locator('#email-error')).toContainText('Enter a valid email address')
  expect(account.updates).toEqual([])
})

test('shows a busy button while saving', async ({ page }) => {
  await mockApi(page)
  const hold = deferred()
  await mockAccount(page, { gate: hold.promise })
  await openSettings(page)

  await page.getByLabel('Last name').fill('Smith')
  await page.getByRole('button', { name: 'Save profile' }).click()
  const busy = page.getByRole('button', { name: 'Saving…' })
  await expect(busy).toBeDisabled()
  await expect(busy).toHaveAttribute('aria-busy', 'true')
  hold.release()
  await expect(page.getByText('Profile saved.')).toBeVisible()
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
  await expect(page.getByLabel('First name')).toHaveValue('Jane')
})

// ---- Password -----------------------------------------------------------------

test('checks the new password before sending', async ({ page }) => {
  await mockApi(page)
  const account = await mockAccount(page)
  await openSettings(page)
  const form = page.getByTestId('password-form')
  const submit = form.getByRole('button', { name: 'Change password' })

  await submit.click()
  await expect(page.locator('#current-password-error')).toHaveText('Enter your current password.')
  await expect(page.locator('#new-password-error')).toHaveText('Choose a new password.')

  await form.getByLabel('Current password').fill(ACCOUNT_PASSWORD)
  await form.getByLabel('New password', { exact: true }).fill('short')
  await submit.click()
  await expect(page.locator('#new-password-error')).toHaveText('Use at least 8 characters.')
  await expect(page.locator('#current-password-error')).toHaveCount(0)

  await form.getByLabel('New password', { exact: true }).fill('a-long-new-password')
  await form.getByLabel('Confirm new password').fill('a-different-password')
  await submit.click()
  await expect(page.locator('#confirm-password-error')).toHaveText("The passwords don't match.")
  expect(account.passwordChanges).toEqual([])
})

test('a wrong current password is shown on that field and keeps you signed in', async ({
  page,
}) => {
  await mockApi(page)
  const account = await mockAccount(page)
  await openSettings(page)
  const form = page.getByTestId('password-form')

  await form.getByLabel('Current password').fill('not-my-password')
  await form.getByLabel('New password', { exact: true }).fill('a-long-new-password')
  await form.getByLabel('Confirm new password').fill('a-long-new-password')
  await form.getByRole('button', { name: 'Change password' }).click()

  await expect(page.locator('#current-password-error')).toHaveText(
    "That isn't your current password.",
  )
  await expect(form.getByLabel('Current password')).toHaveAttribute('aria-invalid', 'true')
  await expect(page).toHaveURL('/settings')
  expect(await storedToken(page)).toBe('test-token')
  expect(account.passwordChanges).toHaveLength(1)
})

test('changing the password stores the new session so you stay signed in', async ({ page }) => {
  await mockApi(page)
  const account = await mockAccount(page)
  await openSettings(page)
  const form = page.getByTestId('password-form')

  await form.getByLabel('Current password').fill(ACCOUNT_PASSWORD)
  await form.getByLabel('New password', { exact: true }).fill('a-long-new-password')
  await form.getByLabel('Confirm new password').fill('a-long-new-password')
  await form.getByRole('button', { name: 'Change password' }).click()

  await expect(form.getByRole('status')).toContainText('Password changed.')
  expect(account.passwordChanges).toEqual([
    { current_password: ACCOUNT_PASSWORD, new_password: 'a-long-new-password' },
  ])
  expect(await storedToken(page)).toBe(RENEWED_TOKEN)
  await expect(form.getByLabel('Current password')).toHaveValue('')
  await expect(form.getByLabel('New password', { exact: true })).toHaveValue('')

  // The old token is now rejected by the API; the app uses the new one.
  await page.reload()
  await expect(page.getByLabel('Email')).toHaveValue(jane.email)
  await expect(page).toHaveURL('/settings')
  expect(account.authHeaders.at(-1)).toBe(`Bearer ${RENEWED_TOKEN}`)
})

// ---- Download my data ---------------------------------------------------------

test('downloads the data export as a JSON file', async ({ page }) => {
  await mockApi(page)
  const hold = deferred()
  const account = await mockAccount(page, { gate: hold.promise })
  await openSettings(page)

  const downloading = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Download my data first' }).click()
  await expect(page.getByRole('button', { name: 'Preparing your data…' })).toBeDisabled()
  hold.release()

  const download = await downloading
  expect(download.suggestedFilename()).toBe('cv-evaluator-export.json')
  const saved = JSON.parse(await readFile((await download.path())!, 'utf8'))
  expect(saved.object).toBe('account_export')
  expect(saved.profile.email).toBe(jane.email)
  expect(account.exports).toBe(1)
  await expect(
    page.getByText('Your data was downloaded as cv-evaluator-export.json.'),
  ).toBeVisible()
})

test('a failed export says so', async ({ page }) => {
  await mockApi(page)
  await mockAccount(page, {
    exportError: {
      status: 500,
      body: apiError('api_error', 'internal_error', 'Something went wrong on our side.'),
    },
  })
  await openSettings(page)
  await page.getByRole('button', { name: 'Download my data first' }).click()
  await expect(page.getByRole('alert')).toHaveText(
    "We couldn't prepare your data. Something went wrong on our side.",
  )
})

// ---- Delete account -----------------------------------------------------------

test('the delete dialog explains what goes, traps focus and closes on Escape', async ({ page }) => {
  await mockApi(page)
  await mockAccount(page)
  await openSettings(page)

  const opener = page.getByRole('button', { name: 'Delete account' })
  await opener.click()
  const dialog = page.getByRole('dialog', { name: 'Delete your account?' })
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('Every CV you uploaded')
  await expect(dialog).toContainText('Your API keys')
  await expect(dialog).toContainText("It can't be undone.")
  await expect(dialog.getByLabel('Your password')).toBeFocused()

  // Tab cycles inside the dialog.
  const focusable = ['Your password', 'Type DELETE to confirm']
  for (const label of focusable.slice(1)) {
    await page.keyboard.press('Tab')
    await expect(dialog.getByLabel(label)).toBeFocused()
  }
  await page.keyboard.press('Tab')
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(dialog.getByRole('button', { name: 'Delete account' })).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(dialog.getByLabel('Your password')).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(dialog.getByRole('button', { name: 'Delete account' })).toBeFocused()

  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(opener).toBeFocused()

  // Cancel closes it too, and it opens empty again.
  await opener.click()
  await dialog.getByLabel('Your password').fill('something')
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toBeHidden()
  await opener.click()
  await expect(dialog.getByLabel('Your password')).toHaveValue('')
})

test('deleting needs the password and the confirmation word', async ({ page }) => {
  await mockApi(page)
  const account = await mockAccount(page)
  await openSettings(page)
  await page.getByRole('button', { name: 'Delete account' }).click()
  const dialog = page.getByRole('dialog', { name: 'Delete your account?' })
  const confirm = dialog.getByRole('button', { name: 'Delete account' })

  await confirm.click()
  await expect(page.locator('#delete-password-error')).toHaveText('Enter your password.')
  await expect(page.locator('#delete-confirm-error')).toHaveText('Type DELETE to confirm.')

  await dialog.getByLabel('Your password').fill(ACCOUNT_PASSWORD)
  await dialog.getByLabel('Type DELETE to confirm').fill('delete')
  await confirm.click()
  await expect(page.locator('#delete-confirm-error')).toHaveText('Type DELETE to confirm.')
  expect(account.deletions).toEqual([])

  // A wrong password comes back from the API on the password field.
  await dialog.getByLabel('Your password').fill('wrong-password')
  await dialog.getByLabel('Type DELETE to confirm').fill('DELETE')
  await confirm.click()
  await expect(page.locator('#delete-password-error')).toHaveText("That password isn't right.")
  await expect(dialog).toBeVisible()
  await expect(page).toHaveURL('/settings')
  expect(account.deleted).toBe(false)
})

test('a failed deletion keeps the dialog open with the error', async ({ page }) => {
  await mockApi(page)
  await mockAccount(page, {
    deleteError: {
      status: 500,
      body: apiError('api_error', 'internal_error', 'Something went wrong on our side.'),
    },
  })
  await openSettings(page)
  await page.getByRole('button', { name: 'Delete account' }).click()
  const dialog = page.getByRole('dialog', { name: 'Delete your account?' })
  await dialog.getByLabel('Your password').fill(ACCOUNT_PASSWORD)
  await dialog.getByLabel('Type DELETE to confirm').fill('DELETE')
  await dialog.getByRole('button', { name: 'Delete account' }).click()
  await expect(dialog.getByRole('alert')).toHaveText('Something went wrong on our side.')
  expect(await storedToken(page)).toBe('test-token')
})

test('deleting the account signs out and says so on the sign-in page', async ({ page }) => {
  await mockApi(page)
  const hold = deferred()
  const account = await mockAccount(page, { gate: hold.promise })
  await openSettings(page)
  await page.getByRole('button', { name: 'Delete account' }).click()
  const dialog = page.getByRole('dialog', { name: 'Delete your account?' })
  await dialog.getByLabel('Your password').fill(ACCOUNT_PASSWORD)
  await dialog.getByLabel('Type DELETE to confirm').fill('DELETE')
  await dialog.getByRole('button', { name: 'Delete account' }).click()

  // While the request runs the dialog cannot be dismissed.
  await expect(dialog.getByRole('button', { name: 'Deleting…' })).toBeDisabled()
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeDisabled()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  hold.release()

  await expect(page).toHaveURL('/signin?deleted=1')
  await expect(page.getByRole('status')).toHaveText(
    'Your account was deleted, along with all of its data.',
  )
  expect(account.deletions).toEqual([{ password: ACCOUNT_PASSWORD }])
  expect(await storedToken(page)).toBeNull()

  // Signed out for good: the app sends you back to sign in.
  await page.goto('/settings')
  await expect(page).toHaveURL(/\/signin/)
})

// ---- Navigation -----------------------------------------------------------------

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
  await expect(page.getByLabel('Email')).toHaveValue('jane.doe@example.com')
})
