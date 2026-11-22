import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { monthGrid, resetMarks, shiftMonth } from '../src/utils/resetCalendar.ts'

const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('reset marks use the verified day and the announcement window only', () => {
  const marks = resetMarks([
    {
      announced_at: '2026-11-22T01:00:00Z',
      confirmed_at: '2026-11-21T09:18:00Z',
      occurred_on: '2026-11-21',
      schedule_dates: ['2026-11-20', '2026-11-21'],
    },
    {
      announced_at: '2026-11-18T09:00:00+08:00',
      confirmed_at: '2026-11-18T09:00:00+08:00',
      schedule_dates: ['2026-11-19'],
    },
    { occurred_on: '2026-02-31', schedule_dates: ['not-a-date'] },
  ])
  assert.equal(marks.get('2026-11-21'), 'confirmed')
  assert.equal(marks.get('2026-11-20'), 'estimated')
  assert.equal(marks.get('2026-11-19'), 'estimated')
  assert.equal(marks.has('2026-11-18'), false)
  assert.equal(marks.has('2026-11-22'), false)
  assert.equal(marks.size, 3)
})

test('an empty announcement leaves the calendar unmarked', () => {
  assert.equal(resetMarks([{ announced_at: '2026-11-21T17:18:00+08:00' }]).size, 0)
  assert.equal(resetMarks([]).size, 0)
})

test('month grid starts on Monday and can move back to the current month', () => {
  const november = monthGrid(2026, 11)
  assert.equal(november[0].date, null)
  assert.equal(november[6].date, '2026-11-01')
  assert.equal(november.filter((cell) => cell.date).length, 30)
  assert.equal(november.length % 7, 0)
  const previous = shiftMonth(2026, 11, -1)
  assert.deepEqual(previous, { year: 2026, month: 10 })
  assert.deepEqual(shiftMonth(previous.year, previous.month, 1), { year: 2026, month: 11 })
})

test('the feed page leads with four calendars and has no source links', () => {
  const page = read('../src/views/AIFeed.vue')
  const calendar = read('../src/components/ResetCalendar.vue')
  assert.ok(page.indexOf('id="reset-calendars"') < page.indexOf('id="feed-news"'))
  assert.match(page, /codex', 'antigravity', 'claude', 'grok_bot'/)
  assert.equal(/<a[\s>]/.test(page), false)
  assert.equal(/<a[\s>]/.test(calendar), false)
  assert.match(calendar, /feed\.noResetDate/)
  assert.match(calendar, /feed\.today/)
  assert.match(calendar, /feed\.previousMonth/)
  assert.match(calendar, /feed\.estimatedNote/)
  assert.equal(calendar.includes('openNotice'), false)
  assert.equal(calendar.includes('openFeed'), false)
  assert.equal(calendar.includes('openProviderPage'), false)
})

test('rebate label keeps the affiliate route and a reduced-motion sway', () => {
  const nav = read('../src/components/MobileBottomNav.vue')
  const desktop = read('../src/components/Navbar.vue')
  const vault = read('../src/templates/vault/layout/VaultLayout.vue')
  const css = read('../src/style.css')
  for (const locale of ['zh-CN', 'zh-TW', 'en-US']) {
    const messages = read(`../src/i18n/locales/${locale}.json`)
    assert.match(messages, /"invite": "返/)
  }
  assert.match(nav, /path: '\/me\/affiliate'/)
  assert.match(nav, /nav-rebate-label/)
  assert.match(desktop, /to="\/me\/affiliate"/)
  assert.match(desktop, /nav-rebate-label/)
  assert.match(vault, /to="\/me\/affiliate"/)
  assert.match(vault, /nav-rebate-label/)
  assert.match(css, /@keyframes nav-rebate-sway/)
  assert.match(css, /prefers-reduced-motion: reduce/)
  assert.match(css, /animation: none/)
})
