// Where the session token lives.
//
// Trade-off: localStorage survives reloads and new tabs, which keeps the app
// simple, but any script running on the page can read it, so an XSS bug would
// leak the token. An httpOnly cookie set by the API would avoid that, at the
// cost of CSRF protection and cookie handling on the Go side. Revisit when the
// API grows a cookie-based session.
const KEY = 'cv-evaluator.token'

export function getToken(): string | null {
  try {
    return localStorage.getItem(KEY)
  } catch {
    return null
  }
}

export function setToken(token: string): void {
  try {
    localStorage.setItem(KEY, token)
  } catch {
    // Storage can be unavailable (private mode, blocked site data); the
    // session then lasts only until the page reloads.
  }
}

export function clearToken(): void {
  try {
    localStorage.removeItem(KEY)
  } catch {
    // Nothing stored, nothing to clear.
  }
}
