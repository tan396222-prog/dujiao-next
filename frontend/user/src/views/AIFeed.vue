<template>
  <div class="min-h-screen bg-background pb-20 pt-24 text-foreground">
    <div class="container mx-auto flex max-w-5xl flex-col gap-5 px-4">
      <header>
        <h1 class="text-[28px] font-bold tracking-tight">{{ t('feed.title') }}</h1>
        <p class="mt-1 text-sm leading-6 text-muted-foreground">{{ t('feed.subtitle') }}</p>
      </header>

      <section class="theme-float rounded-[22px] bg-card p-5">
        <p class="text-sm leading-6 text-muted-foreground">{{ t('feed.disclaimer') }}</p>
        <p v-if="snapshot" class="mt-2 text-xs text-muted-foreground">{{ t('feed.coverage', { names: coverageNames }) }}</p>
      </section>

      <p v-if="loading" class="text-sm text-muted-foreground">{{ t('feed.loading') }}</p>
      <section v-else-if="loadError" class="theme-float rounded-[22px] bg-card p-5">
        <p class="text-sm">{{ t('feed.loadFailed') }}</p>
        <Button class="mt-3 h-10 rounded-full" variant="secondary" @click="load">{{ t('feed.retry') }}</Button>
      </section>

      <template v-else-if="snapshot">
        <section id="reset-calendars" class="flex flex-col gap-4">
          <h2 class="text-lg font-bold">{{ t('feed.resetsTitle') }}</h2>
          <p class="text-sm leading-6 text-muted-foreground">{{ t('feed.resetsHint') }}</p>
          <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <ResetCalendar
              v-for="group in resetGroups"
              :key="group.provider"
              :group="group"
              :provider-name="providerName(group.provider)"
              :status-text="statusText(group)"
            />
          </div>
        </section>

        <section class="theme-float rounded-[22px] bg-card p-5">
          <div class="flex items-start justify-between gap-3">
            <h2 class="text-lg font-bold">{{ t('feed.dailyTitle') }}</h2>
            <span class="text-xs text-muted-foreground">{{ statusText(snapshot.daily) }}</span>
          </div>
          <p v-if="snapshot.daily.title" class="mt-3 text-base font-semibold">{{ snapshot.daily.title }}</p>
          <p v-if="snapshot.daily.summary" class="mt-2 text-sm leading-6 text-muted-foreground">{{ snapshot.daily.summary }}</p>
          <p v-if="!snapshot.daily.title && !snapshot.daily.summary" class="mt-3 text-sm text-muted-foreground">{{ t('feed.noDaily') }}</p>
          <p v-if="snapshot.daily.updated_at" class="mt-3 text-xs text-muted-foreground">{{ t('feed.updatedAt', { time: beijing(snapshot.daily.updated_at) }) }}</p>
        </section>

        <section id="feed-news" class="theme-float rounded-[22px] bg-card p-5">
          <div class="flex items-start justify-between gap-3">
            <h2 class="text-lg font-bold">{{ t('feed.newsTitle') }}</h2>
            <span class="text-xs text-muted-foreground">{{ statusText(snapshot.news) }}</span>
          </div>
          <ul v-if="snapshot.news.items.length" class="mt-4 flex flex-col gap-3">
            <li v-for="(item, index) in snapshot.news.items" :key="`${item.title}-${index}`" class="rounded-2xl bg-secondary/60 p-4">
              <h3 class="text-sm font-semibold leading-6">{{ item.title }}</h3>
              <p v-if="item.summary" class="mt-1 text-sm leading-6 text-muted-foreground">{{ item.summary }}</p>
              <div class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                <span v-if="item.source">{{ item.source }}</span>
                <span v-if="item.updated_at">{{ beijing(item.updated_at) }}</span>
              </div>
            </li>
          </ul>
          <p v-else class="mt-3 text-sm text-muted-foreground">{{ t('feed.noNews') }}</p>
        </section>

        <section class="theme-float rounded-[22px] bg-card p-5">
          <div class="flex items-start justify-between gap-3">
            <h2 class="text-lg font-bold">{{ t('feed.hotTitle') }}</h2>
            <span class="text-xs text-muted-foreground">{{ statusText(snapshot.hot_topics) }}</span>
          </div>
          <ol v-if="snapshot.hot_topics.items.length" class="mt-4 flex flex-col gap-3">
            <li v-for="(item, index) in snapshot.hot_topics.items" :key="`${item.title}-${index}`" class="flex gap-3 rounded-2xl bg-secondary/60 p-4">
              <span class="w-6 shrink-0 text-sm font-bold">{{ index + 1 }}</span>
              <div class="min-w-0">
                <h3 class="text-sm font-semibold leading-6">{{ item.title }}</h3>
                <p v-if="item.source" class="mt-1 text-xs text-muted-foreground">{{ item.source }}</p>
              </div>
            </li>
          </ol>
          <p v-else class="mt-3 text-sm text-muted-foreground">{{ t('feed.noHot') }}</p>
        </section>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { aiFeedAPI } from '../api/aiFeed'
