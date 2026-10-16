// API keys and Settings on a 375px screen: no page-level horizontal scroll;
// the key table scrolls inside its own box.
import { expect, test, type Page } from '@playwright/test'
import {
  apiError,
  mockAccount,
  mockApi,
  mockApiKeys,
  sampleApiKeys,
  signedIn,
} from '../support/mockApi'

test.skip(({ viewport }) => (viewport?.width ?? 0) > 400, 'phone layout only')

async function expectNoHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )
  expect(overflow).toBeLessThanOrEqual(0)
}

test.beforeEach(async ({ page }) => {
  await signedIn(page)
  await mockApi(page)
})

test('the empty state and curl example fit', async ({ page }) => {
  await mockApiKeys(page)
  await page.goto('/api-keys')
  await expect(page.getByRole('heading', { name: 'No API keys yet' })).toBeVisible()
  await expectNoHorizontalScroll(page)
})

test('the key table scrolls inside its box, and the banner and dialogs fit', async ({ page }) => {
  await mockApiKeys(page, { keys: sampleApiKeys() })
  await page.goto('/api-keys')
  await expect(page.getByRole('row', { name: /Zapier/ })).toBeVisible()
  await expectNoHorizontalScroll(page)

  const scroller = page.locator('.table-scroll')
  const { scrollWidth, clientWidth } = await scroller.evaluate((el) => ({
    scrollWidth: el.scrollWidth,
    clientWidth: el.clientWidth,
  }))
  expect(scrollWidth).toBeGreaterThan(clientWidth)
  await scroller.evaluate((el) => (el.scrollLeft = el.scrollWidth))
  await expect(page.getByRole('button', { name: 'Revoke Zapier' })).toBeInViewport()
  await expectNoHorizontalScroll(page)

  await page.getByRole('button', { name: 'Create API key' }).click()
  const dialog = page.getByRole('dialog', { name: 'Create API key' })
  const box = (await dialog.boundingBox())!
  expect(box.x).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(375)
  await dialog.getByLabel('Name').fill('Nightly export script')
  await dialog.getByRole('button', { name: 'Create key' }).click()

  await expect(page.getByTestId('new-key-banner')).toBeVisible()
  await expectNoHorizontalScroll(page)
  const secret = (await page.getByTestId('new-key-secret').boundingBox())!
  expect(secret.x + secret.width).toBeLessThanOrEqual(375)
})

test('settings fit, with errors and the delete dialog', async ({ page }) => {
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
  await page.goto('/settings')
  await expect(page.getByLabel('Email')).toHaveValue('jane.doe@example.com')
  await expectNoHorizontalScroll(page)

  // Fields stack in one column, full width minus the gutter.
  const first = (await page.getByLabel('First name').boundingBox())!
  const last = (await page.getByLabel('Last name').boundingBox())!
  expect(last.y).toBeGreaterThan(first.y)
  expect(first.x + first.width).toBeLessThanOrEqual(375)

  await page.getByLabel('Email').fill('a-really-long-address-that-should-wrap@example.com')
  await page.getByRole('button', { name: 'Save profile' }).click()
  await expect(page.locator('#email-error')).toBeVisible()
  await page.getByTestId('password-form').getByRole('button', { name: 'Change password' }).click()
  await expect(page.locator('#new-password-error')).toBeVisible()
  await expectNoHorizontalScroll(page)

  for (const name of ['Download my data first', 'Delete account']) {
    const box = (await page.getByRole('button', { name }).boundingBox())!
    expect(box.x).toBeGreaterThanOrEqual(0)
    expect(box.x + box.width).toBeLessThanOrEqual(375)
  }

  await page.getByRole('button', { name: 'Delete account' }).click()
  const dialog = page.getByRole('dialog', { name: 'Delete your account?' })
  await dialog.getByRole('button', { name: 'Delete account' }).click()
  await expect(page.locator('#delete-confirm-error')).toBeVisible()
  const box = (await dialog.boundingBox())!
  expect(box.x).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(375)
  await expectNoHorizontalScroll(page)
})
