type TranslateFn = (key: string, params?: Record<string, any>) => string

export type FulfillmentLabelScope = 'orderDetail'

type DeliverySource = {
  fulfillment_type?: unknown
  fulfillmentType?: unknown
  coupon_sale?: unknown
  couponSale?: unknown
}

// 售券商品库存类型保持 manual，避免被当成需要预存卡密的自动发货。
// 前台只在展示时把它和 coupon 交付记录视为自动交付。
export const showsAutomaticDelivery = (source?: DeliverySource | string | null) => {
  if (typeof source === 'string') return source === 'auto' || source === 'coupon'
  if (!source) return false
  if (source.coupon_sale === true || source.couponSale === true) return true
  const type = String(source.fulfillment_type || source.fulfillmentType || '')
  return type === 'auto' || type === 'coupon'
}

export const fulfillmentTypeLabel = (t: TranslateFn, type?: string, scope: FulfillmentLabelScope = 'orderDetail') => {
  if (!type) return '-'
  const baseKey = `${scope}.fulfillmentTypes`
  const map: Record<string, string> = {
    manual: t(`${baseKey}.manual`),
    auto: t(`${baseKey}.auto`),
    upstream: t(`${baseKey}.manual`),
    coupon: t(`${baseKey}.coupon`),
  }
  return map[type] || type
}

export const fulfillmentStatusLabel = (t: TranslateFn, status?: string, scope: FulfillmentLabelScope = 'orderDetail') => {
  if (!status) return '-'
  const baseKey = `${scope}.fulfillmentStatuses`
  const map: Record<string, string> = {
    pending: t(`${baseKey}.pending`),
    delivered: t(`${baseKey}.delivered`),
  }
  return map[status] || status
}
