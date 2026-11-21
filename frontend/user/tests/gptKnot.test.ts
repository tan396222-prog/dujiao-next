import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { GPT_KNOT_PATH } from '../src/brand/gptKnot.ts'

test('the first-screen loader uses the same knot path as the route overlay', () => {
  const html = readFileSync(new URL('../index.html', import.meta.url), 'utf8')
  assert.equal(html.includes(GPT_KNOT_PATH), true)
  assert.equal(html.includes('gpt-knot-spin'), true)
  assert.equal(html.includes('prefers-reduced-motion'), true)
  assert.equal(GPT_KNOT_PATH.startsWith('M'), true)
  assert.equal(GPT_KNOT_PATH.includes(' '), true)
})
