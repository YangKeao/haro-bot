import type { ProviderUsageWindow } from './types'

export function formatUsagePercent(value: number) {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(value)
}

export function formatUsageWindow(window: ProviderUsageWindow) {
  const seconds = window.window_seconds
  if (seconds && seconds % 86_400 === 0) {
    const days = seconds / 86_400
    return `${days}-day limit`
  }
  if (seconds && seconds % 3_600 === 0) {
    const hours = seconds / 3_600
    return `${hours}-hour limit`
  }
  if (seconds && seconds % 60 === 0) {
    const minutes = seconds / 60
    return `${minutes}-minute limit`
  }
  return `${window.kind === 'primary' ? 'Primary' : 'Secondary'} limit`
}

export function formatUsageResetTime(value: string, now = new Date()) {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return ''
  const deltaSeconds = Math.round((date.getTime() - now.getTime()) / 1000)
  const absolute = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
  const relative = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })
  if (Math.abs(deltaSeconds) < 3_600) return `${absolute} · ${relative.format(Math.round(deltaSeconds / 60), 'minute')}`
  if (Math.abs(deltaSeconds) < 172_800) return `${absolute} · ${relative.format(Math.round(deltaSeconds / 3_600), 'hour')}`
  return `${absolute} · ${relative.format(Math.round(deltaSeconds / 86_400), 'day')}`
}
