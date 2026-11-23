import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import {
  CHECKIN_FIREWORK_MS,
  CHECKIN_SPARKS,
  CHECKIN_VIBRATION_PATTERN,
  checkInCelebration,
  checkInVibrationAllowed,
  createCheckInBurst,
} from '../src/utils/checkInCelebration.ts'

const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('only a newly confirmed claim celebrates', () => {
  const fresh = { source: 'claim' as const, ok: true, checkedIn: true, alreadyClaimed: false }
  assert.equal(checkInCelebration(fresh), 'fireworks')
  assert.equal(checkInCelebration({ ...fresh, reducedMotion: true }), 'static')
  assert.equal(checkInCelebration({ ...fresh, source: 'status' }), 'none')
  assert.equal(checkInCelebration({ ...fresh, ok: false }), 'none')
  assert.equal(checkInCelebration({ ...fresh, checkedIn: false }), 'none')
  assert.equal(checkInCelebration({ ...fresh, alreadyClaimed: true }), 'none')
  assert.equal(checkInCelebration({ ...fresh, alreadyClaimed: undefined }), 'none')
})

test('vibration follows the new-success branch and the saved off switch', () => {
  assert.equal(checkInVibrationAllowed('fireworks', true, null), true)
  assert.equal(checkInVibrationAllowed('fireworks', true, 'off'), false)
  assert.equal(checkInVibrationAllowed('fireworks', false, null), false)
  assert.equal(checkInVibrationAllowed('static', true, null), false)
  assert.equal(checkInVibrationAllowed('none', true, null), false)
})

test('a burst vibrates once, cleans up, and does not retry a rejection', () => {
  const calls: Array<number | number[]> = []
  let now = 0
  let scheduled: (() => void) | null = null
  const burst = createCheckInBurst({
    celebration: 'fireworks',
    preference: 'on',
    vibrate: (pattern) => {
      calls.push(pattern)
      return pattern !== 0
    },
    clock: {
      schedule: (fn) => {
        scheduled = fn
        return 1
      },
      cancel: () => {
        scheduled = null
      },
    },
  })
  burst.start()
  burst.start()
  assert.equal(calls.length, 1)
  assert.deepEqual(calls[0], CHECKIN_VIBRATION_PATTERN)
  assert.equal(typeof scheduled, 'function')
  now += CHECKIN_FIREWORK_MS
  scheduled?.()
  assert.deepEqual(calls[1], 0)
  burst.stop()
  assert.equal(calls.length, 2)
})

test('reduced motion and a thrown vibration api do not loop', () => {
  const thrown = createCheckInBurst({
    celebration: 'fireworks',
    vibrate: () => {
      throw new Error('unsupported')
    },
    clock: {
      schedule: (fn) => {
        fn()
        return 7
      },
      cancel: () => undefined,
    },
  })
  assert.doesNotThrow(() => thrown.start())
  const quiet = createCheckInBurst({
    celebration: 'static',
    vibrate: () => {
      throw new Error('should not vibrate')
    },
  })
  assert.doesNotThrow(() => quiet.start())
  assert.equal(quiet.active(), true)
})

test('the check-in card celebrates only after claim and the sparks stay local', () => {
  const card = read('../src/components/DailyCheckInCard.vue')
  const overlay = read('../src/components/CheckInCelebration.vue')
  assert.match(card, /onMounted\(\(\) => \{\s*loadCheckIn\(\)/)
  assert.equal(card.includes('checkInCelebration({\n      source: \'status\''), false)
  assert.match(card, /source: 'claim'/)
  assert.match(card, /data-checkin-success/)
  assert.match(overlay, /pointer-events-none/)
  assert.match(overlay, /prefers-reduced-motion: reduce/)
  assert.equal(overlay.includes('http'), false)
  assert.match(read('../src/utils/checkInCelebration.ts'), /dj-opening-vibration/)
  assert.match(read('../src/utils/openingVibration.ts'), /dj-opening-vibration/)
  assert.equal(CHECKIN_SPARKS.length < 20, true)
})
