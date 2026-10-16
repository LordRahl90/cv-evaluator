// The signed-in user and their token. A small reactive module instead of a
// state library: views read `session.user`, call signIn/signUp/signOut.
import { reactive, readonly } from 'vue'
import * as authApi from '../api/auth'
import type { SignUpDetails } from '../api/auth'
import { clearToken, getToken, setToken } from '../api/token'
import type { Session, SignInRequest, User } from '../api/types'

interface SessionState {
  token: string | null
  user: User | null
  loadingUser: boolean
  userError: Error | null
}

const state = reactive<SessionState>({
  token: getToken(),
  user: null,
  loadingUser: false,
  userError: null,
})

let pendingUser: Promise<User | null> | null = null

export const session = readonly(state)

export function isSignedIn(): boolean {
  return Boolean(state.token)
}

function start({ token, user }: Session): void {
  setToken(token)
  state.token = token
  state.user = user
  state.userError = null
}

export async function signIn(credentials: SignInRequest): Promise<void> {
  start(await authApi.signIn(credentials))
}

export async function signUp(details: SignUpDetails): Promise<void> {
  start(await authApi.signUp(details))
}

/** Replaces the signed-in user, e.g. after the profile was edited. */
export function setUser(user: User): void {
  state.user = user
  state.userError = null
}

/**
 * Swaps in a new session, e.g. the fresh token a password change returns
 * (the old one no longer works).
 */
export function renewSession(next: Session): void {
  start(next)
}

export function signOut(): void {
  clearToken()
  state.token = null
  state.user = null
  state.userError = null
  pendingUser = null
}

/** Loads GET /me once per session; concurrent callers share the request. */
export function loadUser({ force = false } = {}): Promise<User | null> {
  if (!state.token) return Promise.resolve(null)
  if (state.user && !force) return Promise.resolve(state.user)
  if (pendingUser) return pendingUser

  state.loadingUser = true
  state.userError = null
  const pending: Promise<User | null> = authApi
    .getMe()
    .then((user) => {
      state.user = user
      return user
    })
    .catch((err: Error) => {
      state.userError = err
      throw err
    })
    .finally(() => {
      state.loadingUser = false
      if (pendingUser === pending) pendingUser = null
    })
  pendingUser = pending
  return pending
}