import { Button } from '@/components/ui/button'
import ResetCalendar from '../components/ResetCalendar.vue'

type FeedItem = {
  title: string
  summary?: string
  source?: string
  original_url?: string
  feed_url?: string
  updated_at?: string
}

type FeedStatus = {
  status?: string
  stale?: boolean
  fetched_at?: string
}

type Daily = FeedStatus & {
  title?: string
  summary?: string
  feed_url?: string
  updated_at?: string
}

type ResetEvent = {
  kind?: string
  status?: string
  date?: string
}

type ResetGroup = FeedStatus & {
  provider: string
  checked_at?: string
  events: ResetEvent[]
}

type FeedSnapshot = {
  enabled: boolean
  coverage?: string[]
  news: FeedStatus & { items: FeedItem[] }
  hot_topics: FeedStatus & { items: FeedItem[] }
  daily: Daily
  resets: ResetGroup[]
}

const { t } = useI18n()
const loading = ref(true)
const loadError = ref(false)
const snapshot = ref<FeedSnapshot | null>(null)

const providerOrder = ['codex', 'antigravity', 'claude', 'grok_bot']

const coverageNames = computed(() => (snapshot.value?.coverage || []).map(providerName).join(' / '))

const resetGroups = computed(() => {
  const groups = new Map((snapshot.value?.resets || []).map((group) => [group.provider, group]))
  return providerOrder.map((provider) => {
    const group = groups.get(provider)
    return {
      provider,
      status: group?.status,
      fetched_at: group?.fetched_at,
      checked_at: group?.checked_at,
      events: group?.events || [],
    }
  })
})

const providerName = (provider: string) => {
  const key = `feed.providers.${provider}`
  const translated = t(key)
  return translated === key ? provider : translated
}

const beijing = (value?: string) => {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).format(date)
}

const statusText = (section?: FeedStatus) => {
  if (!section?.status) return ''
  const status = t(`feed.status.${section.status}`)
  if (!section.fetched_at) return status
  return t('feed.statusWithTime', { status, time: beijing(section.fetched_at) })
}

const load = async () => {
  loading.value = true
  loadError.value = false
  try {
    const response = await aiFeedAPI.get()
    const data = response.data?.data
    snapshot.value = {
      enabled: data?.enabled !== false,
      coverage: Array.isArray(data?.coverage) ? data.coverage : [],
      news: { status: data?.news?.status, stale: !!data?.news?.stale, fetched_at: data?.news?.fetched_at, items: Array.isArray(data?.news?.items) ? data.news.items : [] },
      hot_topics: { status: data?.hot_topics?.status, stale: !!data?.hot_topics?.stale, fetched_at: data?.hot_topics?.fetched_at, items: Array.isArray(data?.hot_topics?.items) ? data.hot_topics.items : [] },
      daily: data?.daily || { status: 'empty' },
      resets: Array.isArray(data?.resets) ? data.resets.map((group: ResetGroup) => ({ ...group, events: Array.isArray(group.events) ? group.events : [] })) : [],
    }
  } catch {
    loadError.value = true
    snapshot.value = null
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>
