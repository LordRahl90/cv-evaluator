// Jobs that are not scored yet: why they wait, and "Score it", on the
// matches list, a match and the overview.
import { expect, test, type Page } from '@playwright/test'
import {
  apiError,
  mockApi,
  mockResults,
  sampleCV,
  sampleResults,
  sampleSearches,
  signedIn,
  waitingResults,
} from '../support/mockApi'

const [QUEUED, CAPPED, HELD] = waitingResults().map((r) => r.id) as [string, string, string]

const RATE_LIMITED = apiError(
  'rate_limit_error',
  'rate_limited',
  'Too many re-evaluations in a short time (at most 30 per 1h0m0s). Try again in 1800 seconds.',
)

test.beforeEach(async ({ page }) => {
  await signedIn(page)
  await mockApi(page, { cvs: [sampleCV], searches: sampleSearches() })
})

const rows = (page: Page) => page.getByTestId('match-row')
const row = (page: Page, title: string) => rows(page).filter({ hasText: title })

test('the list says why each unscored job waits', async ({ page }) => {
  await mockResults(page, { results: [...sampleResults(), ...waitingResults()] })
  await page.goto('/matches')

  const queued = row(page, 'Backend Engineer, Go')
  await expect(queued).toContainText('Not scored yet')
  await expect(queued.getByTestId('match-row-reason')).toHaveText(
    'In the queue. Jobs closest to your CV are scored first.',
  )
  await expect(row(page, 'Platform Engineer').getByTestId('match-row-reason')).toHaveText(
    "Today's scoring limit is reached. It will be scored within 24 hours.",
  )
  await expect(row(page, 'Marketing Coordinator').getByTestId('match-row-reason')).toContainText(
    'looks far from your CV',
  )
  await expect(page.getByRole('button', { name: /^Score it/ })).toHaveCount(3)

  // Scored jobs, and one being re-scored, offer no button.
  await expect(row(page, 'Senior Go Engineer').getByRole('button')).toHaveCount(0)
  await expect(row(page, 'Go Developer (Contract').getByRole('button')).toHaveCount(0)
})

test('Score it on a row queues the job without leaving the list', async ({ page }) => {
  const api = await mockResults(page, { results: waitingResults() })
  await page.goto('/matches')

  const held = row(page, 'Marketing Coordinator')
  await held.getByRole('button', { name: /^Score it\W+Marketing Coordinator$/ }).click()

  await expect(held.getByTestId('match-row-reason')).toHaveText(
    'Queued to be scored next. The score appears here when it is ready.',
  )
  await expect(held.getByRole('button')).toHaveCount(0)
  await expect(page).toHaveURL('/matches')
  expect(api.writes).toEqual([{ method: 'POST', path: `/results/${HELD}/re-evaluate`, body: null }])
})

test('Score it shows the rate limit message', async ({ page }) => {
  await mockResults(page, { results: waitingResults() })
  await page.route(`**/api/results/${CAPPED}/re-evaluate`, (route) =>
    route.fulfill({ status: 429, json: RATE_LIMITED }),
  )
  await page.goto('/matches')

  const capped = row(page, 'Platform Engineer')
  await capped.getByRole('button', { name: /^Score it/ }).click()
  await expect(capped.getByRole('alert')).toHaveText(RATE_LIMITED.error.message)
  // Nothing was queued, so the button stays for a later try.
  await expect(capped.getByRole('button', { name: /^Score it/ })).toBeEnabled()
})

test('Score it on a job already queued says so', async ({ page }) => {
  await mockResults(page, { results: waitingResults() })
  await page.route(`**/api/results/${QUEUED}/re-evaluate`, (route) =>
    route.fulfill({
      status: 409,
      json: apiError(
        'invalid_request_error',
        'evaluation_in_progress',
        'The result is already queued for or undergoing evaluation.',
      ),
    }),
  )
  await page.goto('/matches')

  const queued = row(page, 'Backend Engineer, Go')
  await queued.getByRole('button', { name: /^Score it/ }).click()
  await expect(queued.getByTestId('match-row-reason')).toContainText('Queued to be scored next')
  await expect(queued.getByRole('alert')).toHaveCount(0)
})

