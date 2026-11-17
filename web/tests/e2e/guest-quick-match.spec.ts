import { expect, test, type Page } from '@playwright/test'
import {
  deferred,
  guestMatchErrors,
  mockApi,
  mockGuestMatch,
  pdfFile,
  sampleCV,
  sampleJobDescription,
  signedIn,
} from '../support/mockApi'

async function expectNoHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )
  expect(overflow).toBeLessThanOrEqual(0)
}

const fileInput = (page: Page) => page.getByTestId('cv-file-input')
const textarea = (page: Page) => page.getByLabel('Job description')
const submit = (page: Page) => page.getByRole('button', { name: 'Score this job' })

async function fillForm(page: Page) {
  await fileInput(page).setInputFiles(pdfFile())
  await textarea(page).fill(sampleJobDescription)
}

test('the sign-in page links to the guest form, which says the CV is not stored', async ({
  page,
}) => {
  await page.goto('/signin')
  await page.getByRole('link', { name: 'Try a quick match without an account' }).click()

  await expect(page).toHaveURL(/\/try$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Quick match' })).toBeVisible()
  await expect(page.getByText('Your CV is read to score this job and is not stored.')).toBeVisible()
  await expect(page.getByText('No PDF chosen yet')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Choose PDF' })).toBeVisible()
  await expect(textarea(page)).toBeVisible()
  await expect(page.getByRole('link', { name: 'Sign in' }).first()).toHaveAttribute(
    'href',
    '/signin',
  )
  await expect(page).toHaveTitle('Quick match · CV Evaluator')
  await expectNoHorizontalScroll(page)
})

test('missing fields and bad files are caught before calling the API', async ({ page }) => {
  const requests = await mockGuestMatch(page)
  await page.goto('/try')

  await submit(page).click()
  await expect(page.getByText('Choose your CV as a PDF first.')).toBeVisible()
  await expect(page.getByText('Paste a job description first.')).toBeVisible()
  await expect(textarea(page)).toHaveAttribute('aria-invalid', 'true')

  await fileInput(page).setInputFiles({
    name: 'cv.docx',
    mimeType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    buffer: Buffer.from('not a pdf'),
  })
  await expect(page.getByText(`"cv.docx" isn't a PDF.`)).toBeVisible()

  await fileInput(page).setInputFiles(pdfFile('huge.pdf', 5 * 1024 * 1024 + 1))
  await expect(page.getByText('"huge.pdf" is 5.0 MB. CVs can be up to 5 MB')).toBeVisible()

  await fileInput(page).setInputFiles(pdfFile())
  await expect(page.getByTestId('guest-file')).toContainText('jane-doe-2026.pdf')
  await expect(page.getByTestId('guest-file')).toContainText('2 KB')
  await expect(page.getByRole('button', { name: 'Change' })).toBeVisible()
  await submit(page).click()
  await expect(page.getByText('Paste a job description first.')).toBeVisible()

  expect(requests).toHaveLength(0)
})

// Runs on desktop and at 375px (the "mobile" project).
test('a short job description is caught before calling the API', async ({ page }) => {
  const requests = await mockGuestMatch(page)
  await page.goto('/try')
  const hint = page.getByTestId('guest-description-hint')
  await expect(hint).toHaveText('At least 40 words')

  await fileInput(page).setInputFiles(pdfFile())
  await textarea(page).fill('Senior Go engineer, remote, PostgreSQL and Kubernetes.')
  await expect(hint).toHaveText('At least 40 words · 7 so far')
  await submit(page).click()

  const error = page.getByText(
    'This job description is too short (7 words). Paste the full posting, at least 40 words.',
  )
  await expect(error).toBeVisible()
  await expect(textarea(page)).toHaveAttribute('aria-invalid', 'true')
  await expect(textarea(page)).toHaveAttribute('aria-describedby', 'guest-description-error')
  await expectNoHorizontalScroll(page)

  // 40 words but under 200 characters is still too short.
  await textarea(page).fill(Array.from({ length: 40 }, (_, i) => `w${i}`).join(' '))
  await expect(error).toBeHidden()
  await expect(hint).toHaveText('At least 40 words · 40 so far')
  await submit(page).click()
  await expect(page.getByText('This job description is too short (40 words).')).toBeVisible()
  expect(requests).toHaveLength(0)

  // The full posting goes through.
  await textarea(page).fill(sampleJobDescription)
  await submit(page).click()
  await expect(page.getByTestId('qm-result')).toBeVisible()
  expect(requests).toHaveLength(1)
})

test('scoring state, then the result with a sign-up call to action', async ({ page }) => {
  const hold = deferred()
  const requests = await mockGuestMatch(page, { gate: hold.promise })
  await page.goto('/try')
  await fillForm(page)
  await submit(page).click()

  await expect(page.getByRole('button', { name: 'Scoring…' })).toBeDisabled()
  await expect(page.getByTestId('qm-scoring')).toContainText('Scoring this job against your CV')
  await expect(textarea(page)).toHaveJSProperty('readOnly', true)

  hold.release()
  const result = page.getByTestId('qm-result')
  await expect(result).toBeVisible()
  await expect(
    result.getByRole('heading', { name: 'Product Engineer (Go, TypeScript)' }),
  ).toBeVisible()
  await expect(result).toContainText('68')
  await expect(result).toContainText('React and TypeScript')
  await expect(result.getByTestId('qm-requirements')).toBeVisible()

  const cta = page.getByTestId('guest-cta')
  await expect(cta).toContainText('Create an account to save your CV and get matches automatically')
  await expect(cta.getByRole('link', { name: 'Create an account' })).toHaveAttribute(
    'href',
    '/signup',
  )

  expect(requests).toHaveLength(1)
  const [sent] = requests
  expect(sent!.authorization).toBeNull()
  expect(sent!.contentType).toContain('multipart/form-data')
  expect(sent!.body).toContain('name="cv"; filename="jane-doe-2026.pdf"')
  expect(sent!.body).toContain('name="job_description"')
  expect(sent!.body).toContain('Product Engineer (Go, TypeScript)')
  await expectNoHorizontalScroll(page)

  // Another job reuses the chosen CV.
  await page.getByRole('button', { name: 'Score another job' }).click()
  await expect(page.getByTestId('guest-file')).toContainText('jane-doe-2026.pdf')
  await expect(textarea(page)).toHaveValue('')
})

test('a signed-in visitor is sent to the full quick match', async ({ page }) => {
  await signedIn(page)
  await mockApi(page, { cvs: [sampleCV] })
  await page.goto('/try')
  await expect(page).toHaveURL(/\/quick-match$/)
})

const limitCases = [
  {
    name: "the visitor's own limit",
    reply: guestMatchErrors.visitorLimit,
    text: 'You have used your guest matches for now (at most 2 per hour).',
  },
  {
    name: 'all guests together',
    reply: guestMatchErrors.busy,
    text: 'Guest matching is busy right now.',
  },
]

for (const c of limitCases) {
  test(`429 for ${c.name} shows the API's message and a sign-up link`, async ({ page }) => {
    await mockGuestMatch(page, { reply: c.reply })
    await page.goto('/try')
    await fillForm(page)
    await submit(page).click()

    const alert = page.getByTestId('guest-error')
    await expect(alert).toContainText(c.text)
    await expect(alert.getByRole('link', { name: 'Create an account' })).toHaveAttribute(
      'href',
      '/signup',
    )
    // The form keeps what was entered.
    await expect(textarea(page)).toHaveValue(sampleJobDescription)
    await expect(page.getByTestId('guest-file')).toContainText('jane-doe-2026.pdf')
  })
}

test('a PDF the API cannot read is reported on the CV field', async ({ page }) => {
  await mockGuestMatch(page, { reply: guestMatchErrors.invalidPDF })
  await page.goto('/try')
  await fillForm(page)
  await submit(page).click()

  await expect(page.getByText('We could not read text from this PDF.')).toBeVisible()
  await expect(page.getByText('No PDF chosen yet')).toBeVisible()
  await expect(textarea(page)).toHaveValue(sampleJobDescription)
})

test('a PDF over the server limit is reported on the CV field', async ({ page }) => {
  await mockGuestMatch(page, { reply: guestMatchErrors.tooLarge })
  await page.goto('/try')
  await fillForm(page)
  await submit(page).click()

  await expect(page.getByText('The CV must be a PDF of at most 5 MB.')).toBeVisible()
})

test('a server error keeps the form and says what happened', async ({ page }) => {
  await mockGuestMatch(page, {
    reply: {
      status: 500,
      body: {
        error: {
          type: 'api_error',
          code: 'internal_error',
          message: 'An unexpected error occurred.',
        },
      },
    },
  })
  await page.goto('/try')
  await fillForm(page)
  await submit(page).click()

  await expect(page.getByTestId('guest-error')).toContainText(
    "We couldn't score this job. An unexpected error occurred.",
  )
  await expect(page.getByTestId('guest-error').getByRole('link')).toHaveCount(0)
})
