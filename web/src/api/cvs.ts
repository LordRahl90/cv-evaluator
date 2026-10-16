// CV endpoints. GET /cv/ returns a bare array of {id, created_at, updated_at}.
// POST /cv/ takes the PDF as the multipart field "cv" and answers 202 once
// the CV is extracted (that happens inside the request today, so it can
// take a while). GET /cv/download/:id returns the extracted text.
import { request, requestBlob, type RequestOptions } from './client'
import type { CVSummary } from './types'

export async function listCVs(options?: RequestOptions): Promise<CVSummary[]> {
  const data = await request<CVSummary[] | null>('/cv/', options)
  return Array.isArray(data) ? data : []
}

export async function uploadCV(file: File, options?: Pick<RequestOptions, 'signal'>) {
  const body = new FormData()
  body.append('cv', file, file.name)
  await request<null>('/cv/', { ...options, method: 'POST', body })
}

/** The extracted text of one CV, as plain text. */
export function downloadCVText(id: string, options?: RequestOptions): Promise<Blob> {
  return requestBlob(`/cv/download/${encodeURIComponent(id)}`, options)
}

export async function deleteCV(id: string, options?: RequestOptions) {
  await request<null>(`/cv/${encodeURIComponent(id)}`, { ...options, method: 'DELETE' })
}
