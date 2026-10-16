// Captures the screens shown in pull requests. Run with `npm run screenshots`.
import { expect, test, type Page } from '@playwright/test'
import path from 'node:path'
import {
  apiError,
  deferred,
  mockApi,
  mockCVStore,
  mockQuickMatch,
  pdfFile,
  sampleCV,
  sampleCVs,
  sampleJobDescription,
  sampleSearches,
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

// CVs and Quick match

test('cvs empty', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  await mockApi(page)
  await mockCVStore(page)
  await page.goto('/cvs')
  await expect(page.getByTestId('cv-drop-zone')).toBeVisible()
  await shoot(page, 'cvs-empty')
  await page.getByTestId('cv-file-input').setInputFiles({
    name: 'cv.docx',
    mimeType: 'application/msword',
    buffer: Buffer.from('x'),
  })
  await expect(page.getByTestId('cv-upload-error')).toBeVisible()
  await shoot(page, 'cvs-validation-error')
})

test('cvs uploading', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  await mockApi(page)
  const hold = deferred()
  await mockCVStore(page, { uploadGate: hold.promise })
  await page.goto('/cvs')
  await page.getByTestId('cv-file-input').setInputFiles(pdfFile())
  await expect(page.getByTestId('cv-upload-progress')).toBeVisible()
  await shoot(page, 'cvs-uploading')
  hold.release()
  await expect(page.getByTestId('cv-upload-success')).toBeVisible()
  await shoot(page, 'cvs-uploaded')
})

test('cvs list and delete dialog', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  await mockApi(page)
  await mockCVStore(page, { cvs: sampleCVs() })
  await page.goto('/cvs')
  await expect(page.getByRole('region', { name: 'All CVs' })).toBeVisible()
  await shoot(page, 'cvs-list')
  await page.getByRole('button', { name: 'Delete CV from 12 Oct 2026' }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await shoot(page, 'cvs-delete-dialog', false)
})

test('quick match form, scoring and result', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  await mockApi(page, { cvs: [sampleCV] })
  const hold = deferred()
  await mockQuickMatch(page, { gate: hold.promise })
  await page.goto('/quick-match')
  await expect(page.getByLabel('Job description')).toBeVisible()
  await shoot(page, 'quick-match-empty')
  await page.getByLabel('Job description').fill(sampleJobDescription)
  await page.getByRole('button', { name: 'Score this job' }).click()
  await expect(page.getByTestId('qm-scoring')).toBeVisible()
  await shoot(page, 'quick-match-scoring')
  hold.release()
  await expect(page.getByTestId('qm-result')).toBeVisible()
  await shoot(page, 'quick-match-result')
})

test('quick match without a CV', async ({ page }) => {
  await page.setViewportSize(desktop)
  await signedIn(page)
  await mockApi(page)
  await page.goto('/quick-match')
  await expect(page.getByTestId('qm-no-cv')).toBeVisible()
  await shoot(page, 'quick-match-no-cv')
})

test('mobile cvs and quick match', async ({ page }) => {
  await page.setViewportSize(phone)
  await signedIn(page)
  await mockApi(page, { cvs: [sampleCV] })
  await mockCVStore(page, { cvs: sampleCVs() })
  await mockQuickMatch(page)
  await page.goto('/cvs')
  await expect(page.getByRole('region', { name: 'All CVs' })).toBeVisible()
  await shoot(page, 'mobile-cvs-list')
  await page.getByRole('button', { name: 'Delete CV from 12 Oct 2026' }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await shoot(page, 'mobile-cvs-delete-dialog', false)
  await page.getByRole('button', { name: 'Cancel' }).click()

  await page.goto('/quick-match')
  await page.getByLabel('Job description').fill(sampleJobDescription)
  await page.getByRole('button', { name: 'Score this job' }).click()
  await expect(page.getByTestId('qm-result')).toBeVisible()
  await shoot(page, 'mobile-quick-match-result')
})

test('mobile cvs empty', async ({ page }) => {
  await page.setViewportSize(phone)
  await signedIn(page)
  await mockApi(page)
  await mockCVStore(page)
  await page.goto('/cvs')
  await expect(page.getByTestId('cv-drop-zone')).toBeVisible()
  await shoot(page, 'mobile-cvs-empty')
})
