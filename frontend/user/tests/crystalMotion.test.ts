import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { motionEnabled, tiltForPoint } from '../src/utils/crystalMotion.ts'

test('tilt follows the pointer and stays small on a large panel', () => {
  const card = { left: 100, top: 80, width: 220, height: 280 }
  assert.deepEqual(tiltForPoint(card, 210, 220), { rx: 0, ry: 0, gx: 50, gy: 50 })
  const right = tiltForPoint(card, 320, 220)
  assert.ok(right.ry > 4)
  assert.equal(right.rx, 0)
  const top = tiltForPoint(card, 210, 80)
  assert.ok(top.rx > 4)
  const panel = tiltForPoint({ left: 0, top: 0, width: 640, height: 520 }, 640, 260)
  assert.ok(panel.ry <= 2.4)
  assert.ok(panel.ry > 0)
})

test('motion only runs for the crystal skin', () => {
  assert.equal(motionEnabled('crystal'), true)
  assert.equal(motionEnabled('classic'), false)
  assert.equal(motionEnabled(undefined), false)
})

test('crystal motion does not cancel or redispatch the original event', () => {
  const source = readFileSync(new URL('../src/utils/crystalMotion.ts', import.meta.url), 'utf8')
  assert.equal(source.includes('preventDefault'), false)
  assert.equal(source.includes('stopPropagation'), false)
  assert.equal(source.includes('dispatchEvent'), false)
  assert.equal(source.includes('.click('), false)
})
