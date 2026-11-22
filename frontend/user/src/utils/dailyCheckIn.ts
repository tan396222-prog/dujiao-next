export interface DailyCheckInStatus {
  checked_in?: boolean
  already_claimed?: boolean
  reward_amount?: string
  balance?: string
  currency?: string
  checkin_date?: string
  next_checkin_date?: string
  debt_offset_amount?: string
  offsets_unrecovered_commission?: boolean
}

export function dailyCheckInView(status: DailyCheckInStatus | null) {
  const checkedIn = status?.checked_in === true
  return {
    showClaimButton: !checkedIn,
    reward: checkedIn ? status?.reward_amount || '' : '',
    balance: status?.balance || '',
    tomorrow: status?.next_checkin_date || '',
    offsetsDebt: status?.offsets_unrecovered_commission === true,
    debtOffset: status?.debt_offset_amount || '0.00',
  }
}
