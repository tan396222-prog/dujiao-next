import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { h } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { createSSRApp } from 'vue'
import {
  planPaymentBootstrap,
  resolvePaymentPageView,
  settlePaymentOrderLoad,
} from '../src/utils/paymentPageState.ts'

const require = createRequire(import.meta.url)
const vueRequire = createRequire(require.resolve('vue/package.json'))
const { parse } = vueRequire('@vue/compiler-dom') as { parse: (source: string) => { children: Array<{ type: number; tag?: string }> } }
const { parse: parseSFC } = vueRequire('vue/compiler-sfc') as {
  parse: (source: string, options: { filename: string }) => { descriptor: { template?: { content: string } } }
}

const read = (relativePath: string) => readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')

const pendingOrder = {
  order_no: 'DJ-GUEST-1',
  status: 'pending_payment',
  total_amount: '12.00',
  currency: 'CNY',
}

test('pay without an order number ends the skeleton on the missing-order state', () => {
  assert.equal(planPaymentBootstrap({ rechargeReturn: false, orderNo: '' }), 'missing-order')
  assert.equal(planPaymentBootstrap({ rechargeReturn: false, orderNo: '   ' }), 'missing-order')
  const settled = settlePaymentOrderLoad({
    isGuest: false,
    hasGuestAuth: false,
    failed: false,
    order: null,
  })
  assert.equal(settled.loading, false)
  assert.equal(settled.view, 'empty')
  assert.equal(settled.guestAuthErrorKey, '')
  assert.equal(resolvePaymentPageView({
    loading: true,
    isGuest: false,
    hasGuestAuth: false,
    guestAuthError: '',
    order: null,
  }), 'loading')
})

test('guest pay without an order number shows the credential form instead of a skeleton', () => {
  assert.equal(planPaymentBootstrap({ rechargeReturn: false, orderNo: '' }), 'missing-order')
  const missingCredentials = settlePaymentOrderLoad({
    isGuest: true,
    hasGuestAuth: false,
    failed: false,
    order: null,
  })
  assert.equal(missingCredentials.loading, false)
  assert.equal(missingCredentials.view, 'guest-form')
  assert.equal(missingCredentials.guestAuthErrorKey, 'payment.guestAuthRequired')

  const savedCredentials = settlePaymentOrderLoad({
    isGuest: true,
    hasGuestAuth: true,
    failed: false,
    order: null,
  })
  assert.equal(savedCredentials.loading, false)
  assert.equal(savedCredentials.view, 'empty')
})

test('a bad order and a network failure finish loading', () => {
  const badGuestOrder = settlePaymentOrderLoad({
    isGuest: true,
    hasGuestAuth: true,
    failed: true,
    order: null,
  })
  assert.equal(badGuestOrder.loading, false)
  assert.equal(badGuestOrder.view, 'guest-form')
  assert.equal(badGuestOrder.guestAuthErrorKey, 'payment.guestAuthInvalid')

  const badMemberOrder = settlePaymentOrderLoad({
    isGuest: false,
    hasGuestAuth: false,
    failed: true,
    order: { order_no: 'stale' },
  })
  assert.equal(badMemberOrder.loading, false)
  assert.equal(badMemberOrder.view, 'empty')
  assert.equal(badMemberOrder.guestAuthErrorKey, '')
})

test('a guest order that finishes loading shows the payment view', () => {
  assert.equal(planPaymentBootstrap({ rechargeReturn: false, orderNo: 'DJ-GUEST-1' }), 'load-order')
  const settled = settlePaymentOrderLoad({
    isGuest: true,
    hasGuestAuth: true,
    failed: false,
    order: pendingOrder,
  })
  assert.equal(settled.loading, false)
  assert.equal(settled.view, 'order')
  assert.equal(settled.guestAuthErrorKey, '')
  assert.equal(resolvePaymentPageView({
    loading: false,
    isGuest: true,
    hasGuestAuth: true,
    guestAuthError: '',
    order: pendingOrder,
  }), 'order')
})

