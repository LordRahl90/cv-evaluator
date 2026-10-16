import { expect, test } from '@playwright/test'
import {
  deferred,
  mockApi,
  mockResults,
  sampleCV,
  sampleResults,
  sampleSearches,
  signedIn,
  unscoredResults,
} from '../support/mockApi'

test.beforeEach(async ({ page }) => {
  await signedIn(page)
  await mockApi(page, { cvs: [sampleCV], searches: sampleSearches() })
})

test('shows the top new matches and real match counts', async ({ page }) => {
  const hold = deferred()
  const api = await mockResults(page, { results: sampleResults(), gate: hold.promise })
  await page.goto('/')
  await expect(page.getByTestId('matches-loading')).toBeVisible()
  hold.release()

  await expect(page.getByText('6 new matches this week.')).toBeVisible()
  const fresh = page.getByTestId('stat-new-matches')
  await expect(fresh).toContainText('6')
  await expect(fresh).toContainText('Across 3 searches')
  const strong = page.getByTestId('stat-strong-matches')
  await expect(strong).toContainText('3')
  await expect(strong).toContainText('1 still new')

  const top = page.getByTestId('top-match')
  await expect(top).toHaveCount(5)
  await expect(top.first()).toContainText('Senior Go Engineer')
  await expect(top.first()).toContainText('Northwind Logistics · Copenhagen')
  await expect(top.first()).toContainText('LinkedIn')
  await expect(top.first()).toContainText('2h ago')

  const call = api.listCalls.at(-1)!
  expect(call.searchParams.get('since')).not.toBeNull()
  expect(call.searchParams.get('limit')).toBe('5')
  expect(call.searchParams.get('status')).toBe('new,saved,applied')

  await top.first().click()
  await expect(page).toHaveURL('/matches/01JA2B3C4D5E6F7G8H9J0KR001')
})

test('jobs still being scored keep an honest empty state', async ({ page }) => {
  await mockResults(page, { results: unscoredResults() })
  await page.goto('/')
  await expect(page.getByTestId('matches-empty')).toContainText('No new matches this week')
  await expect(page.getByTestId('matches-empty')).toContainText('2 jobs are still being scored.')
  await expect(page.getByTestId('top-match')).toHaveCount(0)
})

test('a failed matches load stays inside the matches area', async ({ page }) => {
  await mockResults(page, { results: sampleResults(), summary: null })
  await page.goto('/')
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
  await expect(page.getByRole('alert')).toContainText("We couldn't load your matches.")
  await expect(page.getByRole('region', { name: 'Search activity' })).toBeVisible()
  await expect(page.getByTestId('stat-new-matches')).toContainText("Couldn't load")

  await mockResults(page, { results: sampleResults() })
  await page
    .getByRole('region', { name: 'Top new matches' })
    .getByRole('button', { name: 'Try again' })
    .click()
  await expect(page.getByTestId('top-match')).toHaveCount(5)
})
