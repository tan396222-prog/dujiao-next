export type ResetKind = 'quota_reset' | 'reset_credit'
export type ResetStatus = 'effective' | 'announced' | 'unconfirmed'

export type ResetCalendarEvent = {
  kind?: string
  status?: string
  date?: string
}

export type ResetLine = {
  kind: ResetKind
  status: ResetStatus
  count: number
}

export type MonthCell = {
  key: string
  date: string | null
}

const dateOnly = /^\d{4}-\d{2}-\d{2}$/
const kinds = new Set<ResetKind>(['quota_reset', 'reset_credit'])
const statuses = new Set<ResetStatus>(['effective', 'announced', 'unconfirmed'])

const isDate = (value?: string): value is string => {
  if (typeof value !== 'string' || !dateOnly.test(value)) return false
  const parts = value.split('-')
  const year = Number(parts[0])
  const month = Number(parts[1])
  const day = Number(parts[2])
  if (!Number.isInteger(year) || !Number.isInteger(month) || !Number.isInteger(day)) return false
  const parsed = new Date(Date.UTC(year, month - 1, day))
  return parsed.getUTCFullYear() === year && parsed.getUTCMonth() === month - 1 && parsed.getUTCDate() === day
}

const isKind = (value?: string): value is ResetKind => kinds.has(value as ResetKind)
const isStatus = (value?: string): value is ResetStatus => statuses.has(value as ResetStatus)

// Facts come only from a typed calendar date. Titles, post text, links and
// timestamps are ignored even if an older payload still carries them.
export const resetFacts = (events: ResetCalendarEvent[]): Map<string, ResetLine[]> => {
  const grouped = new Map<string, Map<string, ResetLine>>()
  for (const event of events) {
    if (!isDate(event.date) || !isKind(event.kind) || !isStatus(event.status)) continue
    const lines = grouped.get(event.date) ?? new Map<string, ResetLine>()
    const key = `${event.kind}:${event.status}`
    const current = lines.get(key) ?? { kind: event.kind, status: event.status, count: 0 }
    current.count += 1
    lines.set(key, current)
    grouped.set(event.date, lines)
  }
  const facts = new Map<string, ResetLine[]>()
  for (const [date, lines] of grouped) facts.set(date, [...lines.values()])
  return facts
}

export const monthCounts = (facts: Map<string, ResetLine[]>, year: number, month: number) => {
  const prefix = `${year}-${String(month).padStart(2, '0')}-`
  const counts = { quota: 0, card: 0 }
  for (const [date, lines] of facts) {
    if (!date.startsWith(prefix)) continue
    for (const line of lines) {
      if (line.kind === 'quota_reset') counts.quota += line.count
      if (line.kind === 'reset_credit') counts.card += line.count
    }
  }
  return counts
}

export const dayKind = (lines: ResetLine[] | undefined) => {
  if (!lines?.length) return ''
  const kindsInDay = new Set(lines.map((line) => line.kind))
  if (kindsInDay.size > 1) return 'mixed'
  return lines[0]?.kind || ''
}

export const monthGrid = (year: number, month: number): MonthCell[] => {
  const first = new Date(Date.UTC(year, month - 1, 1))
  const leading = (first.getUTCDay() + 6) % 7
  const days = new Date(Date.UTC(year, month, 0)).getUTCDate()
  const cells: MonthCell[] = []
  for (let index = 0; index < leading; index += 1) cells.push({ key: `pad-${year}-${month}-${index}`, date: null })
  for (let day = 1; day <= days; day += 1) {
    const date = `${year}-${String(month).padStart(2, '0')}-${String(day).padStart(2, '0')}`
    cells.push({ key: date, date })
  }
  while (cells.length % 7 !== 0) cells.push({ key: `tail-${year}-${month}-${cells.length}`, date: null })
  return cells
}

export const shiftMonth = (year: number, month: number, delta: number) => {
  const cursor = new Date(Date.UTC(year, month - 1 + delta, 1))
  return { year: cursor.getUTCFullYear(), month: cursor.getUTCMonth() + 1 }
}

export const shanghaiToday = (now = new Date()) => {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(now)
  const year = Number(parts.find((part) => part.type === 'year')?.value)
  const month = Number(parts.find((part) => part.type === 'month')?.value)
  const day = parts.find((part) => part.type === 'day')?.value || '01'
  return {
    year,
    month,
    date: `${year}-${String(month).padStart(2, '0')}-${day}`,
  }
}
