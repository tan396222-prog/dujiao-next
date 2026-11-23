export type CheckInCelebration = 'none' | 'fireworks' | 'static'

const VIBRATION_PREFERENCE_KEY = 'dj-opening-vibration'

export const CHECKIN_VIBRATION_PATTERN = [90, 70, 90, 70, 160]
export const CHECKIN_FIREWORK_MS = 1600

export const CHECKIN_SPARKS = [
  { x: '-92px', y: '-128px', color: '#d92d20' },
  { x: '96px', y: '-116px', color: '#f2b705' },
  { x: '-18px', y: '-150px', color: '#ef6a3a' },
  { x: '28px', y: '-138px', color: '#d92d20' },
  { x: '-124px', y: '-36px', color: '#f2b705' },
  { x: '128px', y: '-28px', color: '#ef6a3a' },
  { x: '-78px', y: '36px', color: '#d92d20' },
  { x: '82px', y: '42px', color: '#f2b705' },
  { x: '-36px', y: '72px', color: '#ef6a3a' },
  { x: '40px', y: '64px', color: '#d92d20' },
] as const

export type CheckInClaimResult = {
  source: 'status' | 'claim'
  ok: boolean
  checkedIn?: boolean
  alreadyClaimed?: boolean
  reducedMotion?: boolean
}

// A celebration exists only for a claim the server says is new.
// Loading today's status, a replay, a failure, or a missing flag does nothing.
export const checkInCelebration = (input: CheckInClaimResult): CheckInCelebration => {
  if (input.source !== 'claim' || input.ok !== true || input.checkedIn !== true || input.alreadyClaimed !== false) {
    return 'none'
  }
  return input.reducedMotion ? 'static' : 'fireworks'
}

export const checkInVibrationAllowed = (celebration: CheckInCelebration, supported: boolean, preference: string | null) => (
  celebration === 'fireworks' && supported && preference !== 'off'
)

export type CheckInClock = {
  schedule: (fn: () => void, ms: number) => number
  cancel: (id: number) => void
}

export type CheckInBurst = {
  start: () => void
  stop: () => void
  active: () => boolean
}

export function createCheckInBurst(options: {
  celebration: CheckInCelebration
  vibrate?: (pattern: number | number[]) => boolean
  preference?: string | null
  clock?: CheckInClock
  onDone?: () => void
}): CheckInBurst {
  const clock = options.clock ?? {
    schedule: (fn, ms) => window.setTimeout(fn, ms),
    cancel: (id) => window.clearTimeout(id),
  }
  let timer = 0
  let active = false
  let finished = false

  const finish = () => {
    if (finished) return
    finished = true
    active = false
    if (timer) {
      clock.cancel(timer)
      timer = 0
    }
    if (options.vibrate) {
      try {
        options.vibrate(0)
      } catch {
        // A failed cancel is not retried.
      }
    }
    options.onDone?.()
  }

  return {
    active: () => active,
    start: () => {
      if (active || finished || options.celebration === 'none') return
      active = true
      if (checkInVibrationAllowed(options.celebration, typeof options.vibrate === 'function', options.preference ?? null)) {
        try {
          options.vibrate?.(CHECKIN_VIBRATION_PATTERN)
        } catch {
          // Unsupported or rejected vibration is not retried.
        }
      }
      if (options.celebration === 'static') return
      timer = clock.schedule(finish, CHECKIN_FIREWORK_MS)
    },
    stop: finish,
  }
}

export const storedVibrationPreference = () => {
  try {
    return window.localStorage.getItem(VIBRATION_PREFERENCE_KEY)
  } catch {
    return null
  }
}
