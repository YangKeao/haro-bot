import { describe, expect, it } from 'vitest'
import { formatMessageTime, formatMessageTimeTitle } from './time'

describe('formatMessageTime', () => {
  const now = new Date(2026, 7, 25, 14, 30)

  it('uses a compact local time for messages from today', () => {
    const value = new Date(2026, 7, 25, 9, 5)
    expect(formatMessageTime(value.toISOString(), now)).toBe(new Intl.DateTimeFormat(undefined, {
      hour: '2-digit', minute: '2-digit',
    }).format(value))
  })

  it('adds the date and only includes the year when needed', () => {
    const sameYear = new Date(2026, 6, 14, 9, 5)
    const previousYear = new Date(2025, 11, 31, 23, 59)
    expect(formatMessageTime(sameYear.toISOString(), now)).toBe(new Intl.DateTimeFormat(undefined, {
      month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
    }).format(sameYear))
    expect(formatMessageTime(previousYear.toISOString(), now)).toBe(new Intl.DateTimeFormat(undefined, {
      year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
    }).format(previousYear))
  })

  it('does not render invalid timestamps', () => {
    expect(formatMessageTime('not-a-date', now)).toBe('')
    expect(formatMessageTimeTitle('not-a-date')).toBe('')
  })
})
