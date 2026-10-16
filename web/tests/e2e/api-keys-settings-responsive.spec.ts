// API keys and Settings on a 375px screen: no page-level horizontal scroll;
// the key table scrolls inside its own box.
import { expect, test, type Page } from '@playwright/test'
import { mockApi, mockApiKeys, sampleApiKeys, signedIn } from '../support/mockApi'

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

test('settings fit', async ({ page }) => {
  await page.goto('/settings')
  await expect(page.getByTestId('profile-email')).toHaveText('jane.doe@example.com')
  await expectNoHorizontalScroll(page)
})
