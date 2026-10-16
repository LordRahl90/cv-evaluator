import { expect, test, type Page } from '@playwright/test'
import {
  deferred,
  mockApi,
  mockQuickMatch,
  sampleCV,
  sampleJobDescription,
  sampleMatch,
  signedIn,
} from '../support/mockApi'

test.beforeEach(async ({ page }) => {
  await signedIn(page)
})

async function expectNoHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )
  expect(overflow).toBeLessThanOrEqual(0)
}

const textarea = (page: Page) => page.getByLabel('Job description')
const submit = (page: Page) => page.getByRole('button', { name: 'Score this job' })

test('the form names the CV it scores against and the empty recent panel', async ({ page }) => {
  await mockApi(page, { cvs: [sampleCV] })
  await mockQuickMatch(page)
  await page.goto('/quick-match')

  await expect(page.getByRole('heading', { level: 1, name: 'Quick match' })).toBeVisible()
  await expect(textarea(page)).toBeVisible()
  await expect(page.getByRole('link', { name: 'your CV from 12 Oct 2026' })).toHaveAttribute(
    'href',
    '/cvs',
  )
  const recent = page.getByRole('complementary', { name: 'Recent quick matches' })
  await expect(recent).toContainText('Nothing yet')
  await expect(recent).toContainText("They aren't saved.")
})

test('an empty description is caught before calling the API', async ({ page }) => {
  await mockApi(page, { cvs: [sampleCV] })
  const bodies = await mockQuickMatch(page)
  await page.goto('/quick-match')
  await submit(page).click()
  await expect(page.getByText('Paste a job description first.')).toBeVisible()
  await expect(textarea(page)).toHaveAttribute('aria-invalid', 'true')
  expect(bodies).toHaveLength(0)
})

test('loading state, then the full result', async ({ page }) => {
  await mockApi(page, { cvs: [sampleCV] })
  const hold = deferred()
  const bodies = await mockQuickMatch(page, { gate: hold.promise })
  await page.goto('/quick-match')

  await textarea(page).fill(sampleJobDescription)
  await submit(page).click()

  const busy = page.getByRole('button', { name: 'Scoring…' })
  await expect(busy).toBeDisabled()
  await expect(textarea(page)).toHaveAttribute('readonly', '')
  await expect(page.getByTestId('qm-scoring')).toContainText(
    'Scoring this job against your CV from 12 Oct 2026',
  )
  // A second click does nothing while it runs.
  await busy.click({ force: true })
  hold.release()

  const result = page.getByTestId('qm-result')
  await expect(result).toBeVisible()
  expect(bodies).toEqual([{ job_description: sampleJobDescription }])

  await expect(page.getByRole('img', { name: 'Score 68 out of 100' }).first()).toBeVisible()
  await expect(result).toContainText('Partial match')
  await expect(
    result.getByRole('heading', { name: 'Product Engineer (Go, TypeScript)' }),
  ).toBeVisible()
  await expect(result).toContainText('Pasted description · 50 words')
  await expect(result).toContainText(sampleMatch.summary)

  const bars = result.getByRole('list', { name: 'Score breakdown' }).getByRole('listitem')
  await expect(bars).toHaveCount(4)
  await expect(bars.first()).toContainText('Skills')
  await expect(bars.first()).toContainText('62')
  await expect(bars.first()).toContainText('Strong Go and SQL; no React or TypeScript.')

  await expect(result).toContainText('Go APIs and PostgreSQL')
  await expect(result).toContainText('React and TypeScript')

  const reqs = page.getByTestId('qm-requirements').getByRole('listitem')
  await expect(reqs).toHaveCount(4)
  await expect(result).toContainText('2 of 4 met')
  // Must-haves come first; the nice-to-have is last.
  await expect(reqs.last()).toContainText('Nice to have')
  await expect(reqs.last()).toContainText('Partly met')
  await expect(reqs.nth(1)).toContainText('Not met')
  await expect(reqs.nth(1)).toContainText('No frontend work is mentioned in the CV.')

  await expect(page.getByText(/Scored against your CV from 12 Oct 2026 just now/)).toBeVisible()
  const recent = page.getByRole('complementary', { name: 'Recent quick matches' })
  await expect(recent.getByRole('button')).toHaveCount(1)
  await expect(recent).toContainText('Product Engineer (Go, TypeScript)')
})

