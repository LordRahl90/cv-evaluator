import { expect, test, type Page } from '@playwright/test'
import {
  coverLetterErrors,
  deferred,
  mockApi,
  mockCoverLetter,
  mockQuickMatch,
  sampleCV,
  sampleCoverLetter,
  sampleJobDescription,
  sampleMatch,
  sampleStrongJobDescription,
  sampleStrongMatch,
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

/** Scores a job and waits for the result. */
async function scoreJob(page: Page, strong = true) {
  await mockApi(page, { cvs: [sampleCV] })
  await mockQuickMatch(page, {
    reply: { status: 200, body: strong ? sampleStrongMatch : sampleMatch },
  })
  await page.goto('/quick-match')
  await page
    .getByLabel('Job description')
    .fill(strong ? sampleStrongJobDescription : sampleJobDescription)
  await page.getByRole('button', { name: 'Score this job' }).click()
  await expect(page.getByTestId('qm-result')).toBeVisible()
}

const banner = (page: Page) => page.getByTestId('cl-banner')
const panel = (page: Page) => page.getByTestId('cl-panel')
const letterField = (page: Page) =>
  panel(page).getByRole('textbox', { name: 'Letter (edit freely before copying)' })
const subjectField = (page: Page) => panel(page).getByLabel('Subject')

async function writeLetter(page: Page) {
  await banner(page).getByRole('button', { name: 'Write cover letter' }).click()
  await expect(letterField(page)).toHaveValue(sampleCoverLetter.letter)
}

test('a strong match (80+) offers a cover letter in a banner', async ({ page }) => {
  await mockCoverLetter(page)
  await scoreJob(page)

  await expect(
    banner(page).getByRole('heading', { name: 'Strong match: worth applying' }),
  ).toBeVisible()
  await expect(banner(page)).toContainText('No clichés, nothing invented.')
  await expect(banner(page).getByRole('button', { name: 'Write cover letter' })).toHaveClass(
    /btn-primary/,
  )
  await expect(page.getByTestId('cl-write')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Score another job' })).toBeVisible()
})

test('below 80 there is no banner, only a secondary button', async ({ page }) => {
  const bodies = await mockCoverLetter(page)
  await scoreJob(page, false)

  await expect(banner(page)).toHaveCount(0)
  const write = page.getByTestId('cl-write')
  await expect(write).toHaveText('Write cover letter')
  await expect(write).toHaveClass(/btn-secondary/)
  await write.click()
  await expect(letterField(page)).toHaveValue(sampleCoverLetter.letter)
  expect(bodies).toEqual([{ job_description: sampleJobDescription, evaluation: sampleMatch }])
})

test('writing: busy button with a timer, progress steps and the match below', async ({ page }) => {
  const hold = deferred()
  const bodies = await mockCoverLetter(page, { gate: hold.promise })
  await scoreJob(page)

  await banner(page).getByRole('button', { name: 'Write cover letter' }).click()
  await expect(panel(page)).toHaveAttribute('aria-busy', 'true')
  const busy = panel(page).getByRole('button', { name: /^Writing… \d+ s$/ })
  await expect(busy).toBeDisabled()
  await expect(panel(page).getByRole('button', { name: 'Writing… 1 s' })).toBeVisible()

  const steps = panel(page).getByRole('status', { name: 'Progress' }).getByRole('listitem')
  await expect(steps).toHaveText([
    /Picking the requirements you meet/,
    /Writing from your CV/,
    /Checking tone and facts/,
  ])
  await expect(panel(page)).toContainText('Usually 10–20 seconds.')
  await expect(panel(page)).toContainText('Senior Backend Engineer (Go)')

  // The match stays on the page as a summary; the page actions and banner go.
  const summary = page.getByTestId('cl-summary')
  await expect(summary).toContainText('Strong match')
  await expect(summary).toContainText('Strengths: 6 years of production Go')
  await expect(summary).toContainText('Missing: Terraform (nice to have)')
  await expect(page.getByRole('img', { name: 'Score 86 out of 100' })).toBeVisible()
  await expect(banner(page)).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Score another job' })).toHaveCount(0)

  expect(bodies).toEqual([
    { job_description: sampleStrongJobDescription, evaluation: sampleStrongMatch },
  ])
  hold.release()
  await expect(letterField(page)).toHaveValue(sampleCoverLetter.letter)
  await expect(panel(page)).not.toHaveAttribute('aria-busy', 'true')
})

test('ready: editable subject and letter, checks, word count and a not-saved notice', async ({
  page,
}) => {
  const bodies = await mockCoverLetter(page)
  await scoreJob(page)
  await writeLetter(page)

  await expect(subjectField(page)).toHaveValue(sampleCoverLetter.subject)
  const chips = panel(page).getByRole('list', { name: 'Checks passed' })
  await expect(chips).toContainText('Every claim is from your CV')
  await expect(chips).toContainText('Formal tone, no clichés')
  await expect(page.getByTestId('cl-words')).toHaveText('196 words')
  await expect(panel(page)).toContainText(
    "This letter isn't saved. Copy it before you leave this page; Regenerate replaces your edits.",
  )

  // The word count follows edits.
  await letterField(page).fill('Dear Hiring Manager,\n\nShort.\n\nKind regards,\n\nJane Doe')
  await expect(page.getByTestId('cl-words')).toHaveText('8 words')

  // Back to the full result and into the same letter again, without a new request.
  await page.getByRole('button', { name: 'Show full result' }).click()
  await expect(page.getByTestId('qm-result')).toBeVisible()
  await banner(page).getByRole('button', { name: 'Open cover letter' }).click()
  await expect(letterField(page)).toHaveValue(
    'Dear Hiring Manager,\n\nShort.\n\nKind regards,\n\nJane Doe',
  )
  expect(bodies).toHaveLength(1)
})

test('copy puts the subject and the edited letter on the clipboard', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await mockCoverLetter(page)
  await scoreJob(page)
  await writeLetter(page)

  await subjectField(page).fill('Application: Senior Backend Engineer')
  await letterField(page).fill('Dear Hiring Manager,\n\nEdited.\n\nKind regards,\n\nJane Doe')
  await panel(page).getByRole('button', { name: 'Copy' }).click()
  await expect(panel(page).getByRole('button', { name: 'Copied' })).toBeVisible()

  const copied = await page.evaluate(() => navigator.clipboard.readText())
  expect(copied).toBe(
    'Subject: Application: Senior Backend Engineer\n\nDear Hiring Manager,\n\nEdited.\n\nKind regards,\n\nJane Doe',
  )
  // "Copied" goes back to "Copy" after a moment.
  await expect(panel(page).getByRole('button', { name: 'Copy', exact: true })).toBeVisible({
    timeout: 4000,
  })
})

test('when the clipboard is blocked the text is selected for Ctrl/Cmd+C', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: () => Promise.reject(new Error('denied')) },
    })
  })
  await mockCoverLetter(page)
  await scoreJob(page)
  await writeLetter(page)

  await panel(page).getByRole('button', { name: 'Copy' }).click()
  const fallback = page.getByTestId('cl-copy-fallback')
  await expect(fallback).toContainText("Your browser didn't let us copy automatically.")
  await expect(fallback).toContainText(/press (Ctrl|Cmd)\+C to copy it/)
  const box = fallback.getByLabel('Letter to copy')
  await expect(box).toBeFocused()
  await expect(box).toHaveValue(
    `Subject: ${sampleCoverLetter.subject}\n\n${sampleCoverLetter.letter}`,
  )
  const selected = await box.evaluate((el: HTMLTextAreaElement) =>
    el.value.slice(el.selectionStart, el.selectionEnd),
  )
  expect(selected).toBe(`Subject: ${sampleCoverLetter.subject}\n\n${sampleCoverLetter.letter}`)
})

