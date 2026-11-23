// 开场震动只使用浏览器 Vibration API。
// 规范没有强度参数；WebKit 反对且 Safari 未实现；没有粘性用户激活时 vibrate 返回 false。
// 这里不自动刷新、不申请额外权限，也不在没有手势时循环重试。

export const OPENING_VIBRATION_PATTERN = [240, 140, 240, 140, 240, 140, 240, 140, 240]
export const OPENING_VIBRATION_STORAGE_KEY = 'dj-opening-vibration'
const FULL_MOTION_MS = 3000
const REDUCED_MOTION_MS = 450

export type OpeningVibrationClock = {
  now: () => number
  schedule: (fn: () => void, ms: number) => number
  cancel: (id: number) => void
}

export type OpeningVibrationOptions = {
  vibrate?: (pattern: number | number[]) => boolean
  reducedMotion?: boolean
  writePreference?: (value: 'on' | 'off') => void
  clock?: OpeningVibrationClock
}

export type OpeningVibrationController = {
  supported: () => boolean
  pulsing: () => boolean
  enableFromGesture: () => boolean
  disable: () => void
  stop: () => void
}

export function browserVibrate(): ((pattern: number | number[]) => boolean) | null {
  if (typeof navigator === 'undefined' || typeof navigator.vibrate !== 'function') return null
  return (pattern) => navigator.vibrate(pattern)
}

export function openingVibrationLanguage(stored: string | null, navigatorLanguage: string) {
  const value = `${stored || navigatorLanguage || 'en'}`.toLowerCase()
  if (value.startsWith('zh-tw') || value.startsWith('zh-hk') || value.startsWith('zh-hant')) return 'zh-TW'
  if (value.startsWith('zh')) return 'zh-CN'
  return 'en'
}

export function openingVibrationLabel(kind: 'start' | 'stop', language: string) {
  const lang = language.toLowerCase()
  const traditional = lang.includes('tw') || lang.includes('hk') || lang.includes('hant')
  if (lang.startsWith('zh') && traditional) return kind === 'stop' ? '關閉震動' : '開啟震動'
  if (lang.startsWith('zh')) return kind === 'stop' ? '关闭震动' : '开启震动'
  return kind === 'stop' ? 'Stop vibration' : 'Start vibration'
}

export function shouldStopOpeningVibration(state: {
  hidden?: boolean
  leaving?: boolean
  stalled?: boolean
  connected?: boolean
}) {
  if (state.hidden || state.leaving || state.stalled) return true
  return state.connected === false
}

export function createOpeningVibration(options: OpeningVibrationOptions = {}): OpeningVibrationController {
  const vibrate = options.vibrate
  const reducedMotion = options.reducedMotion === true
  const writePreference = options.writePreference ?? (() => undefined)
  const clock = options.clock ?? {
    now: () => Date.now(),
    schedule: (fn, ms) => window.setTimeout(fn, ms),
    cancel: (id) => window.clearTimeout(id),
  }
  let pulsing = false
  let timer = 0
  const motionMs = reducedMotion ? REDUCED_MOTION_MS : FULL_MOTION_MS
  const startedAt = clock.now()

  const clearTimer = () => {
    if (!timer) return
    clock.cancel(timer)
    timer = 0
  }

  const cancelHardware = () => {
    if (!vibrate) return
    try {
      vibrate(0)
    } catch {
      // 取消失败也不留下重试计时器。
    }
  }

  const stop = () => {
    pulsing = false
    clearTimer()
    cancelHardware()
  }

  return {
    supported: () => typeof vibrate === 'function',
    pulsing: () => pulsing,
    enableFromGesture: () => {
      if (pulsing) return true
      if (typeof vibrate !== 'function') return false
      const remaining = motionMs - (clock.now() - startedAt)
      if (remaining <= 0) return false
      let accepted = false
      try {
        accepted = vibrate(OPENING_VIBRATION_PATTERN) === true
      } catch {
        stop()
        return false
      }
      if (!accepted) return false
      writePreference('on')
      pulsing = true
      clearTimer()
      timer = clock.schedule(() => {
        timer = 0
        stop()
      }, remaining)
      return true
    },
    disable: () => {
      writePreference('off')
      stop()
    },
    stop,
  }
}

export function attachOpeningVibration(boot: HTMLElement, options: OpeningVibrationOptions = {}) {
  const vibrate = options.vibrate === undefined ? browserVibrate() ?? undefined : options.vibrate
  const reduced = options.reducedMotion ?? (typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches)
  const controller = createOpeningVibration({
    ...options,
    vibrate,
    reducedMotion: reduced,
    writePreference: options.writePreference ?? ((value) => {
      try {
        window.localStorage.setItem(OPENING_VIBRATION_STORAGE_KEY, value)
      } catch {
        // 记不住选择时仍允许这一次手势震动。
      }
    }),
  })
  const button = boot.querySelector<HTMLButtonElement>('#boot-vibrate')
  let storedLocale: string | null = null
  try {
    storedLocale = window.localStorage.getItem('locale')
  } catch {
    storedLocale = null
  }
  const language = openingVibrationLanguage(storedLocale, typeof navigator === 'undefined' ? 'en' : navigator.language || 'en')
  const render = () => {
    if (!button) return
    button.textContent = openingVibrationLabel(controller.pulsing() ? 'stop' : 'start', language)
  }
  if (controller.supported() && button) {
    button.hidden = false
    render()
    button.addEventListener('click', () => {
      if (controller.pulsing()) controller.disable()
      else controller.enableFromGesture()
      render()
    })
  }
  const stopIfNeeded = () => {
    if (shouldStopOpeningVibration({
      hidden: typeof document !== 'undefined' && document.visibilityState === 'hidden',
      leaving: boot.classList.contains('is-leaving'),
      stalled: boot.classList.contains('is-stalled'),
      connected: boot.isConnected,
    })) controller.stop()
  }
  document.addEventListener('visibilitychange', stopIfNeeded)
  window.addEventListener('pagehide', () => controller.stop())
  const observer = new MutationObserver(stopIfNeeded)
  observer.observe(boot, { attributes: true, attributeFilter: ['class'] })
  if (boot.parentElement) observer.observe(boot.parentElement, { childList: true })
  return controller
}
