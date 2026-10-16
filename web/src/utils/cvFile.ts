// Checks a CV file before it is uploaded, so obvious mistakes get a friendly
// message straight away instead of a long wait and a server error.
export const MAX_CV_BYTES = 5 * 1024 * 1024

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function looksLikePDF(file: File): boolean {
  // Some systems report an empty type for PDFs; fall back to the extension.
  if (file.type) return file.type === 'application/pdf'
  return /\.pdf$/i.test(file.name)
}

/** Returns a message saying what is wrong with the file, or '' if it is fine. */
export function cvFileProblem(file: File): string {
  if (!looksLikePDF(file)) {
    return `"${file.name}" isn't a PDF. Save or export your CV as a PDF and try again.`
  }
  if (file.size === 0) return `"${file.name}" is empty. Choose the PDF again.`
  if (file.size > MAX_CV_BYTES) {
    return `"${file.name}" is ${formatBytes(file.size)}. CVs can be up to 5 MB; try exporting a smaller PDF.`
  }
  return ''
}
