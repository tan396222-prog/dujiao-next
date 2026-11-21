/** Accept only http(s) links with no embedded credentials. */
export function publicHttpLink(value: unknown): string {
  const raw = String(value ?? '').trim()
  if (!raw) return ''
  try {
    const url = new URL(raw)
    if (url.protocol !== 'https:' && url.protocol !== 'http:') return ''
    if (url.username || url.password) return ''
    return url.toString()
  } catch {
    return ''
  }
}
