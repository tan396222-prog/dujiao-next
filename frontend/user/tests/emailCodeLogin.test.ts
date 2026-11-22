import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('email-code login stays beside password login and is not gated by registration verification', () => {
  const pages = [
    read('../src/views/auth/Login.vue'),
    read('../src/templates/vault/auth/Login.vue'),
  ]
  for (const source of pages) {
    const switchAt = source.indexOf('data-login-mode-switch')
    const emailCodeAt = source.indexOf('data-login-mode-email-code')
    assert.ok(switchAt > 0 && emailCodeAt > switchAt)
    const modeSwitch = source.slice(switchAt, emailCodeAt)
    assert.equal(modeSwitch.includes('emailVerificationEnabled'), false)
    assert.match(source, /auth\.login\.modeEmailCode/)
    assert.match(source, /auth\.login\.modePassword/)
    assert.match(source, /sendLoginEmailCode/)
  }
})

test('a failed login-code send shows the error and does not start the resend timer', () => {
  const source = read('../src/composables/useLogin.ts')
  const send = source.slice(source.indexOf('const sendLoginEmailCode'), source.indexOf('const performLogin'))
  assert.match(send, /userAuthAPI\.sendLoginEmailCode/)
  assert.match(send, /info\.value = t\('auth\.login\.emailCodeSent'\)/)
  assert.match(send, /startEmailCodeCountdown\(\)/)
  const failure = send.slice(send.indexOf('} catch'))
  assert.equal(failure.includes('startEmailCodeCountdown'), false)
  assert.match(failure, /error\.value = err\?\.message/)
})

test('email-code login reuses the password login response, including two-factor', () => {
  const store = read('../src/stores/userAuth.ts')
  const method = store.slice(store.indexOf('const loginWithEmailCode'), store.indexOf('const verify2FA'))
  assert.match(method, /userAuthAPI\.loginWithEmailCode/)
  assert.match(method, /return handleLoginResponse/)
})
