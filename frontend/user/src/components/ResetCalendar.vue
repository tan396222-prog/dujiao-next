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
        <span v-if="!cell.date" class="h-10"></span>
        <span
          v-else
          class="grid h-10 place-items-center rounded-xl text-sm"
          :data-date="cell.date"
          :data-mark="marks.get(cell.date) || ''"
          :class="dayClass(cell.date)"
          :aria-label="dayLabel(cell.date)"
        >{{ Number(cell.date.slice(-2)) }}</span>
      </template>
    </div>

    <p v-if="marks.size === 0" class="mt-3 text-sm text-muted-foreground">{{ t('feed.noResetDate') }}</p>
    <p v-else-if="!monthHasMark" class="mt-3 text-sm text-muted-foreground">{{ t('feed.noDateThisMonth') }}</p>
    <ul class="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
      <li class="inline-flex items-center gap-1.5"><span class="h-2.5 w-2.5 rounded-full bg-foreground"></span>{{ t('feed.confirmedMark') }}</li>
      <li class="inline-flex items-center gap-1.5"><span class="h-2.5 w-2.5 rounded-full border border-dashed border-foreground"></span>{{ t('feed.estimatedMark') }}</li>
    </ul>
    <p class="mt-1 text-xs text-muted-foreground">{{ t('feed.estimatedNote') }}</p>

    <ul v-if="group.events.length" class="mt-4 flex flex-col gap-3">
      <li v-for="(event, index) in group.events" :key="`${event.title}-${index}`" class="rounded-2xl bg-secondary/60 p-4">
        <div class="flex flex-wrap items-center gap-2">
          <h4 class="text-sm font-semibold">{{ event.title }}</h4>
          <span v-if="event.estimated" class="rounded-full bg-foreground px-2 py-0.5 text-[11px] font-medium text-background">{{ t('feed.estimated') }}</span>
        </div>
        <p v-if="event.label" class="mt-1 text-xs text-muted-foreground">{{ event.label }}</p>
        <p v-if="event.scope_label" class="mt-1 text-xs text-muted-foreground">{{ event.scope_label }}</p>
        <p v-if="event.summary" class="mt-2 text-sm leading-6">{{ event.summary }}</p>
        <p v-if="event.original" class="mt-1 text-sm leading-6 text-muted-foreground">{{ event.original }}</p>
        <p v-if="event.schedule" class="mt-2 text-xs text-muted-foreground">{{ t('feed.schedule', { text: event.schedule }) }}</p>
        <p v-if="event.estimate" class="mt-1 text-xs text-muted-foreground">{{ t('feed.estimate', { text: event.estimate }) }}</p>
        <div class="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <span v-if="event.announced_at">{{ t('feed.announcedAt', { time: beijing(event.announced_at) }) }}</span>
          <span v-if="event.confirmed_at">{{ t('feed.confirmedAt', { time: beijing(event.confirmed_at) }) }}</span>
        </div>
      </li>
    </ul>
    <p v-else class="mt-3 text-sm text-muted-foreground">{{ t('feed.noReset') }}</p>
    <p v-if="group.checked_at" class="mt-3 text-xs text-muted-foreground">{{ t('feed.checkedAt', { time: beijing(group.checked_at) }) }}</p>
  </article>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { monthGrid, resetMarks, shanghaiToday, shiftMonth } from '../utils/resetCalendar'

type ResetNotice = {
  title: string
  label?: string
  scope_label?: string
  summary?: string
  original?: string
  announced_at?: string
  confirmed_at?: string
  occurred_on?: string
  schedule?: string
  schedule_dates?: string[]
  estimate?: string
  estimated?: boolean
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
  beijing: (value?: string) => string
}>()

const { t, locale } = useI18n()
const today = shanghaiToday()
const year = ref(today.year)
const month = ref(today.month)
const todayDate = computed(() => shanghaiToday().date)

const marks = computed(() => resetMarks(props.group.events || []))
const cells = computed(() => monthGrid(year.value, month.value))
const monthHasMark = computed(() => cells.value.some((cell) => cell.date && marks.value.has(cell.date)))
const weekdays = computed(() => ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'].map((day) => t(`feed.weekday.${day}`)))
const monthTitle = computed(() => new Intl.DateTimeFormat(String(locale.value || 'zh-CN'), {
  timeZone: 'UTC',
  year: 'numeric',
  month: 'long',
}).format(new Date(Date.UTC(year.value, month.value - 1, 15))))

const move = (delta: number) => {
  const next = shiftMonth(year.value, month.value, delta)
  year.value = next.year
  month.value = next.month
}

const goToday = () => {
  const current = shanghaiToday()
  year.value = current.year
  month.value = current.month
}

const dayClass = (date: string) => {
  const mark = marks.value.get(date)
  if (mark === 'confirmed') return 'bg-foreground font-semibold text-background'
  if (mark === 'estimated') return 'border border-dashed border-foreground font-medium'
  if (date === todayDate.value) return 'bg-secondary font-semibold'
  return ''
}

const dayLabel = (date: string) => {
  const mark = marks.value.get(date)
  if (mark === 'confirmed') return t('feed.dayConfirmed', { date })
  if (mark === 'estimated') return t('feed.dayEstimated', { date })
  return date
}
</script>
