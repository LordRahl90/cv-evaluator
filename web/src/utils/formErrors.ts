// Splits an API error into a message for one form field (when the API names
// the offending parameter and the form has that field) or for the whole form.
import { ApiError } from '../api/client'

export interface SplitError<F extends string> {
  fields: Partial<Record<F, string>>
  form: string
}

export function splitApiError<F extends string>(
  err: unknown,
  fieldNames: readonly F[],
): SplitError<F> {
  const message =
    err instanceof Error && err.message ? err.message : 'Something went wrong. Try again.'
  const param = err instanceof ApiError ? err.param : null
  if (param && (fieldNames as readonly string[]).includes(param)) {
    return { fields: { [param as F]: message } as Partial<Record<F, string>>, form: '' }
  }
  return { fields: {}, form: message }
}
