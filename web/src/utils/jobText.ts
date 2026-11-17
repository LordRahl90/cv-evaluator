// Helpers for a pasted job description, shared by the quick match pages.

/** A title for a pasted description: its first line, cut at about 80 characters. */
export function titleFrom(text: string): string {
  const line = text
    .split('\n')
    .map((l) => l.trim())
    .find(Boolean)
  if (!line) return 'Pasted job description'
  if (line.length <= 80) return line
  const cut = line.slice(0, 80)
  return `${cut.slice(0, cut.lastIndexOf(' ') > 40 ? cut.lastIndexOf(' ') : 80)}…`
}

export const wordCount = (text: string) => text.trim().split(/\s+/).filter(Boolean).length

/** The shortest job description a guest match accepts, as the API checks it. */
export const MIN_GUEST_JOB_WORDS = 40
export const MIN_GUEST_JOB_CHARS = 200

/** Why a trimmed job description is too short for a guest match, or ''. */
export function guestJobProblem(text: string): string {
  const words = wordCount(text)
  if (words >= MIN_GUEST_JOB_WORDS && [...text].length >= MIN_GUEST_JOB_CHARS) return ''
  return `This job description is too short (${words} ${words === 1 ? 'word' : 'words'}). Paste the full posting, at least ${MIN_GUEST_JOB_WORDS} words.`
}

/** "Pasted description · 42 words", the meta line under a result's title. */
export const pastedMeta = (words: number) =>
  `Pasted description · ${words} ${words === 1 ? 'word' : 'words'}`
