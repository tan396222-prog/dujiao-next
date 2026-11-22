import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'

const require = createRequire(import.meta.url)
const vueRequire = createRequire(require.resolve('vue/package.json'))
const { parse } = vueRequire('@vue/compiler-dom') as { parse: (source: string) => { children: Array<{ type: number; tag?: string }> } }
const { parse: parseSFC } = vueRequire('vue/compiler-sfc') as {
  parse: (source: string, options: { filename: string }) => { descriptor: { template?: { content: string } } }
}

const routeViews = [
  '../src/views/Checkout.vue',
  '../src/views/Payment.vue',
  '../src/templates/vault/Checkout.vue',
  '../src/templates/vault/Payment.vue',
]

test('checkout and payment pages have one root so route transitions cannot stick blank', () => {
  for (const relativePath of routeViews) {
    const path = fileURLToPath(new URL(relativePath, import.meta.url))
    const source = readFileSync(path, 'utf8')
    const parsed = parseSFC(source, { filename: path })
    const template = parsed.descriptor.template?.content || ''
    const ast = parse(template)
    const elements = ast.children.filter((node) => node.type === 1)
    assert.equal(elements.length, 1, `${relativePath} must render a single element root`)
    assert.equal(elements[0]?.tag, 'div')
    assert.match(template, /PaymentProtectionDialog/)
  }
})

test('payment page shows guest credentials, a missing order, and a qr or cashier failure', () => {
  for (const relativePath of ['../src/views/Payment.vue', '../src/templates/vault/Payment.vue']) {
    const source = readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')
    assert.match(source, /showGuestAuthForm/)
    assert.match(source, /payment\.guestAuthTitle/)
    assert.match(source, /v-else-if="!order"/)
    assert.match(source, /payment\.orderNotFound/)
    assert.match(source, /showQRCode/)
    assert.match(source, /paymentAlert/)
    assert.match(source, /handleOpenPayLink/)
  }
  const payment = readFileSync(fileURLToPath(new URL('../src/composables/usePayment.ts', import.meta.url)), 'utf8')
  assert.match(payment, /openResolvedPaymentLink/)
  assert.match(payment, /payment\.payLinkBlocked/)
  assert.match(payment, /payment\.qrFailed/)
  assert.match(payment, /loadGuestOrderAuth\(\)/)
  assert.match(payment, /guestAuthError\.value = t\('payment\.guestAuthRequired'\)/)
})
