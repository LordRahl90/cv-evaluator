// Small display helpers for names, dates and times.
import type { User } from '../api/types'

type DateInput = string | number | Date

export function initials(user: Pick<User, 'first_name' | 'last_name' | 'email'>): string {
  const parts = [user.first_name, user.last_name].filter(Boolean)
  const source = parts.length ? parts : [user.email || '?']
  return source
    .map((p) => p.trim().charAt(0))
    .join('')
    .slice(0, 2)
    .toUpperCase()
}

const dayFormat = new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'short' })
const timeFormat = new Intl.DateTimeFormat('en-GB', { hour: '2-digit', minute: '2-digit' })

/** "12 Oct" */
export function formatDay(value: DateInput): string {
  return dayFormat.format(new Date(value))
}

/** "in 38 min", "2h ago", "in 3 days", "just now" */
export function formatRelative(value: DateInput, now: number = Date.now()): string {
  const diff = new Date(value).getTime() - now
  const abs = Math.abs(diff)
  const minute = 60_000
  const hour = 60 * minute
  const day = 24 * hour
  if (abs < minute) return diff >= 0 ? 'in under a minute' : 'just now'
  let text: string
  if (abs < hour) text = `${Math.round(abs / minute)} min`
  else if (abs < day) text = `${Math.round(abs / hour)}h`
  else {
    const days = Math.round(abs / day)
    text = `${days} ${days === 1 ? 'day' : 'days'}`
  }
  return diff >= 0 ? `in ${text}` : `${text} ago`
}

/** "07:00 today", "yesterday 18:30", "12 Oct" */
export function formatRunTime(value: DateInput, now: Date = new Date()): string {
  const date = new Date(value)
  const sameDay = (a: Date, b: Date) => a.toDateString() === b.toDateString()
  if (sameDay(date, now)) return `${timeFormat.format(date)} today`
  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  if (sameDay(date, yesterday)) return `yesterday ${timeFormat.format(date)}`
  return formatDay(date)
}

/** "due now", "18:30 today", "07:00 tomorrow", "12 Oct", for scheduled runs. */
export function formatUpcoming(value: DateInput, now: Date = new Date()): string {
  const date = new Date(value)
  if (date.getTime() <= now.getTime()) return 'due now'
  if (date.toDateString() === now.toDateString()) return `${timeFormat.format(date)} today`
  const tomorrow = new Date(now)
  tomorrow.setDate(now.getDate() + 1)
  if (date.toDateString() === tomorrow.toDateString()) return `${timeFormat.format(date)} tomorrow`
  return formatDay(date)
}

export function capitalise(text: string): string {
  return text ? text.charAt(0).toUpperCase() + text.slice(1) : ''
}

export function greeting(now: Date = new Date()): string {
  const hour = now.getHours()
  if (hour < 12) return 'Good morning'
  if (hour < 18) return 'Good afternoon'
  return 'Good evening'
}

// Spelled out rather than Intl: some ICU versions write "Sept" for en-GB.
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

/** "12 Oct 2026", in local time */
export function formatDate(value: DateInput): string {
  const date = new Date(value)
  return `${date.getDate()} ${MONTHS[date.getMonth()]} ${date.getFullYear()}`
}

/** "just now" for the last minute, otherwise as formatRelative. */
export function formatAgo(value: DateInput, now: number = Date.now()): string {
  return now - new Date(value).getTime() < 60_000 ? 'just now' : formatRelative(value, now)
}

/** "12 Oct 2026, 08:00" */
export function formatDateTime(value: DateInput): string {
  return `${formatDate(value)}, ${timeFormat.format(new Date(value))}`
}
