// The account menu at the top right: desktop top bar and the phone top bar.
import { expect, test, type Page } from '@playwright/test'
import { TOKEN_KEY, mockApi, signedIn } from '../support/mockApi'

const trigger = (page: Page) => page.getByRole('button', { name: 'Account menu' })
const menu = (page: Page) => page.getByRole('menu', { name: 'Account' })
const signOutItem = (page: Page) => page.getByRole('menuitem', { name: 'Sign out' })

test.beforeEach(async ({ page }) => {
  await signedIn(page)
  await mockApi(page)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Welcome, Jane' })).toBeVisible()
})

test('sits at the top right and shows who is signed in', async ({ page, viewport }) => {
  const button = trigger(page)
  await expect(button).toBeVisible()
  await expect(button).toContainText('JD')
  if ((viewport?.width ?? 0) > 400) await expect(button).toContainText('Jane Doe')

  const box = await button.boundingBox()
  const width = viewport?.width ?? 0
  expect(box).not.toBeNull()
  // Right edge of the screen, in the top bar.
  expect(box!.x + box!.width).toBeGreaterThan(width - 120)
  expect(box!.y).toBeLessThan(40)
})

test('opens on click with the email, and closes on a second click', async ({ page }) => {
  await expect(trigger(page)).toHaveAttribute('aria-haspopup', 'menu')
  await expect(trigger(page)).toHaveAttribute('aria-expanded', 'false')
  await expect(menu(page)).toBeHidden()

  await trigger(page).click()
  await expect(trigger(page)).toHaveAttribute('aria-expanded', 'true')
  await expect(menu(page)).toBeVisible()
  await expect(menu(page)).toContainText('Jane Doe')
  await expect(menu(page)).toContainText('jane.doe@example.com')
  await expect(signOutItem(page)).toBeFocused()

  await trigger(page).click()
  await expect(menu(page)).toBeHidden()
  await expect(trigger(page)).toHaveAttribute('aria-expanded', 'false')
})

test('closes on Escape and returns focus to the trigger', async ({ page }) => {
  await trigger(page).click()
  await expect(menu(page)).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(menu(page)).toBeHidden()
  await expect(trigger(page)).toBeFocused()
})

test('closes on a click outside', async ({ page }) => {
  await trigger(page).click()
  await expect(menu(page)).toBeVisible()
  // Bottom-left corner of the page, well away from the drop-down.
  await page.mouse.click(8, 500)
  await expect(menu(page)).toBeHidden()
  await expect(trigger(page)).toHaveAttribute('aria-expanded', 'false')
})

test('works from the keyboard', async ({ page }) => {
  await trigger(page).focus()
  await page.keyboard.press('Enter')
  await expect(menu(page)).toBeVisible()
  await expect(signOutItem(page)).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(trigger(page)).toBeFocused()

  await page.keyboard.press('ArrowDown')
  await expect(signOutItem(page)).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL('/signin')
})

test('signs out and clears the session', async ({ page }) => {
  await trigger(page).click()
  await signOutItem(page).click()

  await expect(page).toHaveURL('/signin')
  expect(await page.evaluate((key) => localStorage.getItem(key), TOKEN_KEY)).toBeNull()
  await page.goto('/')
  await expect(page).toHaveURL('/signin')
})

test('no sign out or user details in the sidebar', async ({ page }) => {
  const nav = page.getByRole('navigation')
  await expect(nav.getByText('jane.doe@example.com')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Sign out' })).toHaveCount(0)
})
