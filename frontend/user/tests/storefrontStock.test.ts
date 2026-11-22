import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { allowsBackorder, resolveStockTier, stockCapsPurchase, stockTierMessageKey } from '../src/utils/publicStock.ts'

test('storefront stock tiers hide the exact count and allow manual or auto presale', () => {
  assert.equal(resolveStockTier({ fulfillment_type: 'manual', manual_stock_total: 0 }), 'presale')
  assert.equal(resolveStockTier({ fulfillment_type: 'auto', auto_stock_available: 0 }), 'presale')
  assert.equal(resolveStockTier({ fulfillment_type: 'manual', manual_stock_total: 5 }), 'low')
  assert.equal(resolveStockTier({ fulfillment_type: 'manual', manual_stock_total: 20 }), 'mid')
  assert.equal(resolveStockTier({ fulfillment_type: 'manual', manual_stock_total: 21 }), 'high')
  assert.equal(resolveStockTier({ fulfillment_type: 'manual', manual_stock_total: -1 }), 'high')
  assert.equal(resolveStockTier({ fulfillment_type: 'upstream', upstream_stock: 0 }), 'unavailable')
  assert.equal(allowsBackorder({ fulfillment_type: 'manual' }), true)
  assert.equal(allowsBackorder({ fulfillment_type: 'auto' }), true)
  assert.equal(stockCapsPurchase({ fulfillment_type: 'manual' }), false)
  assert.equal(stockCapsPurchase({ fulfillment_type: 'upstream' }), true)
  assert.equal(allowsBackorder({ fulfillment_type: 'manual', backorder: false, manual_stock_total: 0 }), false)
  assert.equal(resolveStockTier({ fulfillment_type: 'manual', backorder: false, manual_stock_total: 0 }), 'unavailable')
  assert.equal(stockCapsPurchase({ fulfillment_type: 'manual', backorder: false }), true)
  assert.equal(allowsBackorder({ fulfillment_type: 'manual', backorder: true, manual_stock_total: 0 }), true)
  assert.equal(resolveStockTier({
    fulfillment_type: 'manual',
    stock_display_mode: 'hidden',
    stock_quantity_hidden: true,
    stock_status: 'in_stock',
    manual_stock_available: 0,
  }), 'high')
  assert.equal(resolveStockTier({
    fulfillment_type: 'auto',
    stock_display_mode: 'range',
    stock_quantity_hidden: true,
    stock_status: 'low_stock',
    stock_display: 'range_1_5',
    auto_stock_available: 0,
    backorder: true,
  }), 'low')
  assert.equal(resolveStockTier({
    fulfillment_type: 'auto',
    stock_display_mode: 'range',
    stock_quantity_hidden: true,
    stock_status: 'in_stock',
    stock_display: 'range_6_20',
    auto_stock_available: 0,
    backorder: true,
  }), 'mid')
  assert.equal(resolveStockTier({
    fulfillment_type: 'auto',
    stock_display_mode: 'range',
    stock_quantity_hidden: true,
    stock_status: 'in_stock',
    stock_display: 'range_21_50',
    auto_stock_available: 0,
    backorder: true,
  }), 'high')
  assert.equal(resolveStockTier({
    fulfillment_type: 'auto',
    stock_display_mode: 'range',
    stock_quantity_hidden: true,
    stock_status: 'out_of_stock',
    auto_stock_available: 0,
    backorder: true,
  }), 'presale')
  assert.equal(resolveStockTier({
    fulfillment_type: 'auto',
    stock_display_mode: 'exact',
    stock_quantity_hidden: false,
    auto_stock_available: 1,
  }), 'low')
  assert.equal(resolveStockTier({
    fulfillment_type: 'auto',
    stock_display_mode: 'exact',
    stock_quantity_hidden: false,
    auto_stock_available: 20,
  }), 'mid')
  assert.equal(resolveStockTier({
    fulfillment_type: 'auto',
    stock_display_mode: 'exact',
    stock_quantity_hidden: false,
    auto_stock_available: 21,
  }), 'high')
  assert.equal(stockTierMessageKey('presale').includes('stockTier'), true)
  for (const tier of ['presale', 'low', 'mid', 'high', 'unavailable'] as const) {
    assert.equal(/\d/.test(stockTierMessageKey(tier)), false)
  }
})

test('bottom nav invites instead of repeating products, and login stays on the route', () => {
  const nav = readFileSync(new URL('../src/components/MobileBottomNav.vue', import.meta.url), 'utf8')
  assert.match(nav, /path: '\/me\/affiliate'/)
  assert.match(nav, /bottomNav\.invite/)
  assert.equal(nav.includes("path: '/products'"), false)
  assert.equal(nav.includes('LayoutGrid'), false)
})
