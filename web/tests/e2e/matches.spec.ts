import { expect, test } from '@playwright/test'
import {
  deferred,
  mockApi,
  mockResults,
  sampleCV,
  sampleResults,
  sampleSearches,
  signedIn,
  summarize,
  unscoredResults,
} from '../support/mockApi'
import type { SearchResult } from '../../src/api/resultTypes'

test.beforeEach(async ({ page }) => {
  await signedIn(page)
  await mockApi(page, { cvs: [sampleCV], searches: sampleSearches() })
})

const rows = (page: import('@playwright/test').Page) => page.getByTestId('match-row')

test('lists matches best first with their status, gaps and counts', async ({ page }) => {
  await mockResults(page, { results: sampleResults() })
  await page.goto('/matches')

  await expect(page.getByRole('heading', { level: 1, name: 'Matches' })).toBeVisible()
  await expect(page.getByText('6 jobs scored against your CV · 6 new this week')).toBeVisible()
  await expect(rows(page)).toHaveCount(6)
  await expect(page.getByTestId('match-count')).toHaveText('Showing 6 of 6')

  const first = rows(page).first()
  await expect(first).toContainText('Senior Go Engineer')
  await expect(first).toContainText('New')
  await expect(first).toContainText('Northwind Logistics · Copenhagen · LinkedIn · 2h ago')
  await expect(first).toContainText('Missing: Terraform')
  await expect(first.getByRole('img', { name: 'Overall score 91 out of 100' })).toBeVisible()
  await expect(rows(page).nth(1)).toContainText('Saved')
  await expect(rows(page).nth(2)).toContainText('Applied')
  await expect(rows(page).nth(5)).toContainText('Re-scoring…')

  const tabs = page.getByRole('navigation', { name: 'Filter by search' })
  await expect(tabs.getByRole('link', { name: 'All searches 6' })).toHaveAttribute(
    'aria-current',
    'page',
  )
  await expect(tabs.getByRole('link', { name: 'Senior Go · Copenhagen 3' })).toBeVisible()
})

test('search tabs and filters narrow the list and live in the URL', async ({ page }) => {
  const api = await mockResults(page, { results: sampleResults() })
  await page.goto('/matches')
  await expect(rows(page)).toHaveCount(6)
  expect(api.listCalls.at(-1)!.searchParams.get('status')).toBe('new,saved,applied')

  await page.getByRole('link', { name: 'Backend · Remote EU 2' }).click()
  await expect(page).toHaveURL(/search=01JA2B3C4D5E6F7G8H9J0KS002/)
  await expect(rows(page)).toHaveCount(2)
  expect(api.listCalls.at(-1)!.pathname).toContain(
    '/job-searches/01JA2B3C4D5E6F7G8H9J0KS002/results',
  )
  await expect(page.getByRole('link', { name: 'Backend · Remote EU 2' })).toHaveAttribute(
    'aria-current',
    'page',
  )

  await page.getByRole('link', { name: 'All searches 6' }).click()
  await page.getByLabel('Min score').selectOption('80')
  await expect(rows(page)).toHaveCount(3)
  await expect(page).toHaveURL(/min=80/)

  await page.getByLabel('Status').selectOption('saved')
  await expect(rows(page)).toHaveCount(1)
  await expect(rows(page).first()).toContainText('Backend Engineer, Payments')

  await page.getByLabel('Status').selectOption('dismissed')
  await expect(page.getByTestId('matches-empty-state')).toContainText(
    'No matches for these filters',
  )
  await page.getByRole('link', { name: 'Clear filters' }).click()
  await expect(rows(page)).toHaveCount(6)
  await expect(page.getByLabel('Min score')).toHaveValue('')

  await page.getByLabel('Sort').selectOption('newest')
  await expect(page).toHaveURL(/sort=newest/)
  await expect.poll(() => api.listCalls.at(-1)!.searchParams.get('sort')).toBe('newest')

  // A reload keeps the filters.
  await page.goto('/matches?status=applied')
  await expect(page.getByLabel('Status')).toHaveValue('applied')
  await expect(rows(page)).toHaveCount(1)
})

