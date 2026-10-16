import { expect, test, type Page } from '@playwright/test'
import {
  apiError,
  deferred,
  mockApi,
  mockResults,
  sampleCV,
  sampleDetail,
  sampleResults,
  sampleSearches,
  signedIn,
  unscoredResults,
} from '../support/mockApi'

const ID = '01JA2B3C4D5E6F7G8H9J0KR001'

test.beforeEach(async ({ page }) => {
  await signedIn(page)
  await mockApi(page, { cvs: [sampleCV], searches: sampleSearches() })
})

const button = (page: Page, name: string | RegExp) => page.getByRole('button', { name })

test('shows the score, breakdown, requirements, strengths, gaps and evaluation', async ({
  page,
}) => {
  await mockResults(page, { results: sampleResults() })
  await page.goto(`/matches/${ID}`)

  await expect(page.getByRole('heading', { level: 1, name: 'Senior Go Engineer' })).toBeVisible()
  await expect(page.getByRole('img', { name: 'Overall score 91 out of 100' })).toBeVisible()
  await expect(page.getByText('Strong match')).toBeVisible()
  await expect(
    page.getByText('Northwind Logistics · Copenhagen · found on LinkedIn 2h ago'),
  ).toBeVisible()

  await expect(page.getByRole('heading', { name: 'Why it scored 91' })).toBeVisible()
  await expect(page.getByText(/^A close fit\./)).toBeVisible()

  const metrics = page.getByTestId('match-metric')
  await expect(metrics).toHaveCount(5)
  await expect(metrics.first()).toContainText('Skills')
  await expect(metrics.first()).toContainText('94')
  await expect(metrics.nth(3)).toContainText('Domain')

  const reqs = page.getByTestId('match-requirement')
  await expect(reqs).toHaveCount(3)
  await expect(reqs.first()).toContainText('5+ years of Go')
  await expect(reqs.first()).toContainText('Must have')
  await expect(reqs.first()).toContainText('Met')
  await expect(reqs.nth(2)).toContainText('Nice to have')
  await expect(reqs.nth(2)).toContainText('Not met')

  const strengths = page.getByRole('region', { name: 'Strengths' })
  await expect(strengths.getByRole('listitem')).toHaveCount(3)
  await expect(page.getByRole('region', { name: 'Missing skills' })).toContainText('Terraform')

  const evaluation = page.getByRole('region', { name: 'Evaluation' })
  await expect(evaluation).toContainText('Uploaded 12 Oct (active)')
  await expect(evaluation).toContainText('deepseek-chat')
  await expect(evaluation).toContainText('v4')
  await expect(evaluation).toContainText('Not applied yet')

  const posting = page.getByRole('link', { name: /Open posting/ })
  await expect(posting).toHaveAttribute('href', 'https://example.com/jobs/1')
  await expect(posting).toHaveAttribute('target', '_blank')
  await expect(posting).toHaveAttribute('rel', 'noopener noreferrer')

  // The long description is collapsed until asked for.
  const toggle = button(page, 'Read the full description')
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await toggle.click()
  await expect(button(page, 'Show less')).toHaveAttribute('aria-expanded', 'true')
})

test('save, mark applied and dismiss update straight away and persist', async ({ page }) => {
  const api = await mockResults(page, { results: sampleResults() })
  await page.goto(`/matches/${ID}`)

  await button(page, 'Save').click()
  await expect(button(page, 'Saved')).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeAttached()
  await expect(page.getByTestId('match-status')).toHaveText('Saved')

  await button(page, 'Mark applied').click()
  await expect(button(page, 'Applied')).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('region', { name: 'Evaluation' })).toContainText('Applied')

  await button(page, 'Dismiss this job').click()
  await expect(button(page, 'Restore this job')).toBeVisible()
  await button(page, 'Restore this job').click()
  await expect(button(page, 'Dismiss this job')).toBeVisible()

  expect(api.writes.map((w) => w.body)).toEqual([
    { status: 'saved' },
    { status: 'applied' },
    { status: 'dismissed' },
    { status: 'new' },
  ])
})

