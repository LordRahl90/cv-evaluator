import { expect, test, type Page } from '@playwright/test'
import { deferred, mockApi, sampleCV, sampleSearches, signedIn } from '../support/mockApi'

// On phones the navigation sits behind the menu button.
async function openMenuIfCollapsed(page: Page) {
  const toggle = page.getByRole('button', { name: 'Open menu' })
  if (await toggle.isVisible()) await toggle.click()
}

test.beforeEach(async ({ page }) => {
  await signedIn(page)
})

test('new account: loading skeleton, then the setup checklist', async ({ page }) => {
  const hold = deferred()
  await mockApi(page, { gate: hold.promise })

  await page.goto('/')
  await expect(page.getByTestId('overview-skeleton')).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: 'Loading your overview' })).toBeAttached()
  hold.release()

  await expect(page.getByTestId('overview-skeleton')).toBeHidden()
  await expect(page.getByRole('heading', { name: 'Welcome, Jane' })).toBeVisible()
  await expect(
    page.getByText('Two more steps and your first scored jobs start arriving.'),
  ).toBeVisible()
  await expect(page.getByText('1 of 3 done')).toBeVisible()
  await expect(page.getByTestId('step-account')).toContainText('Done. Welcome aboard.')
  await expect(page.getByTestId('step-cv').getByRole('link', { name: 'Upload CV' })).toBeVisible()
  await expect(
    page.getByTestId('step-search').getByRole('link', { name: 'New search' }),
  ).toBeVisible()

  await expect(page.getByTestId('stat-searches')).toContainText('0')
  await expect(page.getByTestId('stat-searches')).toContainText('None yet')
  await expect(page.getByTestId('stat-cv')).toContainText('Not set')
  await expect(page.getByText('Have one job in mind already?')).toBeVisible()
  // No invented match numbers anywhere.
  await expect(page.getByText(/new matches this week/i)).toHaveCount(0)
})

test('CV uploaded but no search yet: two of three steps done', async ({ page }) => {
  await mockApi(page, { cvs: [sampleCV] })
  await page.goto('/')
  await expect(page.getByText('2 of 3 done')).toBeVisible()
  await expect(
    page.getByText('One more step and your first scored jobs start arriving.'),
  ).toBeVisible()
  await expect(page.getByTestId('step-cv')).toContainText('Done. 1 CV on file.')
  await expect(page.getByTestId('stat-cv')).toContainText('Updated 12 Oct')
})

test('account with a CV and searches: stats, empty matches, search activity', async ({ page }) => {
  await mockApi(page, { cvs: [sampleCV], searches: sampleSearches() })
  await page.goto('/')

  await expect(
    page.getByRole('heading', { level: 1, name: /^Good (morning|afternoon|evening), Jane$/ }),
  ).toBeVisible()
  await expect(page.getByText('Get set up')).toHaveCount(0)
  await expect(
    page.getByText('2 active searches are looking for new jobs on your schedule.'),
  ).toBeVisible()

  await expect(page.getByTestId('stat-searches')).toContainText('2')
  await expect(page.getByTestId('stat-searches')).toContainText(/Next run in 3\d min/)
  await expect(page.getByTestId('stat-cv')).toContainText('CV uploaded')
  await expect(page.getByTestId('stat-last-run')).toContainText('2h ago')

  await expect(page.getByTestId('matches-empty')).toContainText(
    'Matches appear here after your first search runs.',
  )

  const activity = page.getByRole('region', { name: 'Search activity' })
  await expect(activity.getByText('Senior Go · Copenhagen')).toBeVisible()
  await expect(activity.getByText(/^Daily · ran /)).toBeVisible()
  await expect(activity.getByText(/^Hourly · next run in 3\d min$/)).toBeVisible()
  await expect(activity.getByText('Paused')).toBeVisible()
})

test('marks the current page in the navigation', async ({ page }) => {
  await mockApi(page)
  await page.goto('/')
  await openMenuIfCollapsed(page)
  await expect(page.getByRole('link', { name: 'Overview' })).toHaveAttribute('aria-current', 'page')
})

test('navigation items lead to "coming soon" pages inside the shell', async ({ page }) => {
  await mockApi(page)
  await page.goto('/')
  await openMenuIfCollapsed(page)
  await page.getByRole('link', { name: 'Job searches' }).click()
  await expect(page).toHaveURL('/searches')
  await expect(page.getByRole('heading', { name: 'Job searches' })).toBeVisible()
  await expect(page.getByText('Coming soon')).toBeVisible()
})

test('a failed load shows the error (old {"error":"..."} format) and can retry', async ({
  page,
}) => {
  await mockApi(page)
  let calls = 0
  await page.route('**/api/cv/', (route) => {
    calls += 1
    if (calls === 1) {
      return route.fulfill({ status: 500, json: { error: 'database unavailable' } })
    }
    return route.fulfill({ status: 200, json: [] })
  })

  await page.goto('/')
  await expect(page.getByRole('alert')).toContainText('Database unavailable')
  await page.getByRole('button', { name: 'Try again' }).click()
  await expect(page.getByRole('heading', { name: 'Welcome, Jane' })).toBeVisible()
})
