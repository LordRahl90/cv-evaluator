// State for the CVs page: the list, the upload in progress, downloads and
// deletes. POST /cv/ processes the PDF inside the request, so an upload can
// take a minute; its state lives at module level so it keeps going (and
// still shows) when the user visits another page and comes back.
import { computed, reactive, readonly, ref, watch } from 'vue'
import { deleteCV, downloadCVText, listCVs, uploadCV } from '../api/cvs'
import type { CVSummary } from '../api/types'
import { cvFileProblem } from '../utils/cvFile'
import { saveBlob } from '../utils/download'

export type LoadStatus = 'loading' | 'ready' | 'error'

interface UploadState {
  /** Name of the file being uploaded, while the request runs. */
  fileName: string
  busy: boolean
  /** Validation or API error from the last attempt. */
  error: string
  /** Set after a successful upload, until the next attempt. */
  succeeded: boolean
  /** Bumped on every successful upload, so open lists can reload. */
  completed: number
}

const uploadState = reactive<UploadState>({
  fileName: '',
  busy: false,
  error: '',
  succeeded: false,
  completed: 0,
})

const errorMessage = (err: unknown) =>
  err instanceof Error && err.message ? err.message : 'Something went wrong. Try again.'

/**
 * Validates and uploads one file. Returns false (with uploadState.error set)
 * when the file is rejected before upload or the upload fails.
 */
export async function startUpload(files: FileList | File[] | null | undefined): Promise<boolean> {
  if (uploadState.busy) return false
  const list = Array.from(files ?? [])
  uploadState.succeeded = false
  const [file] = list
  if (!file) return false
  if (list.length > 1) {
    uploadState.error = 'Choose one PDF at a time.'
    return false
  }
  const problem = cvFileProblem(file)
  if (problem) {
    uploadState.error = problem
    return false
  }

  uploadState.error = ''
  uploadState.fileName = file.name
  uploadState.busy = true
  try {
    await uploadCV(file)
    uploadState.succeeded = true
    uploadState.completed += 1
    return true
  } catch (err) {
    uploadState.error = `We couldn't process "${file.name}". ${errorMessage(err)}`
    return false
  } finally {
    uploadState.busy = false
    uploadState.fileName = ''
  }
}

export function clearUploadMessages(): void {
  if (uploadState.busy) return
  uploadState.error = ''
  uploadState.succeeded = false
}

const newestFirst = (a: CVSummary, b: CVSummary) =>
  new Date(b.created_at).getTime() - new Date(a.created_at).getTime() || b.id.localeCompare(a.id)

export function useCVs() {
  const status = ref<LoadStatus>('loading')
  const loadError = ref('')
  const cvs = ref<CVSummary[]>([])
  const downloadingId = ref<string | null>(null)
  const actionError = ref('')

  async function load({ quiet = false } = {}): Promise<void> {
    if (!quiet) status.value = 'loading'
    loadError.value = ''
    try {
      cvs.value = (await listCVs()).sort(newestFirst)
      status.value = 'ready'
    } catch (err) {
      // A 401 is handled globally; anything else gets a retry.
      loadError.value = errorMessage(err)
      status.value = 'error'
    }
  }

  // The API picks the newest CV (by created_at) for matching.
  const inUse = computed<CVSummary | null>(() => cvs.value[0] ?? null)

  watch(
    () => uploadState.completed,
    () => load({ quiet: status.value === 'ready' }),
  )

  async function download(cv: CVSummary): Promise<void> {
    if (downloadingId.value) return
    downloadingId.value = cv.id
    actionError.value = ''
    try {
      const blob = await downloadCVText(cv.id)
      saveBlob(blob, `cv-${cv.created_at.slice(0, 10)}.txt`)
    } catch (err) {
      actionError.value = `We couldn't download that CV. ${errorMessage(err)}`
    } finally {
      downloadingId.value = null
    }
  }

  /** Deletes a CV; throws so the confirmation dialog can show the error. */
  async function remove(cv: CVSummary): Promise<void> {
    await deleteCV(cv.id)
    cvs.value = cvs.value.filter((c) => c.id !== cv.id)
  }

  return {
    status,
    loadError,
    cvs,
    inUse,
    load,
    upload: readonly(uploadState),
    download,
    downloadingId,
    actionError,
    remove,
  }
}
