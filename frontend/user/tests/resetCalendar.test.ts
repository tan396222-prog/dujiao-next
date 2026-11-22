import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { dayKind, monthCounts, monthGrid, resetFacts, shiftMonth } from '../src/utils/resetCalendar.ts'

const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('calendar facts keep type and status and ignore posts', () => {
  const facts = resetFacts([
    { kind: 'reset_credit', status: 'effective', date: '2026-11-01' },
    { kind: 'reset_credit', status: 'effective', date: '2026-11-02' },
    { kind: 'reset_credit', status: 'effective', date: '2026-11-13' },
    { kind: 'quota_reset', status: 'effective', date: '2026-10-26' },
    { kind: 'quota_reset', status: 'announced', date: '2026-11-13' },
    { kind: 'quota_reset', status: 'effective', date: '2026-02-31' },
    { kind: 'maintenance', status: 'effective', date: '2026-11-18', title: '帖子正文', url: 'https://x.com/example' },
    { date: '2026-11-19', announced_at: '2026-11-19T09:00:00+08:00' },
  ], '2026-11-22')
  assert.equal(dayKind(facts.get('2026-11-01')), 'reset_credit')
  assert.equal(dayKind(facts.get('2026-10-26')), 'quota_reset')
  assert.equal(dayKind(facts.get('2026-11-13')), 'mixed')
  assert.equal(facts.has('2026-11-18'), false)
  assert.equal(facts.has('2026-11-19'), false)
  assert.equal(facts.has('2026-02-31'), false)
  const november = monthCounts(facts, 2026, 11)
  assert.deepEqual(november, { quota: 1, card: 3 })
  assert.deepEqual(monthCounts(facts, 2026, 10), { quota: 1, card: 0 })
  const mixed = facts.get('2026-11-13') || []
  assert.equal(mixed.find((line) => line.kind === 'reset_credit')?.count, 1)
  assert.equal(mixed.find((line) => line.kind === 'quota_reset')?.status, 'announced')
})

test('a future day is not circled unless it is still announced', () => {
  const facts = resetFacts([
    { kind: 'quota_reset', status: 'effective', date: '2026-11-23' },
    { kind: 'reset_credit', status: 'recorded', date: '2026-11-01' },
    { kind: 'quota_reset', status: 'announced', date: '2026-12-02' },
    { kind: 'reset_credit', status: 'effective', date: '2026-11-22' },
  ], '2026-11-22')
  assert.equal(facts.has('2026-11-23'), false)
  assert.equal(facts.get('2026-11-01')?.[0]?.status, 'recorded')
  assert.equal(facts.get('2026-12-02')?.[0]?.status, 'announced')
  assert.equal(dayKind(facts.get('2026-11-22')), 'reset_credit')
})

test('an empty calendar stays unmarked', () => {
  assert.equal(resetFacts([]).size, 0)
  assert.equal(resetFacts([{ kind: 'quota_reset', status: 'effective' }]).size, 0)
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

test('the feed page leads with four calendars and shows no source or post', () => {
  const page = read('../src/views/AIFeed.vue')
  const calendar = read('../src/components/ResetCalendar.vue')
  assert.ok(page.indexOf('id="reset-calendars"') < page.indexOf('id="feed-news"'))
  assert.match(page, /codex', 'antigravity', 'claude', 'grok_bot'/)
  assert.equal(/<a[\s>]/.test(page), false)
  assert.equal(/<a[\s>]/.test(calendar), false)
  assert.match(calendar, /feed\.quotaReset/)
  assert.match(calendar, /feed\.resetCard/)
  assert.match(calendar, /data-reset-day-facts/)
  assert.match(calendar, /#d92d20/)
  assert.equal(calendar.includes('event.title'), false)
  assert.equal(calendar.includes('event.summary'), false)
  assert.equal(calendar.includes('event.original'), false)
  assert.equal(calendar.includes('tooltip'), false)
  assert.equal(calendar.includes('openNotice'), false)
  assert.equal(calendar.includes('openFeed'), false)
  assert.equal(calendar.includes('openProviderPage'), false)
  assert.match(page, /item\.summary/)
  assert.equal(page.includes('<a'), false)
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