test('the row still opens the match', async ({ page }) => {
  await mockResults(page, { results: waitingResults() })
  await page.goto('/matches')
  await row(page, 'Platform Engineer').getByRole('link').click()
  await expect(page).toHaveURL(`/matches/${CAPPED}`)
})

test('the daily limit is explained while jobs wait', async ({ page }) => {
  await mockResults(page, { results: waitingResults() })
  await page.goto('/matches')
  await expect(page.getByTestId('matches-in-progress')).toBeVisible()
  await expect(page.getByTestId('daily-cap')).toContainText("Today's scoring limit is reached")
})

test('a held match can be scored from its page', async ({ page }) => {
  const api = await mockResults(page, { results: waitingResults() })
  await page.goto(`/matches/${HELD}`)

  const section = page.getByTestId('match-unscored')
  await expect(section.getByRole('heading', { name: 'Not scored yet' })).toBeVisible()
  await expect(page.getByTestId('match-waiting-reason')).toContainText('looks far from your CV')
  await expect(page.getByTestId('match-eval-status')).toHaveText('Not scored yet')
  // The aside's re-evaluate button is for scored jobs.
  await expect(page.getByRole('button', { name: /Re-evaluate/ })).toHaveCount(0)

  await section.getByRole('button', { name: 'Score it' }).click()
  await expect(page.getByTestId('match-waiting-reason')).toContainText('Queued to be scored next')
  await expect(section.getByRole('button', { name: 'Score it' })).toHaveCount(0)
  await expect(section.getByRole('button', { name: 'Check again' })).toBeVisible()
  expect(api.writes).toEqual([{ method: 'POST', path: `/results/${HELD}/re-evaluate`, body: null }])
})

test('a match waiting behind the daily limit shows the rate limit message', async ({ page }) => {
  await mockResults(page, { results: waitingResults() })
  await page.route(`**/api/results/${CAPPED}/re-evaluate`, (route) =>
    route.fulfill({ status: 429, json: RATE_LIMITED }),
  )
  await page.goto(`/matches/${CAPPED}`)

  await expect(page.getByTestId('match-waiting-reason')).toContainText(
    "Today's scoring limit is reached",
  )
  await page.getByTestId('match-unscored').getByRole('button', { name: 'Score it' }).click()
  await expect(page.getByRole('alert')).toHaveText(RATE_LIMITED.error.message)
  await expect(page.getByTestId('match-waiting-reason')).toContainText(
    "Today's scoring limit is reached",
  )
  await expect(
    page.getByTestId('match-unscored').getByRole('button', { name: 'Score it' }),
  ).toBeEnabled()
})

test('the overview counts jobs waiting to be scored', async ({ page }) => {
  await mockResults(page, { results: [...sampleResults(), ...waitingResults()] })
  await page.goto('/')
  const waiting = page.getByTestId('stat-waiting')
  await expect(waiting).toContainText('Waiting to be scored')
  await expect(waiting).toContainText('3')
  await expect(waiting).toContainText('Daily limit reached, more within 24 hours')
  await waiting.getByRole('link', { name: 'See them' }).click()
  await expect(page).toHaveURL('/matches')
})

test('the overview leaves the count out when nothing waits', async ({ page }) => {
  await mockResults(page, { results: sampleResults() })
  await page.goto('/')
  await expect(page.getByTestId('stat-strong-matches')).toBeVisible()
  await expect(page.getByTestId('stat-waiting')).toHaveCount(0)
})

test('waiting rows fit a phone screen', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 })
  await mockResults(page, { results: waitingResults() })
  await page.goto('/matches')
  await expect(rows(page)).toHaveCount(3)
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )
  expect(overflow).toBeLessThanOrEqual(0)
  const button = rows(page)
    .first()
    .getByRole('button', { name: /^Score it/ })
  const box = (await button.boundingBox())!
  expect(box.x + box.width).toBeLessThanOrEqual(375)
})
