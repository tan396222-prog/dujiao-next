/**
 * 水晶皮肤的表现层动效。
 * 只给玻璃表面加按压、回弹、光晕和桌面指针倾斜。
 * 监听都是被动的，不改原点击、键盘和滚动，也不另发一次点击。
 */

export const PRESSABLE_SELECTOR = [
  'button',
  '[role="button"]',
  'a[href]',
  '.pcard',
  '.prow',
  '.theme-pill-link',
  '.theme-wordmark',
  '[data-slot="button"]',
].join(',')

export const TILT_SELECTOR = '.pcard, .prow, .buy-panel, .store-product-grid > a'

const SKIP_SELECTOR = 'input, textarea, select, option, [contenteditable="true"], [data-slot="switch"], [data-slot="checkbox"]'

export type PointRect = { left: number; top: number; width: number; height: number }

export type Tilt = { rx: number; ry: number; gx: number; gy: number }

const round = (value: number) => {
  const next = Math.round(value * 100) / 100
  return Object.is(next, -0) ? 0 : next
}

export const motionEnabled = (skin: string | null | undefined): boolean => skin === 'crystal'

/** 指针在表面上的倾斜和光泽位置。大面板少转一点，避免购买栏甩得太厉害。 */
export const tiltForPoint = (rect: PointRect, clientX: number, clientY: number): Tilt => {
  const width = Math.max(rect.width, 1)
  const height = Math.max(rect.height, 1)
  const px = (clientX - rect.left) / width - 0.5
  const py = (clientY - rect.top) / height - 0.5
  const max = width > 560 || height > 480 ? 2.4 : 7
  const clamp = (value: number) => Math.max(-max, Math.min(max, value))
  return {
    rx: round(clamp(-py * max * 2)),
    ry: round(clamp(px * max * 2)),
    gx: round(Math.max(0, Math.min(100, ((clientX - rect.left) / width) * 100))),
    gy: round(Math.max(0, Math.min(100, ((clientY - rect.top) / height) * 100))),
  }
}

export const findPressTarget = (node: EventTarget | null): HTMLElement | null => {
  if (!(node instanceof Element)) return null
  if (node.closest(SKIP_SELECTOR)) return null
  const el = node.closest(PRESSABLE_SELECTOR)
  if (!(el instanceof HTMLElement)) return null
  if (el.matches(':disabled') || el.getAttribute('aria-disabled') === 'true') return null
  return el
}

export const findTiltTarget = (node: EventTarget | null): HTMLElement | null => {
  if (!(node instanceof Element)) return null
  const el = node.closest(TILT_SELECTOR)
  return el instanceof HTMLElement ? el : null
}

const writeTilt = (el: HTMLElement, tilt: Tilt) => {
  el.style.setProperty('--crystal-rx', `${tilt.rx}deg`)
  el.style.setProperty('--crystal-ry', `${tilt.ry}deg`)
  el.style.setProperty('--crystal-gx', `${tilt.gx}%`)
  el.style.setProperty('--crystal-gy', `${tilt.gy}%`)
}

const clearTilt = (el: HTMLElement | null) => {
  if (!el) return
  el.style.removeProperty('--crystal-rx')
  el.style.removeProperty('--crystal-ry')
  el.style.removeProperty('--crystal-gx')
  el.style.removeProperty('--crystal-gy')
}

