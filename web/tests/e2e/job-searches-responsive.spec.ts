import { expect, test, type Page } from '@playwright/test'
import {
  mockApi,
  mockJobSearchesApi,
  sampleSearches,
  sampleSuggestions,
  signedIn,
} from '../support/mockApi'

// Only meaningful on the 375px "mobile" project.
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

test('the empty job searches page fits a 375px screen', async ({ page }) => {
  await mockJobSearchesApi(page)
  await page.goto('/searches')
  await expect(page.getByTestId('searches-empty')).toBeVisible()
  await expectNoHorizontalScroll(page)
})

test('the job searches list fits a 375px screen, menus and confirmations too', async ({ page }) => {
  await mockJobSearchesApi(page, { searches: sampleSearches(), suggestions: sampleSuggestions() })
  await page.goto('/searches')
  await expect(page.getByRole('article')).toHaveCount(3)
  await expectNoHorizontalScroll(page)

  const menuButton = page.getByRole('button', { name: 'More actions for Senior Go · Copenhagen' })
  await menuButton.click()
  const menu = page.getByRole('menu', { name: 'More actions for Senior Go · Copenhagen' })
  const box = await menu.boundingBox()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(375)
  await page.getByRole('menuitem', { name: 'Delete' }).click()
  await expect(page.getByRole('button', { name: 'Delete search' })).toBeVisible()
  await expectNoHorizontalScroll(page)

  // Touch targets: the switch and buttons stay comfortably large.
  const toggle = page.getByRole('article').first().locator('.switch')
  expect((await toggle.boundingBox())!.height).toBeGreaterThanOrEqual(44)
})

test('the search form fits a 375px screen', async ({ page }) => {
  await mockJobSearchesApi(page)
  await page.goto('/searches/new')
  await expect(page.getByRole('heading', { name: 'New job search' })).toBeVisible()
  await page.getByRole('button', { name: 'Create search' }).click()
  await expect(page.getByText('Enter what to search for')).toBeVisible()
  await expectNoHorizontalScroll(page)
})
