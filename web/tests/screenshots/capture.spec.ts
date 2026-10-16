// Captures the screens shown in pull requests. Run with `npm run screenshots`.
import { expect, test, type Page } from '@playwright/test'
import path from 'node:path'
import {
  apiError,
  deferred,
  mockApi,
  mockJobSearchesApi,
  sampleCV,
  sampleSearches,
  sampleSuggestions,
  signedIn,
} from '../support/mockApi'

const OUT = process.env.SCREENSHOT_DIR ?? 'screenshots'
const desktop = { width: 1280, height: 860 }
const phone = { width: 375, height: 812 }

async function shoot(page: Page, name: string, fullPage = true) {
  await page.mouse.move(0, 0)
  await page.evaluate(() => document.fonts.ready)
  await page.screenshot({ path: path.join(OUT, `${name}.png`), fullPage, animations: 'disabled' })
}

test.use({ deviceScaleFactor: 2 })

test('sign in', async ({ page }) => {
  await page.setViewportSize(desktop)
  await page.goto('/signin')
  await page.getByLabel('Email').fill('jane.doe@example.com')
  await page.getByLabel('Password').fill('password123')
  await shoot(page, 'signin')
})

test('sign up with an error', async ({ page }) => {
  await page.setViewportSize(desktop)
  await page.route('**/api/auth/signup', (route) =>
    route.fulfill({
      status: 409,
      json: apiError(
        'invalid_request_error',
        'email_already_used',
        'An account with this email already exists.',
        'email',
      ),
    }),
  )
  await page.goto('/signup')
  await page.getByLabel('First name').fill('Jane')
  await page.getByLabel('Last name').fill('Doe')
  await page.getByLabel('Email').fill('jane.doe@example.com')
  await page.getByLabel('Password').fill('password123')
  await page.getByRole('button', { name: 'Create account' }).click()
  await expect(page.getByText('An account with this email already exists.')).toBeVisible()
  await shoot(page, 'signup-error')
})

test('dashboard loading', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  const hold = deferred()
  await mockApi(page, { gate: hold.promise })
  await page.goto('/')
  await expect(page.getByTestId('overview-skeleton')).toBeVisible()
  await shoot(page, 'dashboard-loading', false)
  hold.release()
})

test('dashboard, new account', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 940 })
  await signedIn(page)
  await mockApi(page)
  await page.goto('/')
  await expect(page.getByText('1 of 3 done')).toBeVisible()
  await shoot(page, 'dashboard-new-account')
})

test('dashboard with a CV and searches', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 940 })
  await signedIn(page)
  await mockApi(page, { cvs: [sampleCV], searches: sampleSearches() })
  await page.goto('/')
  await expect(page.getByTestId('matches-empty')).toBeVisible()
  await shoot(page, 'dashboard-active')
  await page.getByRole('button', { name: 'Account menu' }).click()
  await expect(page.getByRole('menu', { name: 'Account' })).toBeVisible()
  await shoot(page, 'dashboard-user-menu', false)
})

test('mobile sign in', async ({ page }) => {
  await page.setViewportSize(phone)
  await page.goto('/signin')
  await shoot(page, 'mobile-signin')
})

test('mobile dashboard', async ({ page }) => {
  await page.setViewportSize(phone)
  await signedIn(page)
  await mockApi(page)
  await page.goto('/')
  await expect(page.getByText('1 of 3 done')).toBeVisible()
  await shoot(page, 'mobile-dashboard')
  await page.getByRole('button', { name: 'Open menu' }).click()
  await shoot(page, 'mobile-dashboard-menu', false)
  await page.getByRole('button', { name: 'Close menu' }).click()
  await page.getByRole('button', { name: 'Account menu' }).click()
  await expect(page.getByRole('menu', { name: 'Account' })).toBeVisible()
  await shoot(page, 'mobile-user-menu', false)
})

// Job searches

test('job searches, empty', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  await mockApi(page)
  await mockJobSearchesApi(page)
  await page.goto('/searches')
  await expect(page.getByText('Worldwide')).toBeVisible()
  await expect(page.getByText('Jobindex · live')).toBeVisible()
  await shoot(page, 'searches-empty')
})

test('job searches, loading', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  await mockApi(page)
  const hold = deferred()
  await mockJobSearchesApi(page, { gate: hold.promise })
  await page.goto('/searches')
  await expect(page.getByTestId('searches-skeleton').first()).toBeVisible()
  await shoot(page, 'searches-loading', false)
  hold.release()
})