test('"Load more" follows the cursor', async ({ page }) => {
  const many: SearchResult[] = Array.from({ length: 25 }, (_, i) => {
    const r = sampleResults()[i % 6]!
    return {
      ...r,
      id: `01JA2B3C4D5E6F7G8H9J0KM${String(i).padStart(3, '0')}`,
      evaluation: { ...r.evaluation!, overall_score: 99 - i },
    }
  })
  const api = await mockResults(page, { results: many })
  await page.goto('/matches')
  await expect(rows(page)).toHaveCount(20)
  await expect(page.getByTestId('match-count')).toHaveText('Showing 20 of 25')

  await page.getByRole('button', { name: 'Load more matches' }).click()
  await expect(rows(page)).toHaveCount(25)
  expect(api.listCalls.at(-1)!.searchParams.get('starting_after')).toBe(many[19]!.id)
  await expect(page.getByRole('button', { name: 'Load more matches' })).toHaveCount(0)
  await expect(page.getByTestId('match-count')).toHaveText('Showing 25 of 25')
})

test('a failed "Load more" shows an inline error and keeps the list', async ({ page }) => {
  const many = Array.from({ length: 21 }, (_, i) => ({
    ...sampleResults()[0]!,
    id: `01JA2B3C4D5E6F7G8H9J0KM${String(i).padStart(3, '0')}`,
  }))
  await mockResults(page, { results: many })
  await page.goto('/matches')
  await expect(rows(page)).toHaveCount(20)
  await page.route(
    (url) => url.pathname.endsWith('/api/results') && url.searchParams.has('starting_after'),
    (route) => route.fulfill({ status: 500, json: { error: 'database unavailable' } }),
  )
  await page.getByRole('button', { name: 'Load more matches' }).click()
  await expect(page.getByRole('alert')).toContainText("We couldn't load more matches.")
  await expect(rows(page)).toHaveCount(20)
})

test('loading, then an error with a retry', async ({ page }) => {
  const hold = deferred()
  await mockResults(page, { results: sampleResults(), gate: hold.promise })
  let fail = true
  await page.route(
    (url) => url.pathname.endsWith('/api/results'),
    async (route) => {
      await hold.promise
      if (fail) return route.fulfill({ status: 500, json: { error: 'database unavailable' } })
      return route.fallback()
    },
  )
  await page.goto('/matches')
  await expect(page.getByTestId('matches-loading')).toBeVisible()
  hold.release()
  await expect(page.getByRole('alert')).toContainText('Database unavailable')
  fail = false
  await page.getByRole('button', { name: 'Try again' }).click()
  await expect(rows(page)).toHaveCount(6)
})

test('empty: no searches yet', async ({ page }) => {
  await mockResults(page, { summary: { ...summarize([]), by_search: [] } })
  await page.goto('/matches')
  const empty = page.getByTestId('matches-empty-state')
  await expect(empty).toContainText('No matches yet')
  await expect(empty.getByRole('link', { name: 'New search' })).toHaveAttribute(
    'href',
    '/searches/new',
  )
  await expect(
    page.getByText('Every job your searches found, scored against your active CV.'),
  ).toBeVisible()
})

test('empty: a search that has found nothing yet', async ({ page }) => {
  await mockResults(page)
  await page.goto('/matches?search=01JA2B3C4D5E6F7G8H9J0KS003')
  await expect(page.getByTestId('matches-empty-state')).toContainText('No jobs found yet')
})

test('in progress: jobs found but none scored yet', async ({ page }) => {
  await mockResults(page, { results: unscoredResults() })
  await page.goto('/matches')
  const progress = page.getByTestId('matches-in-progress')
  await expect(progress).toContainText('Scoring in progress')
  await expect(progress).toContainText('2 jobs found')
  await expect(rows(page)).toHaveCount(2)
  await expect(rows(page).first()).toContainText('Scoring…')
  await expect(rows(page).first().getByRole('img', { name: 'Not scored yet' })).toBeVisible()
})

test('a row opens the match', async ({ page }) => {
  await mockResults(page, { results: sampleResults() })
  await page.goto('/matches')
  await rows(page).first().click()
  await expect(page).toHaveURL('/matches/01JA2B3C4D5E6F7G8H9J0KR001')
  await expect(page.getByRole('heading', { level: 1, name: 'Senior Go Engineer' })).toBeVisible()
})
