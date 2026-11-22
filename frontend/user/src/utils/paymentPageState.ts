export type PaymentBootstrapPlan = 'recharge' | 'missing-order' | 'load-order'

export const planPaymentBootstrap = (input: {
  rechargeReturn: boolean
  orderNo: string
}): PaymentBootstrapPlan => {
  if (input.rechargeReturn) return 'recharge'
  if (input.orderNo.trim() === '') return 'missing-order'
  return 'load-order'
}

export type PaymentPageView = 'loading' | 'guest-form' | 'empty' | 'order'

export const resolvePaymentPageView = (input: {
  loading: boolean
  isGuest: boolean
  hasGuestAuth: boolean
  guestAuthError: string
  order: unknown
}): PaymentPageView => {
  if (input.loading) return 'loading'
  if (input.isGuest && (!input.hasGuestAuth || input.guestAuthError !== '')) return 'guest-form'
  if (!input.order) return 'empty'
  return 'order'
}

export const settlePaymentOrderLoad = (input: {
  isGuest: boolean
  hasGuestAuth: boolean
  failed: boolean
  order: unknown
}): {
  loading: false
  guestAuthErrorKey: '' | 'payment.guestAuthRequired' | 'payment.guestAuthInvalid'
  view: Exclude<PaymentPageView, 'loading'>
} => {
  let guestAuthErrorKey: '' | 'payment.guestAuthRequired' | 'payment.guestAuthInvalid' = ''
  if (input.isGuest && !input.hasGuestAuth) guestAuthErrorKey = 'payment.guestAuthRequired'
  else if (input.failed && input.isGuest) guestAuthErrorKey = 'payment.guestAuthInvalid'
  const view = resolvePaymentPageView({
    loading: false,
    isGuest: input.isGuest,
    hasGuestAuth: input.hasGuestAuth,
    guestAuthError: guestAuthErrorKey,
    order: input.failed ? null : input.order,
  })
  return {
    loading: false,
    guestAuthErrorKey,
    view: view === 'loading' ? 'empty' : view,
  }
}
