export const FEED_REFRESH_MS = 10 * 60 * 1000

export type FeedRefreshClock = {
  schedule: (fn: () => void, delay: number) => number
  cancel: (id: number) => void
}

const browserClock: FeedRefreshClock = {
  schedule(fn, delay) {
    return globalThis.setTimeout(fn, delay) as unknown as number
  },
  cancel(id) {
    globalThis.clearTimeout(id)
  },
}

// createFeedRefresh polls this site's cached feed while the page is visible.
// A hidden tab drops its timer. Coming back schedules one refresh. start is
// idempotent, and stop clears the timer.
export const createFeedRefresh = (options: {
  reload: () => void
  hidden: () => boolean
  intervalMs?: number
  clock?: FeedRefreshClock
}) => {
  const intervalMs = options.intervalMs ?? FEED_REFRESH_MS
  const clock = options.clock ?? browserClock
  let timer = 0
  let started = false

  const arm = () => {
    if (timer) clock.cancel(timer)
    timer = clock.schedule(() => {
      const current = timer
      timer = 0
      if (current) clock.cancel(current)
      if (!started || options.hidden()) return
      options.reload()
      arm()
    }, intervalMs)
  }

  return {
    start() {
      if (started) return
      started = true
      arm()
    },
    stop() {
      started = false
      if (timer) clock.cancel(timer)
      timer = 0
    },
    onVisibility() {
      if (!started) return
      if (options.hidden()) {
        if (timer) clock.cancel(timer)
        timer = 0
        return
      }
      if (timer) return
      options.reload()
      arm()
    },
  }
}
