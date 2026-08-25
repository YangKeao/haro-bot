export function formatMessageTime(value: string, now = new Date()) {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return ''
  const sameDay = date.getFullYear() === now.getFullYear()
    && date.getMonth() === now.getMonth()
    && date.getDate() === now.getDate()
  if (sameDay) {
    return new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' }).format(date)
  }
  const options: Intl.DateTimeFormatOptions = {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  }
  if (date.getFullYear() !== now.getFullYear()) options.year = 'numeric'
  return new Intl.DateTimeFormat(undefined, options).format(date)
}

export function formatMessageTimeTitle(value: string) {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return ''
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'medium' }).format(date)
}