/** 在 document 上挂一次。皮肤不是 crystal 时什么都不做。 */
export const bindCrystalMotion = (target: Window = window): (() => void) => {
  const reducedQuery = target.matchMedia('(prefers-reduced-motion: reduce)')
  let pressed: HTMLElement | null = null
  let startX = 0
  let startY = 0
  let dragged = false
  let tiltEl: HTMLElement | null = null
  const rippleTimers = new WeakMap<HTMLElement, number>()

  const skin = () => target.document.documentElement.dataset.skin
  const reduced = () => reducedQuery.matches

  const bloom = (el: HTMLElement) => {
    if (reduced()) return
    const previous = rippleTimers.get(el)
    if (previous) target.clearTimeout(previous)
    el.classList.remove('crystal-ripple')
    void el.offsetWidth
    el.classList.add('crystal-ripple')
    rippleTimers.set(el, target.setTimeout(() => {
      el.classList.remove('crystal-ripple')
      rippleTimers.delete(el)
    }, 560))
  }

  const release = (ripple: boolean) => {
    const el = pressed
    pressed = null
    if (!el) return
    el.classList.remove('crystal-pressed')
    if (ripple && !dragged) bloom(el)
  }

  const onDown = (event: PointerEvent) => {
    if (!motionEnabled(skin())) return
    if (event.button !== 0) return
    const el = findPressTarget(event.target)
    if (!el) return
    if (pressed && pressed !== el) pressed.classList.remove('crystal-pressed')
    pressed = el
    startX = event.clientX
    startY = event.clientY
    dragged = false
    const rect = el.getBoundingClientRect()
    el.style.setProperty('--crystal-px', `${event.clientX - rect.left}px`)
    el.style.setProperty('--crystal-py', `${event.clientY - rect.top}px`)
    el.style.setProperty('--crystal-rx', '0deg')
    el.style.setProperty('--crystal-ry', '0deg')
    el.classList.add('crystal-pressed')
  }

  const onMove = (event: PointerEvent | MouseEvent) => {
    const pointerType = 'pointerType' in event ? event.pointerType : 'mouse'
    if (pressed && Math.hypot(event.clientX - startX, event.clientY - startY) > 16) {
      dragged = true
      if (pointerType === 'touch') release(false)
    }
    if (!motionEnabled(skin()) || reduced() || pointerType === 'touch') return
    const el = findTiltTarget(event.target)
    if (tiltEl && tiltEl !== el) clearTilt(tiltEl)
    tiltEl = el
    if (!el || el.classList.contains('crystal-pressed')) return
    writeTilt(el, tiltForPoint(el.getBoundingClientRect(), event.clientX, event.clientY))
    const gloss = findPressTarget(event.target)
    if (gloss && gloss !== el) writeTilt(gloss, tiltForPoint(gloss.getBoundingClientRect(), event.clientX, event.clientY))
  }

  const onUp = () => release(true)
  const onCancel = () => release(false)

  const onOut = (event: PointerEvent) => {
    if (!tiltEl) return
    const next = event.relatedTarget
    if (next instanceof Node && tiltEl.contains(next)) return
    if (!(event.target instanceof Node) || !tiltEl.contains(event.target)) return
    clearTilt(tiltEl)
    tiltEl = null
  }

  const onKeyDown = (event: KeyboardEvent) => {
    if (event.repeat || (event.key !== 'Enter' && event.key !== ' ')) return
    if (!motionEnabled(skin())) return
    const el = findPressTarget(event.target)
    if (!el || event.target !== el) return
    el.classList.add('crystal-pressed')
  }

  const onKeyUp = (event: KeyboardEvent) => {
    if (event.key !== 'Enter' && event.key !== ' ') return
    const el = findPressTarget(event.target)
    if (!el || event.target !== el) return
    el.classList.remove('crystal-pressed')
    bloom(el)
  }

  const listen: AddEventListenerOptions = { capture: true, passive: true }
  const doc = target.document
  doc.addEventListener('pointerdown', onDown, listen)
  doc.addEventListener('pointermove', onMove, listen)
  doc.addEventListener('mousemove', onMove, listen)
  doc.addEventListener('pointerup', onUp, listen)
  doc.addEventListener('pointercancel', onCancel, listen)
  doc.addEventListener('pointerout', onOut, listen)
  doc.addEventListener('keydown', onKeyDown, true)
  doc.addEventListener('keyup', onKeyUp, true)

  return () => {
    doc.removeEventListener('pointerdown', onDown, listen)
    doc.removeEventListener('pointermove', onMove, listen)
    doc.removeEventListener('mousemove', onMove, listen)
    doc.removeEventListener('pointerup', onUp, listen)
    doc.removeEventListener('pointercancel', onCancel, listen)
    doc.removeEventListener('pointerout', onOut, listen)
    doc.removeEventListener('keydown', onKeyDown, true)
    doc.removeEventListener('keyup', onKeyUp, true)
  }
}
