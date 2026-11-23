import test from 'node:test'
import assert from 'node:assert/strict'
import {
  CLASSIC_SKIN,
  CRYSTAL_SKIN,
  isResellerConsolePath,
  resolveStorefrontSkin,
} from '../src/utils/storefrontSkin.ts'

test('crystal skin is the default and classic can be remembered from the query', () => {
  assert.deepEqual(resolveStorefrontSkin(null, null), { skin: CRYSTAL_SKIN, persist: null, clear: false })
  assert.deepEqual(resolveStorefrontSkin('classic', null), { skin: CLASSIC_SKIN, persist: CLASSIC_SKIN, clear: false })
  assert.deepEqual(resolveStorefrontSkin(null, 'classic'), { skin: CLASSIC_SKIN, persist: null, clear: false })
  assert.deepEqual(resolveStorefrontSkin('crystal', 'classic'), { skin: CRYSTAL_SKIN, persist: CRYSTAL_SKIN, clear: false })
})

test('reset clears a stored skin and returns to crystal', () => {
  assert.deepEqual(resolveStorefrontSkin('reset', 'classic'), { skin: CRYSTAL_SKIN, persist: null, clear: true })
  assert.equal(resolveStorefrontSkin('nope', 'nope').skin, CRYSTAL_SKIN)
})

test('reseller console paths keep the original palette', () => {
  assert.equal(isResellerConsolePath('/reseller'), true)
  assert.equal(isResellerConsolePath('/reseller/orders'), true)
  assert.equal(isResellerConsolePath('/products'), false)
  assert.equal(isResellerConsolePath('/resellers'), false)
})
