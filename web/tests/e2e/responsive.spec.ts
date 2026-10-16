import { expect, test, type Page } from '@playwright/test'
import { mockApi, sampleCV, sampleSearches, signedIn } from '../support/mockApi'
import { mockResults, sampleResults, unscoredResults } from '../support/mockApi'

// Only meaningful on the 375px "mobile" project.
test.skip(({ viewport }) => (viewport?.width ?? 0) > 400, 'phone layout only')

async function expectNoHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )
  expect(overflow).toBeLessThanOrEqual(0)
}

test('sign in and sign up fit a 375px screen', async ({ page }) => {
  await page.goto('/signin')
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible()
  await expectNoHorizontalScroll(page)

  await page.goto('/signup')
  await expect(page.getByRole('heading', { name: 'Create your account' })).toBeVisible()
  await expectNoHorizontalScroll(page)
})

test('the overview fits a 375px screen, with the menus closed and open', async ({ page }) => {
  await signedIn(page)
  await mockApi(page, { cvs: [sampleCV], searches: sampleSearches() })
  await page.goto('/')
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
  await expectNoHorizontalScroll(page)

  // The sidebar collapses into a top bar with a menu button.
  await expect(page.getByRole('link', { name: 'Job searches' })).toBeHidden()
  const toggle = page.getByRole('button', { name: 'Open menu' })
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await toggle.click()
  await expect(page.getByRole('button', { name: 'Close menu' })).toHaveAttribute(
    'aria-expanded',
    'true',
  )
  await expect(page.getByRole('link', { name: 'Job searches' })).toBeVisible()
  await expectNoHorizontalScroll(page)

  // The account menu shares the top bar and its drop-down stays on screen.
  await page.getByRole('button', { name: 'Close menu' }).click()
  await page.getByRole('button', { name: 'Account menu' }).click()
  const panel = page.getByRole('menu', { name: 'Account' })
  await expect(panel).toBeVisible()
  const box = await panel.boundingBox()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(375)
  await expectNoHorizontalScroll(page)
})

test('the new-account overview fits a 375px screen', async ({ page }) => {
  await signedIn(page)
  await mockApi(page)
  await page.goto('/')
  await expect(page.getByText('1 of 3 done')).toBeVisible()
  await expectNoHorizontalScroll(page)
})

// ── Matches ─────────────────────────────────────────────────────────────────

test('the matches pages and the overview with matches fit a 375px screen', async ({ page }) => {
  await signedIn(page)
  await mockApi(page, { cvs: [sampleCV], searches: sampleSearches() })
  await mockResults(page, { results: sampleResults() })

  await page.goto('/')
  await expect(page.getByTestId('top-match')).toHaveCount(5)
  await expectNoHorizontalScroll(page)

  await page.goto('/matches')
  await expect(page.getByTestId('match-row')).toHaveCount(6)
  await expectNoHorizontalScroll(page)

  await page.goto('/matches/01JA2B3C4D5E6F7G8H9J0KR001')
  await expect(page.getByRole('heading', { level: 1, name: 'Senior Go Engineer' })).toBeVisible()
  await page.getByRole('button', { name: 'Read the full description' }).click()
  await expectNoHorizontalScroll(page)
})

test('the in-progress and empty matches states fit a 375px screen', async ({ page }) => {
  await signedIn(page)
  await mockApi(page, { cvs: [sampleCV], searches: sampleSearches() })
  await mockResults(page, { results: unscoredResults() })
  await page.goto('/matches')
  await expect(page.getByTestId('matches-in-progress')).toBeVisible()
  await expectNoHorizontalScroll(page)

  await page.goto('/matches?status=dismissed')
  await expect(page.getByTestId('matches-empty-state')).toBeVisible()
  await expectNoHorizontalScroll(page)
})
