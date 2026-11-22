<template>
  <section class="rounded-2xl border bg-card p-7 shadow-sm" data-daily-checkin>
    <h2 class="text-lg font-bold text-foreground">{{ t('personalCenter.checkin.title') }}</h2>
    <p class="mt-2 text-sm font-semibold text-foreground">{{ t('personalCenter.checkin.subtitle') }}</p>
    <p class="mt-3 text-sm text-muted-foreground" data-checkin-state>{{ stateText }}</p>
    <p v-if="view.reward" class="mt-2 text-sm font-bold text-foreground" data-checkin-reward>
      {{ t('personalCenter.checkin.reward') }} {{ view.reward }}
    </p>
    <p v-if="view.balance" class="mt-2 text-sm font-semibold text-foreground" data-checkin-balance>
      {{ t('personalCenter.checkin.balance') }} {{ view.balance }}
    </p>
    <p v-if="view.tomorrow" class="mt-2 text-sm text-foreground">
      {{ t('personalCenter.checkin.tomorrow', { date: view.tomorrow }) }}
    </p>
    <p class="mt-3 text-sm font-semibold text-foreground">{{ t('personalCenter.checkin.debtNote') }}</p>
    <p v-if="view.offsetsDebt" class="mt-2 text-sm font-semibold text-foreground">
      {{ t('personalCenter.checkin.debtOffset', { amount: view.debtOffset }) }}
    </p>
    <p v-if="errorMessage" class="mt-3 text-sm font-semibold text-destructive">{{ errorMessage }}</p>
    <Button
      v-if="view.showClaimButton"
      type="button"
      class="mt-4 font-bold"
      data-checkin-claim
      :disabled="claiming"
      @click="claimCheckIn"
    >
      {{ claiming ? t('personalCenter.checkin.claiming') : t('personalCenter.checkin.claim') }}
    </Button>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { checkInAPI } from '../api/checkin'
import { dailyCheckInView, type DailyCheckInStatus } from '../utils/dailyCheckIn'
import { Button } from '@/components/ui/button'

const { t } = useI18n()
const status = ref<DailyCheckInStatus | null>(null)
const claiming = ref(false)
const errorMessage = ref('')
const view = computed(() => dailyCheckInView(status.value))
const stateText = computed(() => (
  status.value?.checked_in ? t('personalCenter.checkin.claimed') : t('personalCenter.checkin.claim')
))

const loadCheckIn = async () => {
  errorMessage.value = ''
  try {
    const response = await checkInAPI.today()
    status.value = response.data.data || null
  } catch (err: any) {
    errorMessage.value = err?.message || t('personalCenter.checkin.errors.loadFailed')
  }
}

const claimCheckIn = async () => {
  if (claiming.value) return
  claiming.value = true
  errorMessage.value = ''
  try {
    const response = await checkInAPI.claim()
    status.value = response.data.data || status.value
  } catch (err: any) {
    errorMessage.value = err?.message || t('personalCenter.checkin.errors.claimFailed')
  } finally {
    claiming.value = false
  }
}

onMounted(() => {
  loadCheckIn()
})
</script>
