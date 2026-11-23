import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { showsAutomaticDelivery } from '../src/utils/fulfillment.ts'
import { resolveSkuAvailableStock } from '../src/utils/publicStock.ts'

test('coupon sale is shown as automatic delivery and still uses manual stock', () => {
  const product = {
    fulfillment_type: 'manual',
    coupon_sale: true,
  }
  assert.equal(showsAutomaticDelivery(product), true)
  assert.equal(showsAutomaticDelivery({ fulfillmentType: 'coupon' }), true)
  assert.equal(showsAutomaticDelivery({ fulfillment_type: 'manual' }), false)
  assert.equal(showsAutomaticDelivery('auto'), true)
  assert.equal(resolveSkuAvailableStock(product, { manual_stock_total: -1, auto_stock_available: 0 }), null)
  assert.equal(resolveSkuAvailableStock({ fulfillment_type: 'auto' }, { manual_stock_total: -1, auto_stock_available: 0 }), 0)
})

test('storefront surfaces label coupon sale without switching it to card auto', () => {
  const detail = readFileSync(new URL('../src/views/ProductDetail.vue', import.meta.url), 'utf8')
  const cart = readFileSync(new URL('../src/views/Cart.vue', import.meta.url), 'utf8')
  const checkout = readFileSync(new URL('../src/composables/useCheckout.ts', import.meta.url), 'utf8')
  const zh = readFileSync(new URL('../src/i18n/locales/zh-CN.json', import.meta.url), 'utf8')
  assert.equal(detail.includes('showsAutomaticDelivery(product)'), true)
  assert.equal(cart.includes('showsAutomaticDelivery(item)'), true)
  assert.equal(checkout.includes("item.fulfillmentType === 'coupon'"), true)
  assert.equal(zh.includes('"coupon": "自动交付"'), true)
  assert.equal(zh.includes('人工交付'), true)
})
