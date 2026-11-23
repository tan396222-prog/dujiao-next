/**
 * 顾客端水晶玻璃皮肤。
 * 默认 crystal；?skin=classic 记住并回到原有 classic / vault 配色，?skin=crystal 重新打开，?skin=reset 清除记忆并回到默认。
 * 分销控制台（/reseller）保持原配色，不套这层皮肤。
 */

export const STOREFRONT_SKIN_QUERY = 'skin'
export const STOREFRONT_SKIN_STORAGE_KEY = 'dj-storefront-skin'

export const CRYSTAL_SKIN = 'crystal'
export const CLASSIC_SKIN = 'classic'

export type StorefrontSkin = typeof CRYSTAL_SKIN | typeof CLASSIC_SKIN

const RESELLER_PREFIX = '/reseller'

let preferredSkin: StorefrontSkin = CRYSTAL_SKIN

export const parseStorefrontSkin = (value: string | null | undefined): StorefrontSkin | null => {
  if (value === CRYSTAL_SKIN || value === CLASSIC_SKIN) return value
  return null
}

export const resolveStorefrontSkin = (query: string | null | undefined, stored: string | null | undefined): {
  skin: StorefrontSkin
  persist: StorefrontSkin | null
  clear: boolean
} => {
  if (query === 'reset') {
    return { skin: CRYSTAL_SKIN, persist: null, clear: true }
  }
  const fromQuery = parseStorefrontSkin(query)
  if (fromQuery) return { skin: fromQuery, persist: fromQuery, clear: false }
  const fromStore = parseStorefrontSkin(stored)
  if (fromStore) return { skin: fromStore, persist: null, clear: false }
  return { skin: CRYSTAL_SKIN, persist: null, clear: false }
}

export const isResellerConsolePath = (pathname: string): boolean => {
  return pathname === RESELLER_PREFIX || pathname.startsWith(`${RESELLER_PREFIX}/`)
}

const readStoredSkin = (): string | null => {
  try {
    return localStorage.getItem(STOREFRONT_SKIN_STORAGE_KEY)
  } catch {
    return null
  }
}

const writeStoredSkin = (resolved: ReturnType<typeof resolveStorefrontSkin>): void => {
  try {
    if (resolved.clear) {
      localStorage.removeItem(STOREFRONT_SKIN_STORAGE_KEY)
      return
    }
    if (resolved.persist) {
      localStorage.setItem(STOREFRONT_SKIN_STORAGE_KEY, resolved.persist)
    }
  } catch {
    /* localStorage 不可用时只影响本次刷新 */
  }
}

export const applyStorefrontSkin = (skin: StorefrontSkin): void => {
  if (typeof document === 'undefined') return
  document.documentElement.dataset.skin = skin
}

/** 按当前路径套上偏好皮肤；分销控制台强制 classic。 */
export const syncStorefrontSkin = (pathname: string): void => {
  applyStorefrontSkin(isResellerConsolePath(pathname) ? CLASSIC_SKIN : preferredSkin)
}

/** 应用启动时调用一次。站长正式配置不走这里，只用于本地预览和默认外观。 */
export const initStorefrontSkin = (): void => {
  if (typeof window === 'undefined') return
  const query = new URLSearchParams(window.location.search).get(STOREFRONT_SKIN_QUERY)
  const resolved = resolveStorefrontSkin(query, readStoredSkin())
  writeStoredSkin(resolved)
  preferredSkin = resolved.skin
  syncStorefrontSkin(window.location.pathname)
}
