const STORAGE_KEY = 'dj-checkout-placed-order'
export const CHECKOUT_ORDER_RESUME_TTL_MS = 30 * 60 * 1000

export interface CheckoutAttemptItem {
  productId: unknown
  skuId: unknown
  quantity: unknown
  fulfillmentType?: unknown
}

export interface CheckoutAttempt {
  items: CheckoutAttemptItem[]
  manualForm?: unknown
  couponCode?: unknown
  channelId?: unknown
  useBalance?: unknown
  guestEmail?: unknown
}

export interface PlacedCheckoutOrder {
  fingerprint: string
  orderNo: string
  guest: boolean
  at: number
}

export interface CheckoutOrderResumeStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
  removeItem(key: string): void
}

const text = (value: unknown) => String(value ?? '').trim()

export const checkoutAttemptFingerprint = (attempt: CheckoutAttempt): string => {
  const items = attempt.items.map((item) => ({
    productId: text(item.productId),
    skuId: text(item.skuId),
    quantity: text(item.quantity),
    fulfillmentType: text(item.fulfillmentType),
  })).sort((left, right) => `${left.productId}:${left.skuId}`.localeCompare(`${right.productId}:${right.skuId}`))
  return JSON.stringify({
    items,
    manualForm: attempt.manualForm ?? {},
    couponCode: text(attempt.couponCode),
    channelId: text(attempt.channelId),
    useBalance: Boolean(attempt.useBalance),
    guestEmail: text(attempt.guestEmail).toLowerCase(),
  })
}

const readRaw = (storage: CheckoutOrderResumeStorage): PlacedCheckoutOrder | null => {
  try {
    const raw = storage.getItem(STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<PlacedCheckoutOrder>
    const orderNo = text(parsed.orderNo)
    const fingerprint = text(parsed.fingerprint)
    const at = Number(parsed.at)
    if (!orderNo || !fingerprint || !Number.isFinite(at)) return null
    return { fingerprint, orderNo, guest: Boolean(parsed.guest), at }
  } catch {
    return null
  }
}

export const rememberPlacedCheckoutOrder = (order: PlacedCheckoutOrder, storage: CheckoutOrderResumeStorage) => {
  const orderNo = text(order.orderNo)
  const fingerprint = text(order.fingerprint)
  if (!orderNo || !fingerprint) return
  try {
    storage.setItem(STORAGE_KEY, JSON.stringify({
      fingerprint,
      orderNo,
      guest: Boolean(order.guest),
      at: Number(order.at) || Date.now(),
    }))
  } catch {
    // 存储不可用时仍继续跳转支付页。
  }
}

export const readPlacedCheckoutOrder = (
  fingerprint: string,
  storage: CheckoutOrderResumeStorage,
  now = Date.now(),
  ttlMs = CHECKOUT_ORDER_RESUME_TTL_MS,
): PlacedCheckoutOrder | null => {
  const current = readRaw(storage)
  if (!current || current.fingerprint !== fingerprint) return null
  if (now - current.at > ttlMs) {
    forgetPlacedCheckoutOrder(storage)
    return null
  }
  return current
}

export const forgetPlacedCheckoutOrder = (storage: CheckoutOrderResumeStorage, orderNo?: string) => {
  const current = readRaw(storage)
  if (!current) return
  if (orderNo && current.orderNo !== text(orderNo)) return
  try {
    storage.removeItem(STORAGE_KEY)
  } catch {
    // 清不掉时最多保留到读取时过期。
  }
}
