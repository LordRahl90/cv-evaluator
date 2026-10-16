import { expect, test } from '@playwright/test'
import {
  TOKEN_KEY,
  apiError,
  deferred,
  mockApi,
  sampleCV,
  sampleSearches,
  session,
  signedIn,
} from '../support/mockApi'

const storedToken = (page: import('@playwright/test').Page) =>
  page.evaluate((key) => window.localStorage.getItem(key), TOKEN_KEY)

test.describe('sign up', () => {
  test('creates the account and lands on the overview', async ({ page }) => {
    await mockApi(page)
    let body: Record<string, unknown> | null = null
    await page.route('**/api/auth/signup', async (route) => {
      body = route.request().postDataJSON()
      await route.fulfill({ status: 201, json: session() })
    })

    await page.goto('/signup')
    await expect(page.getByRole('heading', { name: 'Create your account' })).toBeVisible()
    await page.getByLabel('First name').fill('Jane')
    await page.getByLabel('Last name').fill('Doe')
    await page.getByLabel('Email').fill('jane.doe@example.com')
    await page.getByLabel('Password').fill('password123')
    await page.getByRole('button', { name: 'Create account' }).click()

    await expect(page).toHaveURL('/')
    await expect(page.getByRole('heading', { name: 'Welcome, Jane' })).toBeVisible()
    expect(body).toEqual({
      first_name: 'Jane',
      last_name: 'Doe',
      email: 'jane.doe@example.com',
      password: 'password123',
    })
    expect(await storedToken(page)).toBe('new-token')
  })

  test('shows "email already used" next to the email field', async ({ page }) => {
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
    await page.getByLabel('Email').fill('jane.doe@example.com')
    await page.getByLabel('Password').fill('password123')
    await page.getByRole('button', { name: 'Create account' }).click()

    await expect(page.getByText('An account with this email already exists.')).toBeVisible()
    await expect(page.getByLabel('Email')).toHaveAttribute('aria-invalid', 'true')
    await expect(page).toHaveURL('/signup')
    expect(await storedToken(page)).toBeNull()
  })

  test('shows a validation error from the API on the password field', async ({ page }) => {
    await page.route('**/api/auth/signup', (route) =>
      route.fulfill({
        status: 400,
        json: apiError(
          'invalid_request_error',
          'parameter_invalid',
          'The password must be at least 8 characters long.',
          'password',
        ),
      }),
    )

    await page.goto('/signup')
    await page.getByLabel('Email').fill('jane.doe@example.com')
    await page.getByLabel('Password').fill('short')
    await page.getByRole('button', { name: 'Create account' }).click()

    await expect(page.getByText('The password must be at least 8 characters long.')).toBeVisible()
    await expect(page.getByLabel('Password')).toHaveAttribute('aria-invalid', 'true')
  })
})

test.describe('sign in', () => {
  test('signs in, shows a busy button meanwhile, and opens the overview', async ({ page }) => {
    await mockApi(page, { cvs: [sampleCV], searches: sampleSearches() })
    const hold = deferred()
    await page.route('**/api/auth/signin', async (route) => {
      await hold.promise
      await route.fulfill({ status: 200, json: session() })
    })

    await page.goto('/signin')
    await page.getByLabel('Email').fill('jane.doe@example.com')
    await page.getByLabel('Password').fill('password123')
    await page.getByRole('button', { name: 'Sign in' }).click()

    const busy = page.getByRole('button', { name: 'Signing in…' })
    await expect(busy).toBeDisabled()
    await expect(busy).toHaveAttribute('aria-busy', 'true')
    hold.release()

    await expect(page).toHaveURL('/')
    await expect(page.getByRole('heading', { level: 1, name: /Jane$/ })).toBeVisible()
    expect(await storedToken(page)).toBe('new-token')
  })

  test('shows invalid credentials inline and stays on the page', async ({ page }) => {
    await page.route('**/api/auth/signin', (route) =>
      route.fulfill({
        status: 401,
        json: apiError(
          'authentication_error',
          'invalid_credentials',
          'The email or password is incorrect.',
        ),
      }),
    )

    await page.goto('/signin')
    await page.getByLabel('Email').fill('jane.doe@example.com')
    await page.getByLabel('Password').fill('wrong-password')
    await page.getByRole('button', { name: 'Sign in' }).click()

    await expect(page.getByRole('alert')).toHaveText('The email or password is incorrect.')
    await expect(page).toHaveURL('/signin')
    await expect(page.getByRole('button', { name: 'Sign in' })).toBeEnabled()
  })

  test('shows an error when the server cannot be reached', async ({ page }) => {
    await page.route('**/api/auth/signin', (route) => route.abort('connectionrefused'))

    await page.goto('/signin')
    await page.getByLabel('Email').fill('jane.doe@example.com')
    await page.getByLabel('Password').fill('password123')
    await page.getByRole('button', { name: 'Sign in' }).click()

    await expect(page.getByRole('alert')).toContainText("Can't reach the server")
  })
})

test.describe('route guard', () => {
  test('sends a signed-out visitor to sign in, then back where they were going', async ({
    page,
  }) => {
    await mockApi(page)
    await page.route('**/api/auth/signin', (route) =>
      route.fulfill({ status: 200, json: session() }),
    )

    await page.goto('/settings')
    await expect(page).toHaveURL('/signin?redirect=/settings')
    await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible()

    await page.getByLabel('Email').fill('jane.doe@example.com')
    await page.getByLabel('Password').fill('password123')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page).toHaveURL('/settings')
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()
  })

  test('the overview needs a session too', async ({ page }) => {
    await page.goto('/')
    await expect(page).toHaveURL('/signin')
  })

  test('an expired session signs out and redirects to sign in', async ({ page }) => {
    await signedIn(page, 'expired-token')
    await mockApi(page, { authStatus: 401 })

    await page.goto('/')
    await expect(page).toHaveURL(/\/signin\?redirect=\/&expired=1$/)
    await expect(page.getByRole('status')).toContainText('Your session has ended')
    expect(await storedToken(page)).toBeNull()
  })

  test('a signed-in user visiting sign in goes to the overview', async ({ page }) => {
    await signedIn(page)
    await mockApi(page)
    await page.goto('/signin')
    await expect(page).toHaveURL('/')
  })
})
