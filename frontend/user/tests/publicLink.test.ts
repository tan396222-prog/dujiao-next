import test from 'node:test'
import assert from 'node:assert/strict'
import { publicHttpLink } from '../src/utils/publicLink.ts'

test('public links keep http and https and drop unsafe targets', () => {
  assert.equal(publicHttpLink('https://aihot.news/codex-reset'), 'https://aihot.news/codex-reset')
  assert.equal(publicHttpLink('http://example.com/a'), 'http://example.com/a')
  assert.equal(publicHttpLink('javascript:alert(1)'), '')
  assert.equal(publicHttpLink('data:text/html,hi'), '')
  assert.equal(publicHttpLink('https://user:pass@example.com/a'), '')
  assert.equal(publicHttpLink('not a url'), '')
})
