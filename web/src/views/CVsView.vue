<script setup lang="ts">
// CVs page: upload a PDF (drop zone when there are none, a header button
// otherwise), list the CVs, download their extracted text, delete them.
// The API has no "active CV" yet: matching uses the newest one, so that is
// the one labelled "Used for matching".
import { computed, onMounted, ref } from 'vue'
import AlertMessage from '../components/AlertMessage.vue'
import AppIcon from '../components/AppIcon'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import CVDropZone from '../components/CVDropZone.vue'
import CVFilePicker from '../components/CVFilePicker.vue'
import UploadProgress from '../components/UploadProgress.vue'
import type { CVSummary } from '../api/types'
import { clearUploadMessages, startUpload, useCVs } from '../composables/useCVs'
import { formatDate, formatDateTime } from '../utils/format'

const {
  status,
  loadError,
  cvs,
  inUse,
  load,
  upload,
  download,
  downloadingId,
  actionError,
  remove,
} = useCVs()

onMounted(() => {
  clearUploadMessages()
  load()
})

const isEmpty = computed(() => status.value === 'ready' && cvs.value.length === 0)

const cvName = (cv: CVSummary) => `CV from ${formatDate(cv.created_at)}`
const shortId = (cv: CVSummary) => `${cv.id.slice(0, 4)}…${cv.id.slice(-4)}`

// Delete with confirmation
const pendingDelete = ref<CVSummary | null>(null)
const deleting = ref(false)
const deleteError = ref('')

function askDelete(cv: CVSummary) {
  deleteError.value = ''
  pendingDelete.value = cv
}

function cancelDelete() {
  if (!deleting.value) pendingDelete.value = null
}

async function confirmDelete() {
  const cv = pendingDelete.value
  if (!cv || deleting.value) return
  deleting.value = true
  deleteError.value = ''
  try {
    await remove(cv)
    pendingDelete.value = null
  } catch (err) {
    deleteError.value = err instanceof Error ? err.message : 'Something went wrong. Try again.'
  } finally {
    deleting.value = false
  }
}

const deleteConsequence = computed(() => {
  const cv = pendingDelete.value
  if (!cv) return ''
  const next = cvs.value.find((c) => c.id !== cv.id)
  if (!next) return "You'll need to upload a CV again before matching."
  if (cv.id === inUse.value?.id) return `Matching will switch to your ${cvName(next)} instead.`
  return 'Matching keeps using your newest CV.'
})
</script>

