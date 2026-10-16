// Copying text. The async Clipboard API needs a secure context and, in some
// browsers, a permission; when it is missing or refuses, copyText resolves
// false and the caller falls back to selecting the text for Ctrl/Cmd+C.

export async function copyText(text: string): Promise<boolean> {
  if (!navigator.clipboard?.writeText) return false
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}

/** "Cmd" on Apple platforms, "Ctrl" elsewhere. */
export function copyShortcut(): string {
  const platform = navigator.platform || navigator.userAgent
  return /Mac|iPhone|iPad|iPod/i.test(platform) ? 'Cmd+C' : 'Ctrl+C'
}
