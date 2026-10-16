import { expect, test, type Page } from '@playwright/test'
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

test.beforeEach(async ({ page }) => {
  await signedIn(page)
})

const card = (page: Page, name: string) =>
  page.getByRole('article').filter({ has: page.getByRole('heading', { name, exact: true }) })

test('empty state lists the supported portals and links to the suggestion form', async ({
  page,
}) => {
  await mockApi(page)
  await mockJobSearchesApi(page)
  await page.goto('/searches')

  const empty = page.getByTestId('searches-empty')
  await expect(empty.getByRole('heading', { name: 'No job searches yet' })).toBeVisible()
  await expect(empty.getByText('LinkedIn')).toBeVisible()
  await expect(empty.getByText('Worldwide')).toBeVisible()
  await expect(empty.getByText('Jobindex')).toBeVisible()
  await expect(empty.getByText('Denmark')).toBeVisible()
  // No CV yet, so the scoring note shows.
  await expect(page.getByText(/You haven't uploaded one yet/)).toBeVisible()

  await empty.getByRole('link', { name: /Missing a job site/ }).click()
  await expect(page).toHaveURL('/searches#suggest')
  await expect(page.locator('#suggest')).toBeFocused()
  await expect(page.locator('#suggest')).toBeInViewport()

  await empty.getByRole('link', { name: 'Create your first search' }).click()
  await expect(page).toHaveURL('/searches/new')
})

test('the CV note is hidden once a CV is uploaded', async ({ page }) => {
  await mockApi(page, { cvs: [sampleCV] })
  await mockJobSearchesApi(page)
  await page.goto('/searches')
  await expect(page.getByTestId('searches-empty')).toBeVisible()
  await expect(page.getByText(/You haven't uploaded one yet/)).toHaveCount(0)
})

test('loading skeleton, then the list with schedule details', async ({ page }) => {
  const hold = deferred()
  await mockApi(page)
  await mockJobSearchesApi(page, { searches: sampleSearches(), gate: hold.promise })
  await page.goto('/searches')
  await expect(page.getByTestId('searches-skeleton').first()).toBeVisible()
  hold.release()

  await expect(
    page.getByText('3 searches · 2 running · checked on their own schedule'),
  ).toBeVisible()
  const go = card(page, 'Senior Go · Copenhagen')
  await expect(go).toContainText('"senior go engineer" in Copenhagen')
  await expect(go).toContainText('Daily')
  await expect(go).toContainText('Running')
  await expect(go).toContainText(/Last run (\d\d:\d\d today|yesterday \d\d:\d\d)/)
  await expect(go).toContainText(/Next \d\d:\d\d (today|tomorrow)/)

  const backend = card(page, 'Backend · Remote EU')
  await expect(backend).toContainText('Last run not yet')

  const paused = card(page, 'Go contractor · Berlin')
  await expect(paused).toContainText('Paused')
  await expect(paused.getByRole('button', { name: 'Run now' })).toHaveCount(0)
  await expect(paused.getByRole('switch')).not.toBeChecked()
  // Nothing the API doesn't provide: no match counts or per-portal status.
  await expect(page.getByText(/matches/)).toHaveCount(0)
})

test('a failed load shows an error and can retry', async ({ page }) => {
  await mockApi(page)
  let calls = 0
  await mockJobSearchesApi(page, {
    searches: sampleSearches(),
    override: (method, path) => {
      if (method === 'GET' && path === '/job-searches' && calls++ === 0) {
        return { status: 500, body: apiError('api_error', 'internal_error', 'Database down.') }
      }
      return undefined
    },
  })
  await page.goto('/searches')
  await expect(page.getByRole('alert')).toContainText('Database down.')
  await page.getByRole('button', { name: 'Try again' }).click()
  await expect(card(page, 'Senior Go · Copenhagen')).toBeVisible()
})

test('the switch pauses and resumes a search', async ({ page }) => {
  await mockApi(page)
  const api = await mockJobSearchesApi(page, { searches: sampleSearches() })
  await page.goto('/searches')

  const go = card(page, 'Senior Go · Copenhagen')
  const toggle = go.getByRole('switch', { name: 'Run Senior Go · Copenhagen on schedule' })
  await expect(toggle).toBeChecked()
  await toggle.click({ force: true })
  await expect(toggle).not.toBeChecked()
  await expect(go).toContainText('Paused')
  await expect(page.getByText('3 searches · 1 running')).toBeVisible()
  expect(api.calls.at(-1)).toEqual({
    method: 'PATCH',
    path: '/job-searches/01JA2B3C4D5E6F7G8H9J0KS001',
    body: { enabled: false },
  })

  const paused = card(page, 'Go contractor · Berlin')
  await paused.getByRole('switch').click({ force: true })
  await expect(paused.getByRole('switch')).toBeChecked()
  await expect(paused).toContainText('Next due now')
  await expect(paused.getByRole('button', { name: 'Run now' })).toBeVisible()
})

test('the switch rolls back when the API refuses', async ({ page }) => {
  await mockApi(page)
  const hold = deferred()
  await mockJobSearchesApi(page, {
    searches: sampleSearches(),
    override: (method) =>
      method === 'PATCH'
        ? { status: 500, body: apiError('api_error', 'internal_error', 'Try again later.') }
        : undefined,
  })
  // Hold the PATCH so the optimistic state is visible first.
  await page.route('**/api/job-searches/*', async (route) => {
    if (route.request().method() === 'PATCH') await hold.promise
    return route.fallback()
  })
  await page.goto('/searches')

  const go = card(page, 'Senior Go · Copenhagen')
  const toggle = go.getByRole('switch')
  await toggle.click({ force: true })
  await expect(toggle).not.toBeChecked()
  await expect(toggle).toBeDisabled()
  hold.release()
  await expect(toggle).toBeChecked()
  await expect(go.getByRole('alert')).toContainText("Couldn't pause this search. Try again later.")
  await expect(go).toContainText('Running')
})

test('run now queues the search', async ({ page }) => {
  await mockApi(page)
  const api = await mockJobSearchesApi(page, { searches: sampleSearches() })
  await page.goto('/searches')
  const go = card(page, 'Senior Go · Copenhagen')
  await go.getByRole('button', { name: 'Run now' }).click()
  await expect(go.getByRole('status')).toContainText('Queued.')
  await expect(go).toContainText('Next due now')
  expect(api.calls.at(-1)?.path).toBe('/job-searches/01JA2B3C4D5E6F7G8H9J0KS001/run')
})

test('run now on a search paused elsewhere (409) explains and shows it paused', async ({
  page,
}) => {
  await mockApi(page)
  await mockJobSearchesApi(page, {
    searches: sampleSearches(),
    override: (_method, path) =>
      path.endsWith('/run')
        ? {
            status: 409,
            body: apiError(
              'invalid_request_error',
              'job_search_disabled',
              'The job search is disabled. Enable it before running it.',
            ),
          }
        : undefined,
  })
  await page.goto('/searches')
  const go = card(page, 'Senior Go · Copenhagen')
  await go.getByRole('button', { name: 'Run now' }).click()
  await expect(go.getByRole('alert')).toContainText("This search is paused, so it can't run.")
  await expect(go.getByRole('switch')).not.toBeChecked()
  await expect(go).toContainText('Paused')
})

test('delete asks first; cancel keeps the search, confirm removes it', async ({ page }) => {
  await mockApi(page)
  const api = await mockJobSearchesApi(page, { searches: sampleSearches() })
  await page.goto('/searches')

  const go = card(page, 'Senior Go · Copenhagen')
  const menu = go.getByRole('button', { name: 'More actions for Senior Go · Copenhagen' })
  await menu.click()
  await page.getByRole('menuitem', { name: 'Delete' }).click()
  await expect(go.getByText(/can't be restored/)).toBeVisible()
  await expect(go.getByRole('button', { name: 'Cancel' })).toBeFocused()
  await go.getByRole('button', { name: 'Cancel' }).click()
  await expect(go.getByText(/can't be restored/)).toHaveCount(0)
  await expect(menu).toBeFocused()
  expect(api.calls).toHaveLength(0)

  await menu.click()
  await page.getByRole('menuitem', { name: 'Delete' }).click()
  await go.getByRole('button', { name: 'Delete search' }).click()
  await expect(go).toHaveCount(0)
  await expect(page.getByTestId('searches-notice')).toContainText('Deleted Senior Go · Copenhagen.')
  await expect(page.getByRole('heading', { name: 'Job searches', level: 1 })).toBeFocused()
  expect(api.calls.at(-1)?.method).toBe('DELETE')
})

test('the actions menu works from the keyboard and opens the edit form', async ({ page }) => {
  await mockApi(page)
  await mockJobSearchesApi(page, { searches: sampleSearches() })
  await page.goto('/searches')
  const menu = page.getByRole('button', { name: 'More actions for Backend · Remote EU' })
  await menu.focus()
  await page.keyboard.press('ArrowDown')
  await expect(page.getByRole('menuitem', { name: 'Edit' })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(menu).toBeFocused()
  await page.keyboard.press('Enter')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL('/searches/01JA2B3C4D5E6F7G8H9J0KS002/edit')
})

test.describe('create', () => {
  test('validates, then creates and returns to the list', async ({ page }) => {
    await mockApi(page)
    const api = await mockJobSearchesApi(page)
    await page.goto('/searches/new')

    await expect(page.getByRole('heading', { name: 'New job search' })).toBeVisible()
    const searchedOn = page.getByTestId('searched-on')
    await expect(searchedOn).toContainText('Jobindex')
    await expect(searchedOn).toContainText('LinkedIn')
    await expect(page.getByTestId('next-step-fetch')).toHaveText(
      'From every portal we support, Jobindex and LinkedIn, now and then daily.',
    )

    await page.getByRole('button', { name: 'Create search' }).click()
    await expect(page.getByText('Enter what to search for')).toBeVisible()
    await expect(page.getByLabel('Keywords')).toBeFocused()
    await expect(page.getByLabel('Keywords')).toHaveAttribute('aria-invalid', 'true')
    expect(api.calls).toHaveLength(0)

    await page.getByLabel('Keywords').fill('Senior Go Engineer')
    await page.getByLabel('Location').fill('Copenhagen')
    await page.getByText('Weekly', { exact: true }).click()
    await expect(page.getByRole('radio', { name: 'Weekly' })).toBeChecked()
    await expect(page.getByTestId('next-step-fetch')).toContainText('now and then weekly')
    await page.getByRole('button', { name: 'Create search' }).click()

    await expect(page).toHaveURL('/searches')
    await expect(page.getByTestId('searches-notice')).toContainText(
      'Created Senior Go Engineer. Its first run starts in a moment.',
    )
    await expect(card(page, 'Senior Go Engineer')).toContainText('Weekly')
    expect(api.calls.at(-1)).toEqual({
      method: 'POST',
      path: '/job-searches',
      body: {
        query: 'Senior Go Engineer',
        location: 'Copenhagen',
        frequency: 'weekly',
        enabled: true,
      },
    })
  })

  test('unticking "run right away" saves the search paused', async ({ page }) => {
    await mockApi(page)
    const api = await mockJobSearchesApi(page)
    await page.goto('/searches/new')
    await page.getByLabel('Name').fill('Later')
    await page.getByLabel('Keywords').fill('go')
    await page.getByLabel('Run the first search right away').uncheck()
    await expect(page.getByText("It's saved paused.")).toBeVisible()
    await page.getByRole('button', { name: 'Create search' }).click()
    await expect(page.getByTestId('searches-notice')).toContainText("It's paused")
    expect((api.calls.at(-1)?.body as { enabled: boolean; name: string }).enabled).toBe(false)
    expect((api.calls.at(-1)?.body as { name: string }).name).toBe('Later')
  })

  test('shows API field errors on the matching field', async ({ page }) => {
    await mockApi(page)
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
    await page.getByLabel('Keywords').fill('go')
    await page.getByLabel('Location').fill('Somewhere')
    await page.getByRole('button', { name: 'Create search' }).click()
    await expect(page.getByLabel('Location')).toBeFocused()
    await expect(page.getByLabel('Location')).toHaveAttribute('aria-invalid', 'true')
    await expect(page.locator('#location-error')).toHaveText(
      'The location parameter must be at most 200 characters long.',
    )
    await expect(page).toHaveURL('/searches/new')
  })

  test('catches over-long fields before sending', async ({ page }) => {
    await mockApi(page)
    const api = await mockJobSearchesApi(page)
    await page.goto('/searches/new')
    await page.getByLabel('Name').fill('x'.repeat(101))
    await page.getByLabel('Keywords').fill('go')
    await page.getByRole('button', { name: 'Create search' }).click()
    await expect(page.locator('#name-error')).toContainText('100 characters or fewer')
    expect(api.calls).toHaveLength(0)
  })
})

test.describe('edit', () => {
  test('loads the search, saves changes with PATCH', async ({ page }) => {
    await mockApi(page)
    const api = await mockJobSearchesApi(page, { searches: sampleSearches() })
    await page.goto('/searches/01JA2B3C4D5E6F7G8H9J0KS001/edit')

    await expect(page.getByRole('heading', { name: 'Edit job search' })).toBeVisible()
    await expect(page.getByLabel('Name')).toHaveValue('Senior Go · Copenhagen')
    await expect(page.getByLabel('Keywords')).toHaveValue('senior go engineer')
    await expect(page.getByRole('radio', { name: 'Daily' })).toBeChecked()
    await expect(page.getByLabel('Run the first search right away')).toHaveCount(0)

    await page.getByLabel('Location').fill('Aarhus')
    await page.getByText('Hourly', { exact: true }).click()
    await page.getByRole('button', { name: 'Save changes' }).click()
    await expect(page).toHaveURL('/searches')
    await expect(page.getByTestId('searches-notice')).toContainText(
      'Saved changes to Senior Go · Copenhagen.',
    )
    await expect(card(page, 'Senior Go · Copenhagen')).toContainText('in Aarhus')
    expect(api.calls.at(-1)).toEqual({
      method: 'PATCH',
      path: '/job-searches/01JA2B3C4D5E6F7G8H9J0KS001',
      body: {
        name: 'Senior Go · Copenhagen',
        query: 'senior go engineer',
        location: 'Aarhus',
        frequency: 'hourly',
      },
    })
  })

  test('a missing search shows a not-found message', async ({ page }) => {
    await mockApi(page)
    await mockJobSearchesApi(page)
    await page.goto('/searches/01JA2B3C4D5E6F7G8H9J0KS999/edit')
    await expect(page.getByRole('alert')).toContainText("We couldn't find this search.")
    await expect(page.getByRole('link', { name: 'Back to job searches' })).toBeVisible()
  })
})

test.describe('portal suggestions', () => {
  test('lists live portals and your suggestions with their status', async ({ page }) => {
    await mockApi(page)
    await mockJobSearchesApi(page, { searches: sampleSearches(), suggestions: sampleSuggestions() })
    await page.goto('/searches')
    const section = page.getByRole('region', { name: 'Job portals' })
    const chips = section.getByRole('list', { name: 'Portals and your suggestions' })
    await expect(chips.getByRole('listitem')).toHaveText([
      'Jobindex · live',
      'LinkedIn · live',
      'StepStone · planned, you suggested it',
      'The Hub · submitted 2 days ago',
    ])
  })

  test('suggesting a portal adds it to the list', async ({ page }) => {
    await mockApi(page)
    const api = await mockJobSearchesApi(page, { searches: sampleSearches() })
    await page.goto('/searches')
    const section = page.getByRole('region', { name: 'Job portals' })

    await section.getByRole('button', { name: 'Suggest portal' }).click()
    await expect(section.locator('#portal-url-error')).toContainText("Enter the portal's website")
    await section.getByLabel('Website').fill('not a url')
    await section.getByRole('button', { name: 'Suggest portal' }).click()
    await expect(section.locator('#portal-url-error')).toContainText('Enter a web address')
    expect(api.calls).toHaveLength(0)

    await section.getByLabel('Portal name').fill('Indeed')
    await section.getByLabel('Website').fill('indeed.com')
    await section.getByRole('button', { name: 'Suggest portal' }).click()
    await expect(section.getByRole('status')).toContainText("We've added Indeed")
    await expect(section.getByRole('listitem').filter({ hasText: 'Indeed' })).toContainText(
      'submitted just now',
    )
    await expect(section.getByLabel('Website')).toHaveValue('')
    expect(api.calls.at(-1)).toEqual({
      method: 'POST',
      path: '/portal-suggestions',
      body: { url: 'indeed.com', name: 'Indeed' },
    })
  })

  for (const [code, message, friendly] of [
    [
      'portal_already_suggested',
      'You have already suggested this portal as 01JA3C4D5E6F7G8H9J0K1M2N3P.',
      "You've already suggested this portal. It's in your list above.",
    ],
    [
      'portal_already_supported',
      'This portal is already searched. See GET /providers.',
      'We already search this portal, so every search includes it.',
    ],
  ]) {
    test(`409 ${code} shows a friendly message on the website field`, async ({ page }) => {
      await mockApi(page)
      await mockJobSearchesApi(page, {
        searches: sampleSearches(),
        override: (method, path) =>
          method === 'POST' && path === '/portal-suggestions'
            ? { status: 409, body: apiError('invalid_request_error', code, message, 'url') }
            : undefined,
      })
      await page.goto('/searches')
      const section = page.getByRole('region', { name: 'Job portals' })
      await section.getByLabel('Website').fill('https://www.linkedin.com')
      await section.getByRole('button', { name: 'Suggest portal' }).click()
      await expect(section.locator('#portal-url-error')).toHaveText(friendly)
      await expect(section.getByLabel('Website')).toBeFocused()
      await expect(page.getByText(message)).toHaveCount(0)
    })
  }
})

test('dashboard links lead to the new search form and the list', async ({ page }) => {
  await mockApi(page, { cvs: [sampleCV], searches: sampleSearches() })
  await mockJobSearchesApi(page, { searches: sampleSearches() })
  await page.goto('/')
  await page.getByRole('link', { name: 'Manage' }).click()
  await expect(page).toHaveURL('/searches')
  await page.goto('/')
  await page.getByRole('link', { name: 'New search' }).click()
  await expect(page).toHaveURL('/searches/new')
  await expect(page.getByRole('heading', { name: 'New job search' })).toBeVisible()
})
