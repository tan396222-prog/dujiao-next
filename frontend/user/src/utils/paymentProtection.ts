// 结账环境检查只借用 who-are-you 的自动化矛盾判断，并在本店同源完成。
// 来源：https://github.com/yt1988/who-are-you
// commit 0836962988ff1cb92e086fa45499a7fb0de5b4ad
// MIT License, Copyright (c) 2026 yt1988
// 不采集 canvas、字体、WebRTC、地理位置，也不向任何第三方发送数据。

export interface AutomationHints {
  webdriver: boolean
  traces: string[]
}

export interface ProtectionPrecheck {
  banned: boolean
  retry_after_seconds: number
  signal: string
  token: string
}

export function collectAutomationHints(): AutomationHints {
  const traces: string[] = []
  const agent = navigator.userAgent || ''
  if (/HeadlessChrome/i.test(agent)) traces.push('headless')
  const weak = [
    window.outerWidth === 0 || window.outerHeight === 0,
    window.innerWidth === 0,
    navigator.languages.length === 0,
    /Chrome/i.test(agent) && !('chrome' in window),
  ].filter(Boolean).length
  if (weak >= 2) traces.push('headless')
  const driver = Object.getOwnPropertyNames(window).some((name) => /^(cdc_|__playwright|__puppeteer|__nightmare|callPhantom|_phantom|domAutomation|_selenium)/.test(name))
  if (driver) traces.push('cdp')
  const versions = (window as Window & { process?: { versions?: { electron?: string } } }).process?.versions
  if (versions?.electron) traces.push('electron')
  return { webdriver: navigator.webdriver === true, traces: [...new Set(traces)].slice(0, 4) }
}

export function protectionHeaders(token: string): Record<string, string> {
  const value = token.trim()
  return value ? { 'X-DJ-Protection': value } : {}
}

export async function requestProtection(input: {
  authenticated: boolean
  bind: string
  signal?: AbortSignal
}): Promise<ProtectionPrecheck> {
  const { userOrderAPI, guestOrderAPI } = await import('../api/order')
  const hints = collectAutomationHints()
  const api = input.authenticated ? userOrderAPI : guestOrderAPI
  const response = await api.precheck({ bind: input.bind, webdriver: hints.webdriver, traces: hints.traces }, { signal: input.signal })
  return response.data.data as ProtectionPrecheck
}