test('regenerate asks first when the letter was edited', async ({ page }) => {
  const second = { ...sampleCoverLetter, subject: 'Application for the Go role' }
  const bodies = await mockCoverLetter(page, {
    replies: [
      { status: 200, body: sampleCoverLetter },
      { status: 200, body: second },
    ],
  })
  await scoreJob(page)
  await writeLetter(page)

  await letterField(page).fill('My own edits')
  await panel(page).getByRole('button', { name: 'Regenerate' }).click()
  const dialog = page.getByRole('dialog', { name: 'Replace your edits?' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused()
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toBeHidden()
  await expect(letterField(page)).toHaveValue('My own edits')
  expect(bodies).toHaveLength(1)

  await panel(page).getByRole('button', { name: 'Regenerate' }).click()
  await dialog.getByRole('button', { name: 'Regenerate' }).click()
  await expect(subjectField(page)).toHaveValue('Application for the Go role')
  await expect(letterField(page)).toHaveValue(sampleCoverLetter.letter)
  expect(bodies).toHaveLength(2)

  // Unedited: no question.
  await panel(page).getByRole('button', { name: 'Regenerate' }).click()
  await expect(dialog).toBeHidden()
  await expect.poll(() => bodies.length).toBe(3)
})

const errorCases = [
  {
    name: 'no CV (422)',
    reply: coverLetterErrors.noCV,
    text: 'You need a CV before we can write a letter.',
    retry: false,
  },
  {
    name: 'rate limited (429)',
    reply: coverLetterErrors.rateLimited,
    text: "You've asked for several letters in the last minute. Wait a minute, then try again.",
    retry: true,
  },
  {
    name: 'quality checks failed (502)',
    reply: coverLetterErrors.failed,
    text: "We couldn't write a letter that passed our quality checks this time. Try again: each attempt is written from scratch.",
    retry: true,
  },
  {
    name: 'unexpected (500)',
    reply: {
      status: 500,
      body: {
        error: {
          type: 'api_error',
          code: 'internal_error',
          message: 'An unexpected error occurred. Please try again later.',
        },
      },
    },
    text: "We couldn't write the cover letter. An unexpected error occurred. Please try again later.",
    retry: true,
  },
]

for (const c of errorCases) {
  test(`error: ${c.name}`, async ({ page }) => {
    const bodies = await mockCoverLetter(page, {
      replies: [c.reply, { status: 200, body: sampleCoverLetter }],
    })
    await scoreJob(page)
    await banner(page).getByRole('button', { name: 'Write cover letter' }).click()

    const error = page.getByTestId('cl-error')
    await expect(error).toContainText(c.text)
    await expect(error).toHaveAttribute('role', 'alert')
    const retry = panel(page).getByRole('button', { name: 'Try again' })
    if (c.retry) {
      await retry.click()
      await expect(letterField(page)).toHaveValue(sampleCoverLetter.letter)
      expect(bodies).toHaveLength(2)
    } else {
      await expect(retry).toHaveCount(0)
      await error.getByRole('link', { name: 'Upload a CV' }).click()
      await expect(page).toHaveURL('/cvs')
    }
  })
}

test('a failed regeneration keeps the current letter', async ({ page }) => {
  await mockCoverLetter(page, {
    replies: [{ status: 200, body: sampleCoverLetter }, coverLetterErrors.failed],
  })
  await scoreJob(page)
  await writeLetter(page)

  await panel(page).getByRole('button', { name: 'Regenerate' }).click()
  await expect(page.getByTestId('cl-error')).toContainText('Your current letter is unchanged.')
  await expect(letterField(page)).toHaveValue(sampleCoverLetter.letter)
})

test('cover letter states fit a 375px screen', async ({ page, viewport }) => {
  test.skip((viewport?.width ?? 0) > 400, 'phone layout only')
  const hold = deferred()
  await mockCoverLetter(page, { gate: hold.promise })
  await scoreJob(page)
  await expect(banner(page)).toBeVisible()
  await expectNoHorizontalScroll(page)

  await banner(page).getByRole('button', { name: 'Write cover letter' }).click()
  await expect(panel(page).getByRole('button', { name: /^Writing…/ })).toBeVisible()
  await expectNoHorizontalScroll(page)

  hold.release()
  await expect(letterField(page)).toHaveValue(sampleCoverLetter.letter)
  await expectNoHorizontalScroll(page)
})
