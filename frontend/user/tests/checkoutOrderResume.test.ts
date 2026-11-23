import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import {
  CHECKOUT_ORDER_RESUME_TTL_MS,
  checkoutAttemptFingerprint,
  forgetPlacedCheckoutOrder,
  readPlacedCheckoutOrder,
  rememberPlacedCheckoutOrder,
  type CheckoutOrderResumeStorage,
} from '../src/utils/checkoutOrderResume.ts'

const memoryStorage = (): CheckoutOrderResumeStorage & { dump: () => string | null } => {
  let value: string | null = null
  return {
    getItem: () => value,
    setItem: (_key, next) => {
      value = next
    },
    removeItem: () => {
      value = null
    },
    dump: () => value,
  }
}

const attempt = {
  items: [
    { productId: 8, skuId: 3, quantity: 1, fulfillmentType: 'auto' },
    { productId: 2, skuId: '', quantity: 2, fulfillmentType: 'manual' },
  ],
  manualForm: { '2': { account: 'player' } },
  couponCode: ' SAVE ',
  channelId: 4,
  useBalance: false,
  guestEmail: 'Guest@Example.com',
}

test('the same checkout attempt keeps one order across a retry', () => {
  const storage = memoryStorage()
  const fingerprint = checkoutAttemptFingerprint(attempt)
  assert.equal(fingerprint, checkoutAttemptFingerprint({
    ...attempt,
    items: [...attempt.items].reverse(),
  }))
  assert.notEqual(fingerprint, checkoutAttemptFingerprint({ ...attempt, channelId: 9 }))

  const now = 1_700_000_000_000
  rememberPlacedCheckoutOrder({
    fingerprint,
    orderNo: 'DJ-1',
    guest: true,
    at: now,
  }, storage)
  assert.equal(readPlacedCheckoutOrder(fingerprint, storage, now + 1000)?.orderNo, 'DJ-1')
  assert.equal(readPlacedCheckoutOrder(fingerprint, storage, now + CHECKOUT_ORDER_RESUME_TTL_MS + 1), null)
  assert.equal(storage.dump(), null)

  rememberPlacedCheckoutOrder({ fingerprint, orderNo: 'DJ-1', guest: true, at: now }, storage)
  assert.equal(storage.dump()?.includes('order_password'), false)
  assert.equal(readPlacedCheckoutOrder(checkoutAttemptFingerprint({ ...attempt, couponCode: 'OTHER' }), storage, now), null)
  forgetPlacedCheckoutOrder(storage, 'DJ-OTHER')
  assert.equal(readPlacedCheckoutOrder(fingerprint, storage, now)?.orderNo, 'DJ-1')
  forgetPlacedCheckoutOrder(storage, 'DJ-1')
  assert.equal(readPlacedCheckoutOrder(fingerprint, storage, now), null)
})

test('checkout navigates to an existing attempt and falls back to a full page load', () => {
  const source = readFileSync(fileURLToPath(new URL('../src/composables/useCheckout.ts', import.meta.url)), 'utf8')
  assert.match(source, /readPlacedCheckoutOrder/)
  assert.match(source, /rememberPlacedCheckoutOrder/)
  assert.match(source, /window\.location\.assign\(router\.resolve\(location\)\.href\)/)
  assert.match(source, /saveGuestOrderAuth/)
})
