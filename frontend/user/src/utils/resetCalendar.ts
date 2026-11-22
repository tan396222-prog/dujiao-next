export type ResetMark = 'confirmed' | 'estimated'

export type ResetCalendarEvent = {
  occurred_on?: string
  schedule_dates?: string[]
  announced_at?: string
  confirmed_at?: string
}

export type MonthCell = {
  key: string
  date: string | null
}

const dateOnly = /^\d{4}-\d{2}-\d{2}$/

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

// Confirmed days come only from the verified occurrence date. Schedule days are
// the announcement's own stated window and stay estimates. Publish, confirmation
// and fetch timestamps are ignored.
export const resetMarks = (events: ResetCalendarEvent[]): Map<string, ResetMark> => {
  const marks = new Map<string, ResetMark>()
  for (const event of events) {
    for (const day of event.schedule_dates || []) {
      if (isDate(day) && !marks.has(day)) marks.set(day, 'estimated')
    }
  }
  for (const event of events) {
    if (isDate(event.occurred_on)) marks.set(event.occurred_on, 'confirmed')
  }
  return marks
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
