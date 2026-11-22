const WALLPAPER_PATH =
  /^\/uploads\/(?:product|post|banner|editor|common|category|reseller|wallpaper)\/\d{4}\/\d{2}\/[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\.(?:jpg|jpeg|png|webp)$/

export const STORE_WALLPAPER_MAX_BYTES = 8 * 1024 * 1024

export function publicWallpaperPath(raw: unknown): string {
  const value = String(raw ?? '').trim()
  return WALLPAPER_PATH.test(value) ? value : ''
}

export function applyStoreWallpaper(raw: unknown, enabled: boolean) {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  const path = enabled ? publicWallpaperPath(raw) : ''
  if (!path) {
    root.removeAttribute('data-wallpaper')
    root.style.removeProperty('--store-wallpaper')
    return
  }
  root.dataset.wallpaper = '1'
  root.style.setProperty('--store-wallpaper', `url("${path}")`)
}
