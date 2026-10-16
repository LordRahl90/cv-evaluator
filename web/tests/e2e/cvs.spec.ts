import { expect, test, type Page } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { deferred, mockApi, mockCVStore, pdfFile, sampleCVs, signedIn } from '../support/mockApi'

test.beforeEach(async ({ page }) => {
  await signedIn(page)
})

const fileInput = (page: Page) => page.getByTestId('cv-file-input')

async function navTo(page: Page, name: string) {
  const toggle = page.getByRole('button', { name: 'Open menu' })
  if (await toggle.isVisible()) await toggle.click()
  await page.getByRole('navigation').getByRole('link', { name, exact: true }).click()
}

async function expectNoHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )
  expect(overflow).toBeLessThanOrEqual(0)
}

test('no CVs: drop zone, tips and a real file input', async ({ page }) => {
  await mockApi(page)
  await mockCVStore(page)
  await page.goto('/cvs')

  await expect(page.getByRole('heading', { level: 1, name: 'CVs' })).toBeVisible()
  const zone = page.getByRole('region', { name: 'Upload a CV' })
  await expect(zone.getByRole('heading', { name: 'Upload your first CV' })).toBeVisible()
  await expect(zone.getByRole('button', { name: 'Choose PDF' })).toBeVisible()
  await expect(zone).toContainText('PDF only · up to 5 MB')
  await expect(fileInput(page)).toHaveAttribute('accept', /application\/pdf/)
  await expect(page.getByText('Keep skills explicit')).toBeVisible()
  // No made-up file names, sizes or section chips.
  await expect(page.getByText(/\.pdf/)).toHaveCount(0)
  await expectNoHorizontalScroll(page)
})

test('the sidebar link opens the CVs page', async ({ page }) => {
  await mockApi(page)
  await mockCVStore(page)
  await page.goto('/')
  await navTo(page, 'CVs')
  await expect(page).toHaveURL('/cvs')
  await expect(page.getByRole('heading', { level: 1, name: 'CVs' })).toBeVisible()
})

test('upload: processing state while the request runs, then the list', async ({ page }) => {
  await mockApi(page)
  const hold = deferred()
  const store = await mockCVStore(page, { uploadGate: hold.promise })
  await page.goto('/cvs')

  await fileInput(page).setInputFiles(pdfFile())
  const progress = page.getByTestId('cv-upload-progress')
  await expect(progress).toBeVisible()
  await expect(progress).toContainText('Processing jane-doe-2026.pdf')
  await expect(page.getByRole('button', { name: 'Choose PDF' })).toHaveCount(0)

  // The app stays usable: go to another page and back while it runs.
  await navTo(page, 'Overview')
  await expect(page.getByRole('heading', { name: 'Welcome, Jane' })).toBeVisible()
  await navTo(page, 'CVs')
  await expect(page.getByTestId('cv-upload-progress')).toBeVisible()
  hold.release()

  await expect(page.getByTestId('cv-upload-success')).toContainText('CV uploaded')
  await expect(page.getByTestId('cv-upload-progress')).toHaveCount(0)
  await expect(page.getByRole('region', { name: 'Used for matching' })).toContainText(
    'CV from 16 Oct 2026',
  )
  await expect(page.getByRole('region', { name: 'All CVs' })).toContainText('Used for matching')
  expect(store.uploads[0]).toEqual({ field: 'cv', fileName: 'jane-doe-2026.pdf' })
})

test('upload from the list: a second CV becomes the one used for matching', async ({ page }) => {
  await mockApi(page)
  const hold = deferred()
  await mockCVStore(page, { cvs: sampleCVs(), uploadGate: hold.promise })
  await page.goto('/cvs')

  const table = page.getByRole('region', { name: 'All CVs' })
  await expect(table.getByText('Used for matching')).toHaveCount(1)
  await expect(page.getByRole('region', { name: 'Used for matching' })).toContainText(
    'CV from 12 Oct 2026',
  )

  await fileInput(page).setInputFiles(pdfFile('platform.pdf'))
  await expect(table.getByTestId('cv-upload-progress')).toContainText('Processing platform.pdf')
  await expect(page.getByRole('button', { name: 'Upload CV' })).toBeDisabled()
  hold.release()

  await expect(page.getByRole('region', { name: 'Used for matching' })).toContainText(
    'CV from 16 Oct 2026',
  )
  await expect(table.getByText('Older version')).toHaveCount(3)
  await expect(page.getByRole('button', { name: 'Upload CV' })).toBeEnabled()
})

test('client-side checks: not a PDF, too large, empty', async ({ page }) => {
  await mockApi(page)
  const store = await mockCVStore(page)
  await page.goto('/cvs')

  await fileInput(page).setInputFiles({
    name: 'cv.docx',
    mimeType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    buffer: Buffer.from('not a pdf'),
  })
  await expect(page.getByTestId('cv-upload-error')).toContainText(`"cv.docx" isn't a PDF`)

  await fileInput(page).setInputFiles(pdfFile('huge.pdf', 6 * 1024 * 1024))
  await expect(page.getByTestId('cv-upload-error')).toContainText(
    '"huge.pdf" is 6.0 MB. CVs can be up to 5 MB',
  )

  await fileInput(page).setInputFiles({
    name: 'empty.pdf',
    mimeType: 'application/pdf',
    buffer: Buffer.alloc(0),
  })
  await expect(page.getByTestId('cv-upload-error')).toContainText('"empty.pdf" is empty')

  expect(store.uploads).toHaveLength(0)
})