test('job searches, list', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  await mockApi(page, { cvs: [sampleCV] })
  await mockJobSearchesApi(page, {
    searches: sampleSearches(),
    suggestions: sampleSuggestions(),
    override: (method, path) =>
      method === 'PATCH' && path.endsWith('S002')
        ? { status: 500, body: apiError('api_error', 'internal_error', 'Try again later.') }
        : undefined,
  })
  await page.goto('/searches')
  await expect(page.getByText('StepStone · planned, you suggested it')).toBeVisible()
  await shoot(page, 'searches-list')

  // Run now, a refused toggle (rolled back) and a delete confirmation.
  const card = (name: string) =>
    page.getByRole('article').filter({ has: page.getByRole('heading', { name, exact: true }) })
  await card('Senior Go · Copenhagen').getByRole('button', { name: 'Run now' }).click()
  await expect(card('Senior Go · Copenhagen').getByRole('status')).toBeVisible()
  await card('Backend · Remote EU').getByRole('switch').click({ force: true })
  await expect(card('Backend · Remote EU').getByRole('alert')).toBeVisible()
  await card('Go contractor · Berlin')
    .getByRole('button', { name: /More actions/ })
    .click()
  await page.getByRole('menuitem', { name: 'Delete' }).click()
  await expect(page.getByRole('button', { name: 'Delete search' })).toBeVisible()
  await shoot(page, 'searches-actions')

  await card('Senior Go · Copenhagen')
    .getByRole('button', { name: /More actions/ })
    .click()
  await shoot(page, 'searches-menu', false)
})

test('portal suggestion conflict', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  await mockApi(page)
  await mockJobSearchesApi(page, {
    searches: sampleSearches(),
    suggestions: sampleSuggestions(),
    override: (method, path) =>
      method === 'POST' && path === '/portal-suggestions'
        ? {
            status: 409,
            body: apiError(
              'invalid_request_error',
              'portal_already_supported',
              'This portal is already searched. See GET /providers.',
              'url',
            ),
          }
        : undefined,
  })
  await page.goto('/searches#suggest')
  const section = page.getByRole('region', { name: 'Job portals' })
  await section.getByLabel('Portal name').fill('LinkedIn')
  await section.getByLabel('Website').fill('linkedin.com')
  await section.getByRole('button', { name: 'Suggest portal' }).click()
  await expect(section.locator('#portal-url-error')).toBeVisible()
  await section.screenshot({
    path: path.join(OUT, 'portal-suggestion-conflict.png'),
    animations: 'disabled',
  })
})

test('portal suggestion sent', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  await mockApi(page)
  await mockJobSearchesApi(page, { searches: sampleSearches(), suggestions: sampleSuggestions() })
  await page.goto('/searches#suggest')
  const section = page.getByRole('region', { name: 'Job portals' })
  await section.getByLabel('Portal name').fill('Indeed')
  await section.getByLabel('Website').fill('indeed.com')
  await section.getByRole('button', { name: 'Suggest portal' }).click()
  await expect(section.getByRole('status')).toBeVisible()
  await page.mouse.move(0, 0)
  await section.screenshot({
    path: path.join(OUT, 'portal-suggestion-sent.png'),
    animations: 'disabled',
  })
})

test('new search form', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  await mockApi(page, { cvs: [sampleCV] })
  await mockJobSearchesApi(page, {
    override: (method) =>
      method === 'POST'
        ? {
            status: 400,
            body: apiError(
              'invalid_request_error',
              'parameter_invalid',
              'The location parameter must be at most 200 characters long.',
              'location',
            ),
          }
        : undefined,
  })
  await page.goto('/searches/new')
  await expect(page.getByTestId('searched-on')).toContainText('LinkedIn')
  await shoot(page, 'search-form-new')
  await page.getByRole('button', { name: 'Create search' }).click()
  await expect(page.getByText('Enter what to search for')).toBeVisible()
  await shoot(page, 'search-form-validation')
  await page.getByLabel('Keywords').fill('Senior Go Engineer')
  await page.getByLabel('Location').fill('Copenhagen, Denmark')
  await page.getByRole('button', { name: 'Create search' }).click()
  await expect(page.locator('#location-error')).toBeVisible()
  await shoot(page, 'search-form-api-error')
})

test('edit search form', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  await mockApi(page, { cvs: [sampleCV] })
  await mockJobSearchesApi(page, { searches: sampleSearches() })
  await page.goto('/searches/01JA2B3C4D5E6F7G8H9J0KS001/edit')
  await expect(page.getByLabel('Keywords')).toHaveValue('senior go engineer')
  await shoot(page, 'search-form-edit')
})

test('mobile job searches', async ({ page }) => {
  await page.setViewportSize(phone)
  await signedIn(page)
  await mockApi(page)
  await mockJobSearchesApi(page)
  await page.goto('/searches')
  await expect(page.getByText('Worldwide')).toBeVisible()
  await shoot(page, 'mobile-searches-empty')
})

test('mobile job searches list and form', async ({ page }) => {
  await page.setViewportSize(phone)
  await signedIn(page)
  await mockApi(page, { cvs: [sampleCV] })
  await mockJobSearchesApi(page, { searches: sampleSearches(), suggestions: sampleSuggestions() })
  await page.goto('/searches')
  await expect(page.getByText('StepStone · planned, you suggested it')).toBeVisible()
  await shoot(page, 'mobile-searches-list')
  await page.goto('/searches/new')
  await expect(page.getByTestId('searched-on')).toContainText('LinkedIn')
  await shoot(page, 'mobile-search-form')
})
