export type PaymentResetReason = 'generic' | 'route_change' | 'change_payment_method'

export interface PaymentResetPolicy {
  resumeLatestPayment: boolean
  clearSelectedChannel: boolean
  stopActivePaymentWatch: boolean
}

export interface CachedPaymentRestorePolicy {
  startActivePaymentWatch: boolean
  autoOpenPayLink: boolean
}

export type PaymentPresentationMode = 'qr' | 'redirect'
export type PaymentLinkNavigationTarget = 'current-tab' | 'new-window'
export type PaymentInteractionLabelKey =
  | 'payment.modeQr'
  | 'payment.modeRedirect'
  | 'payment.modeWap'
  | 'payment.modePage'
export type PaymentResultTitleKey =
  | 'payment.resultTitle'
  | 'payment.resultRedirectTitle'
  | 'payment.modeWap'
  | 'payment.modePage'

const REDIRECT_PAYMENT_INTERACTION_MODES = new Set(['redirect', 'wap', 'page'])
const normalizeInteractionMode = (mode: unknown) => String(mode || '').trim().toLowerCase()

export const isCustomerSurchargePayment = (payment?: { fee_policy?: unknown } | null) => {
  const policy = String(payment?.fee_policy || '').trim().toLowerCase()
  return policy === 'customer_surcharge' || policy === 'legacy_customer_surcharge'
}

export const isRedirectPaymentInteractionMode = (mode: unknown) => {
  return REDIRECT_PAYMENT_INTERACTION_MODES.has(normalizeInteractionMode(mode))
}

export const resolvePaymentPresentationMode = (mode: unknown): PaymentPresentationMode => {
  return isRedirectPaymentInteractionMode(mode) ? 'redirect' : 'qr'
}

export const resolvePaymentLinkNavigationTarget = (automatic: boolean): PaymentLinkNavigationTarget => {
  return automatic ? 'current-tab' : 'new-window'
}

export const resolvePaymentInteractionLabelKey = (mode: unknown): PaymentInteractionLabelKey | null => {
  switch (normalizeInteractionMode(mode)) {
    case 'qr':
      return 'payment.modeQr'
    case 'redirect':
      return 'payment.modeRedirect'
    case 'wap':
      return 'payment.modeWap'
    case 'page':
      return 'payment.modePage'
    default:
      return null
  }
}

export const resolvePaymentResultTitleKey = (mode: unknown): PaymentResultTitleKey => {
  switch (normalizeInteractionMode(mode)) {
    case 'wap':
      return 'payment.modeWap'
    case 'page':
      return 'payment.modePage'
    case 'redirect':
      return 'payment.resultRedirectTitle'
    default:
      return 'payment.resultTitle'
  }
}

export const getPaymentResetPolicy = (reason: PaymentResetReason = 'generic'): PaymentResetPolicy => {
  if (reason === 'change_payment_method') {
    return {
      resumeLatestPayment: false,
      clearSelectedChannel: true,
      stopActivePaymentWatch: true,
    }
  }

  return {
    resumeLatestPayment: true,
    clearSelectedChannel: false,
    stopActivePaymentWatch: false,
  }
}

export const getCachedPaymentRestorePolicy = (): CachedPaymentRestorePolicy => ({
  startActivePaymentWatch: true,
  autoOpenPayLink: false,
})

export const shouldAutoOpenPaymentLink = (payment?: { interaction_mode?: unknown; pay_url?: unknown; fee_policy?: unknown } | null) => {
  if (!payment || isCustomerSurchargePayment(payment)) return false
  if (!isRedirectPaymentInteractionMode(payment.interaction_mode)) return false
  return resolvePaymentNavigationURL(payment.pay_url) !== null
}

export type PaymentLinkOpenResult = 'opened' | 'missing' | 'rejected' | 'blocked'

// 只接受绝对 http(s)。相对地址、javascript: 和无法解析的值不能拿去跳转，否则当前页会被换成空白。
export const resolvePaymentNavigationURL = (value: unknown): string | null => {
  const raw = String(value ?? '').trim()
  if (!raw || raw.length > 2048) return null
  try {
    const url = new URL(raw)
    if (url.protocol !== 'https:' && url.protocol !== 'http:') return null
    return url.href
  } catch {
    return null
  }
}

export const openResolvedPaymentLink = (input: {
  url: unknown
  automatic: boolean
  assign: (url: string) => void
  openWindow: (url: string) => boolean
  telegram?: boolean
  openTelegram?: (url: string) => void
}): PaymentLinkOpenResult => {
  const raw = String(input.url ?? '').trim()
  if (!raw) return 'missing'
  const safeURL = resolvePaymentNavigationURL(raw)
  if (!safeURL) return 'rejected'
  try {
    if (input.telegram && input.openTelegram) {
      input.openTelegram(safeURL)
      return 'opened'
    }
    if (resolvePaymentLinkNavigationTarget(input.automatic) === 'current-tab') {
      input.assign(safeURL)
      return 'opened'
    }
    return input.openWindow(safeURL) ? 'opened' : 'blocked'
  } catch {
    return 'rejected'
  }
}