test('results without requirements still render; recent list switches results', async ({
  page,
}) => {
  await mockApi(page, { cvs: [sampleCV] })
  let call = 0
  await page.route('**/api/match/job-description', (route) => {
    call += 1
    const body =
      call === 1
        ? sampleMatch
        : { ...sampleMatch, requirements: undefined, overall_score: 84, summary: 'Great fit.' }
    return route.fulfill({ status: 200, json: body })
  })
  await page.goto('/quick-match')

  await textarea(page).fill(sampleJobDescription)
  await submit(page).click()
  await expect(page.getByTestId('qm-result')).toContainText('Partial match')

  await page.getByRole('button', { name: 'Score another job' }).click()
  await expect(textarea(page)).toHaveValue('')
  await textarea(page).fill('Senior SRE\nKubernetes, Terraform, on-call.')
  await submit(page).click()

  const result = page.getByTestId('qm-result')
  await expect(result).toContainText('Strong match')
  await expect(result.getByRole('heading', { name: 'Senior SRE' })).toBeVisible()
  await expect(page.getByTestId('qm-requirements')).toHaveCount(0)

  const recent = page.getByRole('complementary', { name: 'Recent quick matches' })
  await expect(recent.getByRole('button')).toHaveCount(2)
  await recent.getByRole('button', { name: /Product Engineer/ }).click()
  await expect(result).toContainText('Partial match')
  await expect(recent.getByRole('button', { name: /Product Engineer/ })).toHaveAttribute(
    'aria-current',
    'true',
  )
})

test('no CV yet: an empty state that links to the CVs page', async ({ page }) => {
  await mockApi(page, { cvs: [] })
  await mockQuickMatch(page)
  await page.goto('/quick-match')

  const empty = page.getByTestId('qm-no-cv')
  await expect(empty.getByRole('heading', { name: 'Upload a CV first' })).toBeVisible()
  await expect(textarea(page)).toHaveCount(0)
  await empty.getByRole('link', { name: 'Upload CV' }).click()
  await expect(page).toHaveURL('/cvs')
})

test('a 422 from the match call also shows the no-CV state', async ({ page }) => {
  // The list failed to load, so the page only learns about it from the 422.
  await mockApi(page)
  await page.route('**/api/cv/', (route) => route.fulfill({ status: 500, json: {} }))
  await mockQuickMatch(page, {
    reply: { status: 422, body: { error: 'upload a CV before matching' } },
  })
  await page.goto('/quick-match')

  await expect(page.getByText('Scored against your newest CV')).toBeVisible()
  await textarea(page).fill(sampleJobDescription)
  await submit(page).click()
  await expect(page.getByTestId('qm-no-cv')).toBeVisible()
})

test('an API error is shown inline and the text is kept for a retry', async ({ page }) => {
  await mockApi(page, { cvs: [sampleCV] })
  await mockQuickMatch(page, {
    reply: { status: 504, body: { error: 'context deadline exceeded' } },
  })
  await page.goto('/quick-match')

  await textarea(page).fill(sampleJobDescription)
  await submit(page).click()
  await expect(page.getByTestId('qm-error')).toContainText(
    "We couldn't score this job. Context deadline exceeded",
  )
  await expect(textarea(page)).toHaveValue(sampleJobDescription)
  await expect(submit(page)).toBeEnabled()
})

test('the sidebar and the overview link to Quick match', async ({ page }) => {
  await mockApi(page, { cvs: [sampleCV] })
  await page.goto('/')
  await page.getByRole('link', { name: 'Try Quick match' }).click()
  await expect(page).toHaveURL('/quick-match')
  await expect(page.getByRole('heading', { level: 1, name: 'Quick match' })).toBeVisible()
})

test('quick match fits a 375px screen, form and result', async ({ page, viewport }) => {
  test.skip((viewport?.width ?? 0) > 400, 'phone layout only')
  await mockApi(page, { cvs: [sampleCV] })
  await mockQuickMatch(page)
  await page.goto('/quick-match')
  await expect(textarea(page)).toBeVisible()
  await expectNoHorizontalScroll(page)

  await textarea(page).fill(sampleJobDescription)
  await submit(page).click()
  await expect(page.getByTestId('qm-result')).toBeVisible()
  await expectNoHorizontalScroll(page)
})
