import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const card = readFileSync(new URL('../src/components/ProductCard.vue', import.meta.url), 'utf8')
const quickBuy = readFileSync(new URL('../src/components/ProductQuickBuy.vue', import.meta.url), 'utf8')
const crystal = readFileSync(new URL('../src/styles/crystal-glass.css', import.meta.url), 'utf8')

test('every product card action is the fixed circular cart, including the lead card', () => {
  assert.equal(card.includes("{{ t('quickBuy.buyNow') }}"), false)
  assert.match(card, /:aria-label="lead \? t\('quickBuy\.buyNow'\) : t\('products\.quickBuyAria'\)"/)
  assert.equal((card.match(/class="pcard-cart/g) ?? []).length, 1)
  assert.equal((card.match(/\$emit\('quickBuy', product\)/g) ?? []).length, 2)
  assert.match(card, /max-w-\[34px\]/)
  assert.match(crystal, /html\[data-skin="crystal"\] \.pcard-cart:is\(:hover, :focus, :focus-visible, :active\)/)
  assert.match(crystal, /max-width: 44px;/)
  assert.match(quickBuy, /t\('quickBuy\.buyNow'\)/)
  assert.match(quickBuy, /t\('quickBuy\.addToCart'\)/)
})