test('payment copy for the stuck skeleton is translated, and the loader is not an i18n miss', () => {
  for (const locale of ['zh-CN', 'zh-TW', 'en-US']) {
    const messages = JSON.parse(read(`../src/i18n/locales/${locale}.json`)) as {
      payment: Record<string, string>
    }
    assert.equal(typeof messages.payment.title, 'string')
    assert.equal(typeof messages.payment.subtitle, 'string')
    assert.equal(typeof messages.payment.backToOrders, 'string')
    assert.equal(typeof messages.payment.orderNotFound, 'string')
    assert.equal(typeof messages.payment.guestAuthTitle, 'string')
    assert.equal(messages.payment.title.includes('payment.'), false)
  }
  const english = JSON.parse(read('../src/i18n/locales/en-US.json')) as { payment: Record<string, string> }
  assert.equal(english.payment.title, 'Payment')
  assert.equal(english.payment.backToOrders, 'Back to orders')
  assert.equal(english.payment.subtitle, 'Choose payment method and proceed')
  assert.equal(english.payment.orderNotFound, 'Order not found or failed to load')
  assert.equal(english.payment.guestAuthTitle, 'Enter guest credentials')
})

test('mounted payment page settles a missing order and still has one root for the home transition', () => {
  const payment = read('../src/composables/usePayment.ts')
  const mountedStart = payment.indexOf('onMounted(() => {')
  const mounted = payment.slice(mountedStart, payment.indexOf('onUnmounted', mountedStart))
  assert.match(mounted, /planPaymentBootstrap/)
  assert.match(mounted, /plan === 'missing-order'/)
  assert.match(mounted, /applySettledOrderLoad\(false\)/)
  assert.doesNotMatch(mounted, /if \(!orderNoQuery\.value\) return/)
  const loadOrder = payment.slice(payment.indexOf('const loadOrder = async'), payment.indexOf('const debouncedLoadOrder ='))
  assert.match(loadOrder, /applySettledOrderLoad\(failed\)/)
  assert.match(loadOrder, /await loadLatestPayment\(\)/)

  for (const relativePath of ['../src/views/Payment.vue', '../src/templates/vault/Payment.vue']) {
    const source = read(relativePath)
    const parsed = parseSFC(source, { filename: relativePath })
    const template = parsed.descriptor.template?.content || ''
    const ast = parse(template)
    const elements = ast.children.filter((node) => node.type === 1)
    assert.equal(elements.length, 1, `${relativePath} must render a single element root`)
    assert.match(template, /paymentPageView === 'loading'/)
    assert.match(template, /paymentPageView === 'empty'/)
    assert.match(template, /paymentPageView === 'guest-form'/)
  }
})

test('a settled payment page can route back to the home product view', async () => {
  const view = resolvePaymentPageView({
    loading: false,
    isGuest: false,
    hasGuestAuth: false,
    guestAuthError: '',
    order: null,
  })
  assert.equal(view, 'empty')

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { render: () => h('main', { id: 'home-root' }, 'featured-products') } },
      {
        path: '/pay',
        component: { render: () => h('div', { id: 'payment-root', 'data-view': view }, 'order-not-found') },
      },
    ],
  })
  const renderAt = async (path: string) => {
    const app = createSSRApp({
      render: () => h('div', { id: 'app-root' }, [h(RouterView)]),
    })
    app.use(router)
    await router.replace(path)
    await router.isReady()
    return renderToString(app)
  }
  const payHtml = await renderAt('/pay')
  assert.match(payHtml, /id="payment-root"/)
  assert.match(payHtml, /data-view="empty"/)
  assert.match(payHtml, /order-not-found/)
  assert.equal(payHtml.includes('data-view="loading"'), false)

  const homeHtml = await renderAt('/')
  assert.match(homeHtml, /id="home-root"/)
  assert.match(homeHtml, /featured-products/)
  assert.equal(homeHtml.includes('order-not-found'), false)
  assert.equal(homeHtml.includes('id="payment-root"'), false)
})
