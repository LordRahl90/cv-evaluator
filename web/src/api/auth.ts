// Account endpoints: POST /auth/signup, POST /auth/signin, GET /me.
import { request, type RequestOptions } from './client'
import type { Session, SignInRequest, SignUpRequest, User } from './types'

export interface SignUpDetails {
  firstName: string
  lastName: string
  email: string
  phone: string
  password: string
}

export function signUp({ firstName, lastName, email, phone, password }: SignUpDetails) {
  const body: SignUpRequest = { first_name: firstName, last_name: lastName, email, password }
  if (phone) body.phone = phone
  return request<Session>('/auth/signup', { method: 'POST', body, auth: false })
}

export function signIn(credentials: SignInRequest) {
  return request<Session>('/auth/signin', { method: 'POST', body: credentials, auth: false })
}

export function getMe(options?: RequestOptions) {
  return request<User>('/me', options)
}