<template>
  <div class="page">
    <div class="page-stack">
      <div class="page-head">
        <div class="page-head-text">
          <h1 class="page-title">CVs</h1>
          <p class="page-sub">Your newest CV is the one every job is scored against.</p>
        </div>
        <div v-if="status === 'ready' && !isEmpty" class="page-actions">
          <CVFilePicker label="Upload CV" :disabled="upload.busy" @select="startUpload" />
        </div>
      </div>

      <AlertMessage v-if="upload.error" data-testid="cv-upload-error">
        {{ upload.error }}
      </AlertMessage>
      <AlertMessage v-else-if="upload.succeeded" tone="info" data-testid="cv-upload-success">
        CV uploaded. It's now the one used for matching.
      </AlertMessage>
      <AlertMessage v-if="actionError">{{ actionError }}</AlertMessage>

      <!-- Loading -->
      <div v-if="status === 'loading'" class="page-stack" role="status" aria-live="polite">
        <span class="sr-only">Loading your CVs…</span>
        <div class="card skeleton-card" aria-hidden="true" data-testid="cvs-skeleton">
          <span class="skeleton skeleton-line skeleton-w-32" />
          <span class="skeleton skeleton-line skeleton-w-56" />
        </div>
        <div class="card skeleton-card" aria-hidden="true">
          <span class="skeleton skeleton-row" />
          <span class="skeleton skeleton-row" />
        </div>
      </div>

      <!-- Error -->
      <section v-else-if="status === 'error'" class="card error-card">
        <AlertMessage>We couldn't load your CVs. {{ loadError }}</AlertMessage>
        <button type="button" class="btn btn-secondary" @click="load()">
          <AppIcon name="refresh" />Try again
        </button>
      </section>

      <!-- No CVs yet -->
      <template v-else-if="isEmpty">
        <section v-if="upload.busy" class="card cv-upload-card" aria-label="Uploading your CV">
          <UploadProgress :file-name="upload.fileName" />
        </section>
        <CVDropZone v-else @select="startUpload" />

        <section class="tip-grid" aria-label="Tips">
          <div class="card tip">
            <AppIcon name="check" />
            <div>
              <div class="tip-title">Keep skills explicit</div>
              <div class="tip-text">A skills list carries the most weight in every score.</div>
            </div>
          </div>
          <div class="card tip">
            <AppIcon name="check" />
            <div>
              <div class="tip-title">Several versions are fine</div>
              <div class="tip-text">
                Upload one per angle, such as backend or platform. Matching uses the newest.
              </div>
            </div>
          </div>
          <div class="card tip">
            <AppIcon name="check" />
            <div>
              <div class="tip-title">Processing takes a minute</div>
              <div class="tip-text">
                You can use other pages while it runs; keep this tab open until it's done.
              </div>
            </div>
          </div>
        </section>
      </template>

      <!-- List -->
      <template v-else>
        <section
          v-if="inUse"
          class="card cv-in-use"
          aria-labelledby="cv-in-use-label cv-in-use-title"
        >
          <div id="cv-in-use-label" class="cv-in-use-label">Used for matching</div>
          <h2 id="cv-in-use-title" class="cv-in-use-title">{{ cvName(inUse) }}</h2>
          <div class="cv-in-use-text">
            Your newest CV, uploaded {{ formatDateTime(inUse.created_at) }}. Quick match and your
            job searches score jobs against it.
          </div>
        </section>

        <section class="card cv-table-card" aria-label="All CVs">
          <table class="cv-table">
            <thead>
              <tr>
                <th scope="col">CV</th>
                <th scope="col">Status</th>
                <th scope="col">Uploaded</th>
                <th scope="col" class="cv-col-actions">Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="upload.busy" class="cv-row-uploading">
                <td colspan="4"><UploadProgress :file-name="upload.fileName" /></td>
              </tr>
              <tr v-for="cv in cvs" :key="cv.id" :data-testid="`cv-row-${cv.id}`">
                <td>
                  <div class="cv-file">
                    <span class="cv-file-icon" aria-hidden="true">PDF</span>
                    <div class="cv-file-body">
                      <div class="cv-file-name">{{ cvName(cv) }}</div>
                      <div class="cv-file-meta">ID {{ shortId(cv) }}</div>
                    </div>
                  </div>
                </td>
                <td>
                  <span v-if="cv.id === inUse?.id" class="badge badge-green">
                    <span class="badge-dot" aria-hidden="true" />Used for matching
                  </span>
                  <span v-else class="badge badge-grey">Older version</span>
                </td>
                <td class="cv-uploaded">{{ formatDateTime(cv.created_at) }}</td>
                <td class="cv-col-actions">
                  <button
                    type="button"
                    class="icon-btn cv-download-btn"
                    :aria-label="`Download extracted text of ${cvName(cv)}`"
                    :aria-busy="downloadingId === cv.id ? 'true' : undefined"
                    :disabled="downloadingId !== null"
                    title="Download extracted text"
                    @click="download(cv)"
                  >
                    <span v-if="downloadingId === cv.id" class="spinner" aria-hidden="true" />
                    <AppIcon v-else name="download" />
                  </button>
                  <button
                    type="button"
                    class="icon-btn cv-delete-btn"
                    :aria-label="`Delete ${cvName(cv)}`"
                    title="Delete CV"
                    @click="askDelete(cv)"
                  >
                    <AppIcon name="trash" />
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </section>
        <p class="cv-note">
          Matching always uses your newest CV. To score against an older version, upload it again.
          Changing CVs doesn't change past scores.
        </p>
      </template>
    </div>

    <ConfirmDialog
      :open="pendingDelete !== null"
      title="Delete this CV?"
      confirm-label="Delete CV"
      busy-label="Deleting…"
      :busy="deleting"
      :error="deleteError"
      @confirm="confirmDelete"
      @cancel="cancelDelete"
    >
      <p v-if="pendingDelete" class="confirm-dialog-paragraph">
        Your {{ cvName(pendingDelete) }} and its extracted text will be removed. This can't be
        undone.
      </p>
      <p class="confirm-dialog-paragraph">{{ deleteConsequence }}</p>
    </ConfirmDialog>
  </div>
</template>
