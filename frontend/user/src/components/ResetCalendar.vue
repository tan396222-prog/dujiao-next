<template>
  <article class="theme-float rounded-[22px] bg-card p-4 sm:p-5" :data-provider="group.provider">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <h3 class="text-base font-bold">{{ providerName }}</h3>
      <span class="text-xs text-muted-foreground">{{ statusText }}</span>
    </div>

    <div class="mt-3 flex items-center gap-2">
      <button type="button" class="grid h-10 w-10 shrink-0 place-items-center rounded-full bg-secondary text-sm" :aria-label="t('feed.previousMonth')" @click="move(-1)">‹</button>
      <p class="min-w-0 flex-1 text-center text-sm font-semibold">{{ monthTitle }}</p>
      <button type="button" class="grid h-10 w-10 shrink-0 place-items-center rounded-full bg-secondary text-sm" :aria-label="t('feed.nextMonth')" @click="move(1)">›</button>
      <button type="button" class="h-10 shrink-0 rounded-full bg-secondary px-3 text-xs font-medium" @click="goToday">{{ t('feed.today') }}</button>
    </div>

    <div class="mt-3 grid grid-cols-7 gap-1 text-center text-[11px] text-muted-foreground">
      <span v-for="(label, index) in weekdays" :key="`${label}-${index}`" class="py-1">{{ label }}</span>
    </div>
    <div class="grid grid-cols-7 gap-1" data-reset-calendar>
      <template v-for="cell in cells" :key="cell.key">
        <span v-if="!cell.date" class="h-11"></span>
        <button
          v-else
          type="button"
          class="grid h-11 place-items-center rounded-full text-sm"
          :data-date="cell.date"
          :data-kind="dayKind(facts.get(cell.date))"
          :class="dayClass(cell.date)"
          :aria-pressed="selected === cell.date"
          :aria-label="dayLabel(cell.date)"
          @click="select(cell.date)"
        >{{ Number(cell.date.slice(-2)) }}</button>
      </template>
    </div>

    <p v-if="facts.size === 0" class="mt-3 text-sm text-muted-foreground">{{ t('feed.noResetDate') }}</p>
    <p v-else-if="!monthHasMark" class="mt-3 text-sm text-muted-foreground">{{ t('feed.noDateThisMonth') }}</p>
    <p v-else class="mt-3 text-sm font-semibold" data-month-counts>
      {{ t('feed.monthCounts', { quota: counts.quota, cards: counts.card }) }}
    </p>

    <ul class="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
      <li class="inline-flex items-center gap-1.5"><span class="reset-swatch" data-kind="quota_reset"></span>{{ t('feed.quotaReset') }}</li>
      <li class="inline-flex items-center gap-1.5"><span class="reset-swatch" data-kind="reset_credit"></span>{{ t('feed.resetCard') }}</li>
    </ul>

    <div v-if="selectedLines.length" class="mt-4 rounded-2xl bg-secondary/60 p-4" data-reset-day-facts>
      <p class="text-sm font-semibold">{{ selected }}</p>
      <ul class="mt-2 flex flex-col gap-1">
        <li v-for="line in selectedLines" :key="`${line.kind}-${line.status}`" class="text-sm font-semibold">
          {{ t(line.kind === 'quota_reset' ? 'feed.quotaReset' : 'feed.resetCard') }}
          {{ t('feed.eventCount', { count: line.count }) }}
          · {{ t(`feed.resetStatus.${line.status}`) }}
        </li>
      </ul>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { dayKind, monthCounts, monthGrid, resetFacts, shanghaiToday, shiftMonth, type ResetLine } from '../utils/resetCalendar'

type ResetNotice = {
  kind?: string
  status?: string
  date?: string
}

type ResetGroup = {
  provider: string
  status?: string
  fetched_at?: string
  checked_at?: string
  events: ResetNotice[]
}

const props = defineProps<{
  group: ResetGroup
  providerName: string
  statusText: string
}>()

const { t, locale } = useI18n()
const today = shanghaiToday()
const year = ref(today.year)
const month = ref(today.month)
const selected = ref('')
const todayDate = computed(() => shanghaiToday().date)

const facts = computed(() => resetFacts(props.group.events || []))
const cells = computed(() => monthGrid(year.value, month.value))
const counts = computed(() => monthCounts(facts.value, year.value, month.value))
const monthHasMark = computed(() => cells.value.some((cell) => cell.date && facts.value.has(cell.date)))
const weekdays = computed(() => ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'].map((day) => t(`feed.weekday.${day}`)))
const monthTitle = computed(() => new Intl.DateTimeFormat(String(locale.value || 'zh-CN'), {
  timeZone: 'UTC',
  year: 'numeric',
  month: 'long',
}).format(new Date(Date.UTC(year.value, month.value - 1, 15))))
const selectedLines = computed(() => facts.value.get(selected.value) || [])

const move = (delta: number) => {
  const next = shiftMonth(year.value, month.value, delta)
  year.value = next.year
  month.value = next.month
  selected.value = ''
}

const goToday = () => {
  const current = shanghaiToday()
  year.value = current.year
  month.value = current.month
  selected.value = ''
}

const select = (date: string) => {
  selected.value = facts.value.has(date) && selected.value !== date ? date : ''
}

const dayClass = (date: string) => {
  const kind = dayKind(facts.value.get(date))
  if (kind) return 'reset-day font-semibold'
  if (date === todayDate.value) return 'bg-secondary font-semibold'
  return ''
}

const dayLabel = (date: string) => {
  const lines = facts.value.get(date)
  if (!lines?.length) return date
  const factsText = lines.map((line: ResetLine) => {
    const name = t(line.kind === 'quota_reset' ? 'feed.quotaReset' : 'feed.resetCard')
    return `${name} ${t('feed.eventCount', { count: line.count })} ${t(`feed.resetStatus.${line.status}`)}`
  }).join('，')
  return `${date}，${factsText}`
}
</script>

<style scoped>
.reset-day {
  box-shadow: inset 0 0 0 2px #d92d20;
  color: #d92d20;
}

.reset-day[data-kind='mixed'] {
  box-shadow: inset 0 0 0 2px #d92d20, inset 0 0 0 4px transparent, inset 0 0 0 6px #d92d20;
}

.reset-swatch {
  height: 0.7rem;
  width: 0.7rem;
  border-radius: 9999px;
  box-shadow: inset 0 0 0 2px #d92d20;
}

.reset-swatch[data-kind='reset_credit'] {
  box-shadow: inset 0 0 0 2px #d92d20, inset 0 0 0 3px transparent, inset 0 0 0 5px #d92d20;
}
</style>
