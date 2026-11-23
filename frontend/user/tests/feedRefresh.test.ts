import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createFeedRefresh, FEED_REFRESH_MS } from '../src/utils/feedRefresh.ts'

test('an open feed page refreshes on a ten minute timer and cleans up', () => {
  assert.equal(FEED_REFRESH_MS, 10 * 60 * 1000)
  const timers = new Map<number, { fn: () => void, delay: number }>()
  let next = 1
  const clock = {
    schedule(fn: () => void, delay: number) {
      const id = next
      next += 1
      timers.set(id, { fn, delay })
      return id
    },
    cancel(id: number) {
      timers.delete(id)
    },
  }
  let hidden = false
  let loads = 0
  const refresh = createFeedRefresh({
    reload: () => { loads += 1 },
    hidden: () => hidden,
    clock,
  })
  refresh.start()
  refresh.start()
  assert.equal(timers.size, 1)
  assert.equal([...timers.values()][0]?.delay, FEED_REFRESH_MS)
  assert.equal(loads, 0)

  const pending = [...timers.values()][0]
  pending?.fn()
  assert.equal(loads, 1)
  assert.equal(timers.size, 1)

  hidden = true
  refresh.onVisibility()
  assert.equal(timers.size, 0)
  assert.equal(loads, 1)

  hidden = false
  refresh.onVisibility()
  refresh.onVisibility()
  assert.equal(loads, 2)
  assert.equal(timers.size, 1)

  refresh.stop()
  assert.equal(timers.size, 0)
  const leftover = next
  refresh.onVisibility()
  refresh.start()
  refresh.stop()
  assert.equal(timers.size, 0)
  assert.ok(next >= leftover)
})

test('the feed page does not call the upstream and removes source text', () => {
  const page = readFileSync(new URL('../src/views/AIFeed.vue', import.meta.url), 'utf8')
  const helper = readFileSync(new URL('../src/utils/feedRefresh.ts', import.meta.url), 'utf8')
  assert.match(page, /createFeedRefresh/)
  assert.match(page, /onBeforeUnmount/)
  assert.equal(page.includes('item.source'), false)
  assert.equal(page.includes('aihot.news'), false)
  assert.equal(page.includes('AIHOT'), false)
  assert.equal(helper.includes('aihot.news'), false)
  for (const locale of ['zh-CN', 'zh-TW', 'en-US']) {
    const messages = readFileSync(new URL(`../src/i18n/locales/${locale}.json`, import.meta.url), 'utf8')
    assert.equal(messages.includes('AIHOT'), false)
    assert.equal(messages.includes('aihot.news'), false)
  }
})
