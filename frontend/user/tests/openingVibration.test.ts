import assert from 'node:assert/strict'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import {
  OPENING_VIBRATION_PATTERN,
  createOpeningVibration,
  openingVibrationLabel,
  openingVibrationLanguage,
  shouldStopOpeningVibration,
} from '../src/utils/openingVibration.ts'

const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

const clock = () => {
  let now = 0
  const timers: { id: number; fn: () => void }[] = []
  let next = 1
  return {
    now: () => now,
    schedule: (fn: () => void) => {
      const id = next++
      timers.push({ id, fn })
      return id
    },
    cancel: (id: number) => {
      const index = timers.findIndex((timer) => timer.id === id)
      if (index >= 0) timers.splice(index, 1)
    },
    timers,
    advance: (ms: number) => {
      now += ms
    },
  }
}

test('opening vibration stays inside the specification limits', () => {
  assert.ok(OPENING_VIBRATION_PATTERN.length <= 10)
  assert.equal(OPENING_VIBRATION_PATTERN.length % 2, 1)
  assert.ok(OPENING_VIBRATION_PATTERN.every((value) => value > 0 && value <= 10000))
})

test('a missing or rejecting vibration api does not pulse or throw', () => {
  const missing = createOpeningVibration({ clock: clock() })
  assert.equal(missing.supported(), false)
  assert.equal(missing.enableFromGesture(), false)

  const calls: Array<number | number[]> = []
  const rejected = createOpeningVibration({
    clock: clock(),
    vibrate: (pattern) => {
      calls.push(pattern)
      return false
    },
  })
  assert.equal(rejected.enableFromGesture(), false)
  assert.deepEqual(calls, [OPENING_VIBRATION_PATTERN])
  assert.equal(rejected.pulsing(), false)

  const broken = createOpeningVibration({
    clock: clock(),
    vibrate: () => {
      throw new Error('blocked')
    },
  })
  assert.equal(broken.enableFromGesture(), false)
  assert.equal(broken.pulsing(), false)
})

test('a gesture starts one cleanup timer and a second start does not stack', () => {
  const calls: Array<number | number[]> = []
  const time = clock()
  const controller = createOpeningVibration({
    clock: time,
    vibrate: (pattern) => {
      calls.push(pattern)
      return true
    },
  })
  assert.equal(controller.enableFromGesture(), true)
  assert.equal(controller.enableFromGesture(), true)
  assert.equal(time.timers.length, 1)
  assert.equal(calls.length, 1)
  controller.stop()
  assert.equal(time.timers.length, 0)
  assert.deepEqual(calls.at(-1), 0)
  assert.equal(controller.pulsing(), false)
})

test('the animation window, hiding, and an explicit off switch all stop vibration', () => {
  const calls: Array<number | number[]> = []
  const time = clock()
  let stored: string | null = null
  const controller = createOpeningVibration({
    clock: time,
    writePreference: (value) => {
      stored = value
    },
    vibrate: (pattern) => {
      calls.push(pattern)
      return true
    },
  })
  time.advance(3000)
  assert.equal(controller.enableFromGesture(), false)
  assert.equal(calls.length, 0)

  const fresh = clock()
  const active = createOpeningVibration({
    clock: fresh,
    writePreference: (value) => {
      stored = value
    },
    vibrate: (pattern) => {
      calls.push(pattern)
      return true
    },
  })
  assert.equal(active.enableFromGesture(), true)
  active.disable()
  assert.equal(stored, 'off')
  assert.equal(fresh.timers.length, 0)
  assert.equal(calls.at(-1), 0)
  assert.equal(shouldStopOpeningVibration({ hidden: true }), true)
  assert.equal(shouldStopOpeningVibration({ leaving: true }), true)
  assert.equal(shouldStopOpeningVibration({ stalled: true }), true)
  assert.equal(shouldStopOpeningVibration({ connected: false }), true)
  assert.equal(shouldStopOpeningVibration({ connected: true }), false)
})

test('the opening control follows the saved storefront language', () => {
  assert.equal(openingVibrationLanguage('zh-CN', 'en-US'), 'zh-CN')
  assert.equal(openingVibrationLanguage(null, 'zh-TW'), 'zh-TW')
  assert.equal(openingVibrationLanguage(null, 'en-US'), 'en')
  assert.equal(openingVibrationLabel('start', openingVibrationLanguage('zh-CN', 'en-US')), '开启震动')
})

test('labels stay short and the boot page only opts in from its own control', () => {
  assert.equal(openingVibrationLabel('start', 'zh-CN'), '开启震动')
  assert.equal(openingVibrationLabel('stop', 'zh-TW'), '關閉震動')
  assert.equal(openingVibrationLabel('start', 'en-US'), 'Start vibration')
  const html = read('../index.html')
  const source = read('../src/utils/openingVibration.ts')
  assert.match(html, /id="boot-vibrate"/)
  assert.match(html, /hidden/)
  assert.match(html, /attachOpeningVibration/)
  assert.equal(html.includes('navigator.vibrate'), false)
  assert.equal(html.includes('setInterval'), false)
  assert.equal(html.includes('Audio('), false)
  assert.equal(source.includes('setInterval'), false)
  assert.match(source, /vibrate\(0\)/)
  assert.match(source, /enableFromGesture/)
  const files = walk(fileURLToPath(new URL('../src', import.meta.url))).filter((file) => file.endsWith('.ts') || file.endsWith('.vue'))
  const owners = files.filter((file) => readFileSync(file, 'utf8').includes('navigator.vibrate'))
  assert.deepEqual(owners.map((file) => file.split('/').at(-1)), ['openingVibration.ts'])
})

function walk(root: string): string[] {
  const found: string[] = []
  for (const name of readdirSync(root)) {
    const path = join(root, name)
    if (statSync(path).isDirectory()) found.push(...walk(path))
    else found.push(path)
  }
  return found
}
