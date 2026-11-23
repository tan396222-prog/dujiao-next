import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { dailyCheckInView } from '../src/utils/dailyCheckIn.ts'

const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('daily check-in waits for a click and does not offset commission debt', () => {
  const idle = dailyCheckInView(null)
  assert.equal(idle.showClaimButton, true)
  assert.equal(idle.reward, '')
  assert.equal(idle.offsetsDebt, false)
  const claimed = dailyCheckInView({
    checked_in: true,
    reward_amount: '0.40',
    balance: '1.20',
    next_checkin_date: '2026-11-23',
    debt_offset_amount: '0.00',
    offsets_unrecovered_commission: false,
  })
  assert.equal(claimed.showClaimButton, false)
  assert.equal(claimed.reward, '0.40')
  assert.equal(claimed.balance, '1.20')
  assert.equal(claimed.tomorrow, '2026-11-23')
  assert.equal(claimed.offsetsDebt, false)
  const card = read('../src/components/DailyCheckInCard.vue')
  assert.match(card, /onMounted\(\(\) => \{\s*loadCheckIn\(\)/)
  assert.equal(card.includes('claimCheckIn()'), false)
  assert.match(card, /@click="claimCheckIn"/)
})

test('check-in copy shows the actual reward and hides every amount range', () => {
  const card = read('../src/components/DailyCheckInCard.vue')
  const celebration = read('../src/components/CheckInCelebration.vue')
  const api = read('../src/api/checkin.ts')
  assert.match(api, /post\('\/daily-checkins', \{\}\)/)
  const visible = `${card.split('<script')[0]}\n${celebration.split('<style')[0]}`
  assert.equal(/\d+\.\d{2}/.test(visible), false)
  for (const locale of ['zh-CN', 'zh-TW', 'en-US']) {
    const messages = JSON.parse(read(`../src/i18n/locales/${locale}.json`))
    const checkin = messages.personalCenter.checkin
    const blob = JSON.stringify(checkin)
    for (const leaked of ['0.10', '0.80', '0.01', '0.30', '1.80', '2.20', '3%', '首签', '首簽', 'first check-in']) {
      assert.equal(blob.includes(leaked), false, `${locale} leaked ${leaked}`)
    }
    assert.match(checkin.subtitle, /withdrawn|不支持提现|不支持提現/)
    assert.match(checkin.reward, /reward|奖励|獎勵/)
    assert.match(checkin.balance, /balance|余额|餘額/)
    assert.match(checkin.tomorrow, /\{date\}/)
  }
  const claimed = dailyCheckInView({
    checked_in: true,
    reward_amount: '1.80',
    balance: '2.49',
    next_checkin_date: '2026-11-24',
    debt_offset_amount: '0.00',
    offsets_unrecovered_commission: false,
  })
  assert.equal(claimed.reward, '1.80')
  assert.equal(claimed.balance, '2.49')
  assert.equal(claimed.tomorrow, '2026-11-24')
})

test('the rebate page no longer offers a withdrawal form', () => {
  const panel = read('../src/views/personal/AffiliatePanel.vue')
  assert.equal(panel.includes('handleApplyWithdraw'), false)
  assert.equal(panel.includes('withdrawForm'), false)
  assert.equal(panel.includes('applyWithdraw'), false)
  assert.match(panel, /balanceShopOnly/)
  assert.match(panel, /DailyCheckInCard/)
  for (const locale of ['zh-CN', 'zh-TW', 'en-US']) {
    const messages = read(`../src/i18n/locales/${locale}.json`)
    assert.match(messages, /balanceShopOnly/)
    assert.match(messages, /checkin/)
    assert.equal(messages.includes('可提现佣金'), false)
    assert.equal(messages.includes('可提現佣金'), false)
  }
})

test('the announcement sheet is opaque white and uses the original qr bytes', () => {
  const modal = read('../src/components/AnnouncementModal.vue')
  assert.match(modal, /#ffffff/)
  assert.match(modal, /#111111/)
  assert.equal(modal.includes('dark:prose-invert'), false)
  assert.match(modal, /TCY15216367909/)
  assert.match(modal, /\/wechat-qr\.jpg/)
  assert.equal(modal.includes('object-fit'), false)
  assert.match(modal, /dismissToday/)
  assert.match(modal, /dismissForever/)
  const crystal = read('../src/styles/crystal-glass.css')
  const glassRule = crystal.indexOf('html[data-skin="crystal"] :is([role="dialog"], [role="alertdialog"])')
  const solidRule = crystal.indexOf('html[data-skin="crystal"] [data-announcement-sheet]')
  assert.ok(glassRule >= 0 && solidRule > glassRule)
  assert.match(crystal, /\[data-announcement-sheet\][\s\S]*background-color: #ffffff !important/)
  assert.match(crystal, /-webkit-backdrop-filter: none !important/)
  const bytes = readFileSync(new URL('../public/wechat-qr.jpg', import.meta.url))
  assert.equal(bytes.subarray(0, 3).toString('hex'), 'ffd8ff')
  assert.equal(bytes.subarray(bytes.length - 2).toString('hex'), 'ffd9')
  assert.equal(createHash('sha256').update(bytes).digest('hex'), 'ef0eed3f512f048ccae18fb7f64161b11385aa182c84b482161d41b473386009')
})
