// Hands a Blob to the browser as a file download.
export function saveBlob(blob: Blob, fileName: string): void {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = fileName
  link.rel = 'noopener'
  document.body.appendChild(link)
  link.click()
  link.remove()
  // Give the browser a moment to start the download before revoking.
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

// Saves data the app already holds as a pretty-printed JSON file. The API call
// itself stays in src/api/.
export function saveJsonFile(filename: string, data: unknown): void {
  const blob = new Blob([JSON.stringify(data, null, 2) + '\n'], { type: 'application/json' })
  saveBlob(blob, filename)
}