test('drag and drop a file onto the drop zone', async ({ page }) => {
  await mockApi(page)
  const store = await mockCVStore(page)
  await page.goto('/cvs')

  const zone = page.getByTestId('cv-drop-zone')
  const dataTransfer = await page.evaluateHandle(() => {
    const dt = new DataTransfer()
    dt.items.add(new File(['%PDF-1.4 test'], 'dropped.pdf', { type: 'application/pdf' }))
    return dt
  })
  await zone.dispatchEvent('dragenter', { dataTransfer })
  await expect(zone).toContainText('Drop your PDF to upload it')
  await zone.dispatchEvent('drop', { dataTransfer })

  await expect(page.getByTestId('cv-upload-success')).toBeVisible()
  expect(store.uploads[0]?.fileName).toBe('dropped.pdf')
})

test('an API error during upload is shown and the drop zone comes back', async ({ page }) => {
  await mockApi(page)
  await mockCVStore(page, {
    uploadReply: { status: 500, body: { error: 'could not extract text from pdf' } },
  })
  await page.goto('/cvs')

  await fileInput(page).setInputFiles(pdfFile('scan.pdf'))
  await expect(page.getByTestId('cv-upload-error')).toContainText(
    `We couldn't process "scan.pdf". Could not extract text from pdf`,
  )
  await expect(page.getByRole('button', { name: 'Choose PDF' })).toBeVisible()
})

test('a failed list load shows an error with retry', async ({ page }) => {
  await mockApi(page)
  let calls = 0
  await page.route('**/api/cv/', (route) => {
    calls += 1
    if (calls === 1) return route.fulfill({ status: 500, json: { error: 'database unavailable' } })
    return route.fulfill({ status: 200, json: sampleCVs() })
  })
  await page.goto('/cvs')
  await expect(page.getByRole('alert')).toContainText("We couldn't load your CVs.")
  await page.getByRole('button', { name: 'Try again' }).click()
  await expect(page.getByRole('region', { name: 'All CVs' })).toBeVisible()
})

test('download saves the extracted text', async ({ page }) => {
  await mockApi(page)
  await mockCVStore(page, { cvs: sampleCVs(), downloadText: 'Jane Doe\nGo engineer' })
  await page.goto('/cvs')

  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Download extracted text of CV from 12 Oct 2026' }).click()
  const download = await downloadPromise
  expect(download.suggestedFilename()).toBe('cv-2026-10-12.txt')
  expect(await readFile(await download.path(), 'utf8')).toBe('Jane Doe\nGo engineer')
})

test('a failed download shows an inline error', async ({ page }) => {
  await mockApi(page)
  await mockCVStore(page, {
    cvs: sampleCVs(),
    downloadReply: { status: 404, body: { error: 'cv not found' } },
  })
  await page.goto('/cvs')
  await page.getByRole('button', { name: 'Download extracted text of CV from 12 Oct 2026' }).click()
  await expect(page.getByRole('alert')).toContainText("We couldn't download that CV. Cv not found")
})

test('delete asks for confirmation; cancel keeps the CV', async ({ page }) => {
  await mockApi(page)
  const store = await mockCVStore(page, { cvs: sampleCVs() })
  await page.goto('/cvs')

  const trigger = page.getByRole('button', { name: 'Delete CV from 12 Oct 2026' })
  await trigger.click()
  const dialog = page.getByRole('dialog', { name: 'Delete this CV?' })
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('Matching will switch to your CV from 3 Sep 2026 instead.')
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused()

  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toBeHidden()
  await expect(trigger).toBeFocused()

  // Escape cancels too.
  await trigger.click()
  await expect(dialog).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()

  expect(store.deleted).toHaveLength(0)
  await expect(page.getByRole('region', { name: 'All CVs' }).getByRole('row')).toHaveCount(4)
})

test('delete with confirm removes the CV and moves "Used for matching"', async ({ page }) => {
  await mockApi(page)
  const store = await mockCVStore(page, { cvs: sampleCVs() })
  await page.goto('/cvs')

  await page.getByRole('button', { name: 'Delete CV from 12 Oct 2026' }).click()
  const dialog = page.getByRole('dialog', { name: 'Delete this CV?' })
  await dialog.getByRole('button', { name: 'Delete CV' }).click()
  await expect(dialog).toBeHidden()

  expect(store.deleted).toEqual(['01JA2B3C4D5E6F7G8H9J0K1CV1'])
  await expect(page.getByRole('button', { name: 'Delete CV from 12 Oct 2026' })).toHaveCount(0)
  await expect(page.getByRole('region', { name: 'Used for matching' })).toContainText(
    'CV from 3 Sep 2026',
  )
})

test('a failed delete keeps the dialog open with the error', async ({ page }) => {
  await mockApi(page)
  await mockCVStore(page, {
    cvs: sampleCVs(),
    deleteReply: { status: 500, body: { error: 'database unavailable' } },
  })
  await page.goto('/cvs')

  await page.getByRole('button', { name: 'Delete CV from 3 Sep 2026' }).click()
  const dialog = page.getByRole('dialog', { name: 'Delete this CV?' })
  await expect(dialog).toContainText('Matching keeps using your newest CV.')
  await dialog.getByRole('button', { name: 'Delete CV' }).click()
  await expect(dialog.getByRole('alert')).toContainText('Database unavailable')
  await expect(dialog).toBeVisible()
})

test('the CV list fits a 375px screen', async ({ page, viewport }) => {
  test.skip((viewport?.width ?? 0) > 400, 'phone layout only')
  await mockApi(page)
  await mockCVStore(page, { cvs: sampleCVs() })
  await page.goto('/cvs')
  await expect(page.getByRole('region', { name: 'All CVs' })).toBeVisible()
  await expectNoHorizontalScroll(page)
  await page.getByRole('button', { name: 'Delete CV from 12 Oct 2026' }).click()
  const box = await page.getByRole('dialog').boundingBox()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(375)
})