test('a refused update rolls back and says why', async ({ page }) => {
  await mockResults(page, { results: sampleResults() })
  const hold = deferred()
  await page.route(`**/api/results/${ID}`, async (route) => {
    if (route.request().method() !== 'PATCH') return route.fallback()
    await hold.promise
    return route.fulfill({
      status: 500,
      json: apiError('api_error', 'internal_error', 'An unexpected error occurred.'),
    })
  })
  await page.goto(`/matches/${ID}`)

  await button(page, 'Save').click()
  // Optimistic: shown as saved, busy, while the request is out.
  await expect(page.getByTestId('match-status')).toHaveText('Saved')
  await expect(button(page, 'Saving…')).toBeDisabled()
  hold.release()

  await expect(page.getByRole('alert')).toContainText(
    "We couldn't update this match. An unexpected error occurred.",
  )
  await expect(button(page, 'Save')).toHaveAttribute('aria-pressed', 'false')
  await expect(page.getByTestId('match-status')).toHaveCount(0)
})

test('re-evaluate queues a new score; 409 means it already is', async ({ page }) => {
  const api = await mockResults(page, { results: sampleResults() })
  await page.goto(`/matches/${ID}`)

  await button(page, 'Re-evaluate with active CV').click()
  await expect(page.getByTestId('match-eval-status')).toHaveText('Re-scoring…')
  await expect(button(page, 'Scoring in progress')).toBeDisabled()
  expect(api.writes).toEqual([{ method: 'POST', path: `/results/${ID}/re-evaluate`, body: null }])
  // The previous score stays until the new one replaces it.
  await expect(page.getByRole('heading', { name: 'Why it scored 91' })).toBeVisible()
})

test('re-evaluate conflict keeps the pending state without an error', async ({ page }) => {
  await mockResults(page, { results: sampleResults() })
  await page.route(`**/api/results/${ID}/re-evaluate`, (route) =>
    route.fulfill({
      status: 409,
      json: apiError(
        'invalid_request_error',
        'evaluation_in_progress',
        'The result is already queued for or undergoing evaluation.',
      ),
    }),
  )
  await page.goto(`/matches/${ID}`)
  await button(page, 'Re-evaluate with active CV').click()
  await expect(page.getByTestId('match-eval-status')).toHaveText('Re-scoring…')
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(page.getByRole('status').filter({ hasText: 'already being scored' })).toBeAttached()
})

test('a failed re-evaluate rolls back', async ({ page }) => {
  await mockResults(page, { results: sampleResults() })
  await page.route(`**/api/results/${ID}/re-evaluate`, (route) =>
    route.fulfill({ status: 500, json: { error: 'database unavailable' } }),
  )
  await page.goto(`/matches/${ID}`)
  await button(page, 'Re-evaluate with active CV').click()
  await expect(page.getByRole('alert')).toContainText("We couldn't re-evaluate this match.")
  await expect(page.getByTestId('match-eval-status')).toHaveCount(0)
  await expect(button(page, 'Re-evaluate with active CV')).toBeEnabled()
})

test('a job that is not scored yet says so', async ({ page }) => {
  const [r] = unscoredResults()
  await mockResults(page, { results: [r!] })
  await page.goto(`/matches/${r!.id}`)
  await expect(page.getByTestId('match-unscored')).toContainText('Scoring in progress')
  await expect(page.getByRole('img', { name: 'Not scored yet' })).toBeVisible()
  await expect(page.getByTestId('match-metric')).toHaveCount(0)
})

test('an older evaluation without requirements leaves the checklist out', async ({ page }) => {
  const r = sampleResults()[0]!
  const detail = sampleDetail(r)
  detail.evaluation!.requirements = null
  await mockResults(page, { results: [r], details: { [r.id]: detail } })
  await page.goto(`/matches/${r.id}`)
  await expect(page.getByRole('heading', { name: 'Why it scored 91' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Requirements' })).toHaveCount(0)
})

test('an unknown match shows a way back', async ({ page }) => {
  await mockResults(page)
  await page.goto('/matches/01JA2B3C4D5E6F7G8H9J0KR999')
  await expect(page.getByRole('heading', { name: "We couldn't find this match" })).toBeVisible()
  await page.getByRole('link', { name: 'Back to matches' }).click()
  await expect(page).toHaveURL('/matches')
})
