import { describe, expect, it } from 'vitest'
import { formatUsagePercent, formatUsageResetTime, formatUsageWindow } from './providerUsage'

describe('provider usage formatting', () => {
  it('labels common quota durations and formats percentages', () => {
    expect(formatUsageWindow({ kind: 'primary', used_percent: 2, window_seconds: 18_000 })).toBe('5-hour limit')
    expect(formatUsageWindow({ kind: 'secondary', used_percent: 2, window_seconds: 604_800 })).toBe('7-day limit')
    expect(formatUsageWindow({ kind: 'secondary', used_percent: 2 })).toBe('Secondary limit')
    expect(formatUsagePercent(42.55)).toBe(new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(42.55))
  })

  it('combines an absolute reset time with a relative duration', () => {
    const reset = new Date(2026, 7, 26, 17, 0)
    const now = new Date(2026, 7, 26, 12, 0)
    const formatted = formatUsageResetTime(reset.toISOString(), now)
    expect(formatted).toContain(new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(reset))
    expect(formatUsageResetTime('not-a-date', now)).toBe('')
  })
})
