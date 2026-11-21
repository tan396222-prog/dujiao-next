const STOCK_DISPLAY_EXACT = 'exact'
const STOCK_DISPLAY_HIDDEN = 'hidden'
const STOCK_STATUS_UNLIMITED = 'unlimited'
const STOCK_STATUS_IN_STOCK = 'in_stock'
const STOCK_STATUS_LOW_STOCK = 'low_stock'
const STOCK_STATUS_OUT_OF_STOCK = 'out_of_stock'

export type PublicStockDisplay =
  | { kind: 'unlimited' }
  | { kind: 'out' }
  | { kind: 'remaining'; count: number }
  | { kind: 'in_stock' }
  | { kind: 'low_stock' }
  | { kind: 'hidden' }
  | { kind: 'range'; min: number; max: number }
  | { kind: 'range_plus'; min: number }

const normalizeStockNumber = (value: unknown) => {
  const numberValue = Number(value)
  if (!Number.isFinite(numberValue)) return 0
  return Math.max(Math.floor(numberValue), 0)
}

const normalizeManualStockTotal = (value: unknown) => {
  const numberValue = Number(value)
  if (!Number.isFinite(numberValue)) return 0
  const integerValue = Math.floor(numberValue)
  if (integerValue === -1) return -1
  return Math.max(integerValue, 0)
}

const normalizeStockStatus = (value: unknown, fallbackQuantity: number | null) => {
  const status = String(value || '').trim()
  if (
    status === STOCK_STATUS_UNLIMITED ||
    status === STOCK_STATUS_IN_STOCK ||
    status === STOCK_STATUS_LOW_STOCK ||
    status === STOCK_STATUS_OUT_OF_STOCK
  ) {
    return status
  }
  if (fallbackQuantity === null || fallbackQuantity === -1) return STOCK_STATUS_UNLIMITED
  if (fallbackQuantity <= 0) return STOCK_STATUS_OUT_OF_STOCK
  if (fallbackQuantity <= 5) return STOCK_STATUS_LOW_STOCK
  return STOCK_STATUS_IN_STOCK
}

const isQuantityHidden = (product: any, sku: any) => {
  if (sku?.stock_quantity_hidden === true) return true
  if (product?.stock_quantity_hidden === true) return true
  return String(product?.stock_display_mode || sku?.stock_display_mode || STOCK_DISPLAY_EXACT).trim() !== STOCK_DISPLAY_EXACT
}

export const resolveSkuAvailableStock = (product: any, sku: any): number | null => {
  if (!sku) return 0
  if (isQuantityHidden(product, sku)) {
    const status = normalizeStockStatus(sku?.stock_status, null)
    if (status === STOCK_STATUS_OUT_OF_STOCK) return 0
    return null
  }
  if (product?.fulfillment_type === 'upstream') {
    const upstreamStock = Number(sku?.upstream_stock ?? 0)
    if (upstreamStock === -1) return null
    return Math.max(Math.floor(upstreamStock), 0)
  }
  if (product?.fulfillment_type === 'auto') {
    const autoStock = Number(sku?.auto_stock_available ?? 0)
    if (autoStock < 0) return null
    return normalizeStockNumber(autoStock)
  }
  const total = normalizeManualStockTotal(sku?.manual_stock_total)
  if (total === -1) return null
  return total
}

export const resolveSkuStockDisplay = (product: any, sku: any): PublicStockDisplay => {
  const available = resolveSkuAvailableStock(product, sku)
  const hidden = isQuantityHidden(product, sku)
  const status = normalizeStockStatus(sku?.stock_status, available)

  if (status === STOCK_STATUS_UNLIMITED) return { kind: 'unlimited' }
  if (status === STOCK_STATUS_OUT_OF_STOCK) return { kind: 'out' }

  if (!hidden) {
    return { kind: 'remaining', count: Math.max(available ?? 0, 0) }
  }

  const display = String(sku?.stock_display || '').trim()
  const min = Number(sku?.stock_range_min)
  const max = Number(sku?.stock_range_max)
  if (display.startsWith('range_') && Number.isFinite(min) && min > 0) {
    if (Number.isFinite(max) && max >= min) {
      return { kind: 'range', min: Math.floor(min), max: Math.floor(max) }
    }
    return { kind: 'range_plus', min: Math.floor(min) }
  }
  if (display === STOCK_DISPLAY_HIDDEN) return { kind: 'hidden' }
  if (status === STOCK_STATUS_LOW_STOCK) return { kind: 'low_stock' }
  return { kind: 'in_stock' }
}

export type StockTier = 'presale' | 'low' | 'mid' | 'high' | 'unavailable'

// allowsBackorder 人工和自动发货可以在没有库存时下单，付款后等补货再交付。
export const allowsBackorder = (product: any) => {
  if (!product) return false
  if (product.backorder === true) return true
  if (product.backorder === false) return false
  const type = String(product.fulfillment_type || 'manual').trim()
  return type === 'manual' || type === 'auto' || type === ''
}

// stockCapsPurchase 上游和明确不可预售的商品仍按真实库存限制购买数量。
export const stockCapsPurchase = (product: any) => {
  if (!product) return false
  if (product.backorder === true) return false
  if (product.backorder === false) return true
  return String(product.fulfillment_type || '').trim() === 'upstream'
}

const productStockQuantity = (product: any): number | null => {
  if (!product) return 0
  const status = String(product.stock_status || '')
  if (status === STOCK_STATUS_UNLIMITED) return null
  if (product.fulfillment_type === 'upstream') {
    const upstream = Number(product.upstream_stock ?? product.manual_stock_total ?? 0)
    if (upstream === -1) return null
    return Math.max(Math.floor(upstream), 0)
  }
  if (product.fulfillment_type === 'auto') {
    const autoStock = Number(product.auto_stock_available ?? 0)
    if (autoStock < 0) return null
    return normalizeStockNumber(autoStock)
  }
  const total = normalizeManualStockTotal(product.manual_stock_available ?? product.manual_stock_total)
  if (total === -1) return null
  return total
}

export const resolveStockTier = (product: any, sku?: any): StockTier => {
  const status = String((sku || product)?.stock_status || product?.stock_status || '')
  if (isQuantityHidden(product, sku)) {
    if (status === STOCK_STATUS_OUT_OF_STOCK) return allowsBackorder(product) ? 'presale' : 'unavailable'
    if (status === STOCK_STATUS_LOW_STOCK) return 'low'
    return 'high'
  }
  const available = sku ? resolveSkuAvailableStock(product, sku) : productStockQuantity(product)
  if (available === null) {
    if (status === STOCK_STATUS_UNLIMITED) return 'high'
    if (status === STOCK_STATUS_OUT_OF_STOCK) return allowsBackorder(product) ? 'presale' : 'unavailable'
    if (status === STOCK_STATUS_LOW_STOCK) return 'low'
    return 'high'
  }
  if (available <= 0) return allowsBackorder(product) ? 'presale' : 'unavailable'
  if (available <= 5) return 'low'
  if (available <= 20) return 'mid'
  return 'high'
}

export const stockTierMessageKey = (tier: StockTier) => {
  switch (tier) {
    case 'presale':
      return 'products.stockTier.presale'
    case 'low':
      return 'products.stockTier.low'
    case 'mid':
      return 'products.stockTier.mid'
    case 'unavailable':
      return 'products.stockTier.unavailable'
    default:
      return 'products.stockTier.high'
  }
}
