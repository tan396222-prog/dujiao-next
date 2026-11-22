<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Gauge, Network, ShieldCheck, UserRound } from 'lucide-vue-next'
import { adminAPI } from '@/api/admin'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { notifyError, notifySuccess } from '@/utils/notify'

const { t } = useI18n()
const loading = ref(false)
const submitting = ref(false)
const attemptsLoading = ref(false)
const unbanSubmitting = ref(false)
const unbanIP = ref('')
const clientIdentity = ref<Record<string, string>>({})
const activeAttempts = ref<Array<Record<string, unknown>>>([])
const attemptEvents = ref<Array<Record<string, unknown>>>([])

const defaultRateLimit = (guest: boolean) => ({
  enabled: guest,
  window_seconds: 60,
  max_requests: guest ? 3 : 10,
  block_seconds: 120,
})

const defaultAttemptLimit = () => ({
  enabled: true,
  window_seconds: 60,
  max_requests: 20,
  block_seconds: 86400,
})

const form = reactive({
  enabled: false,
  common: {
    ip_blacklist_text: '',
    attempt_limit: defaultAttemptLimit(),
  },
  guest: {
    enabled: true,
    max_pending_orders_per_ip: 2,
    max_quantity_per_product_per_order: 1,
    max_pending_quantity_per_ip_product: 2,
    payment_expire_minutes: 10,
    rate_limit: defaultRateLimit(true),
  },
  member: {
    enabled: true,
    max_pending_orders_per_user: 5,
    max_pending_orders_per_ip: 0,
    max_quantity_per_product_per_order: 0,
    rate_limit: defaultRateLimit(false),
  },
})

const numberValue = (value: unknown, fallback: number) => {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

const applyRateLimit = (target: typeof form.guest.rate_limit, raw: unknown, guest: boolean) => {
  const data = raw as Record<string, unknown> | undefined
  const defaults = defaultRateLimit(guest)
  target.enabled = data?.enabled != null ? !!data.enabled : defaults.enabled
  target.window_seconds = numberValue(data?.window_seconds, defaults.window_seconds)
  target.max_requests = numberValue(data?.max_requests, defaults.max_requests)
  target.block_seconds = numberValue(data?.block_seconds, defaults.block_seconds)
}

const applyAttemptLimit = (raw: unknown) => {
  const data = raw as Record<string, unknown> | undefined
  const defaults = defaultAttemptLimit()
  form.common.attempt_limit.enabled = data?.enabled != null ? !!data.enabled : defaults.enabled
  form.common.attempt_limit.window_seconds = numberValue(data?.window_seconds, defaults.window_seconds)
  form.common.attempt_limit.max_requests = numberValue(data?.max_requests, defaults.max_requests)
  form.common.attempt_limit.block_seconds = numberValue(data?.block_seconds, defaults.block_seconds)
}

const applyLegacyRateLimit = (target: typeof form.guest.rate_limit, raw: unknown) => {
  const data = raw as Record<string, unknown> | undefined
  target.enabled = data?.enabled != null ? !!data.enabled : false
  target.window_seconds = numberValue(data?.window_seconds, 60)
  target.max_requests = numberValue(data?.max_requests, 5)
  target.block_seconds = numberValue(data?.block_seconds, 120)
}

const applyRecommendedGuestPolicy = () => {
  form.guest.enabled = true
  form.guest.max_pending_orders_per_ip = 2
  form.guest.max_quantity_per_product_per_order = 1
  form.guest.max_pending_quantity_per_ip_product = 2
  form.guest.payment_expire_minutes = 10
  Object.assign(form.guest.rate_limit, defaultRateLimit(true))
}

const loadNestedConfig = (data: Record<string, unknown>) => {
  const common = data.common as Record<string, unknown> | undefined
  const guest = data.guest as Record<string, unknown> | undefined
  const member = data.member as Record<string, unknown> | undefined
  form.enabled = !!data.enabled
  form.common.ip_blacklist_text = ((common?.ip_blacklist as string[] | undefined) || []).join('\n')
  applyAttemptLimit(common?.attempt_limit)

  form.guest.enabled = guest?.enabled != null ? !!guest.enabled : true
  form.guest.max_pending_orders_per_ip = numberValue(guest?.max_pending_orders_per_ip, 2)
  form.guest.max_quantity_per_product_per_order = numberValue(guest?.max_quantity_per_product_per_order, 1)
  form.guest.max_pending_quantity_per_ip_product = numberValue(guest?.max_pending_quantity_per_ip_product, 2)
  form.guest.payment_expire_minutes = numberValue(guest?.payment_expire_minutes, 10)
  applyRateLimit(form.guest.rate_limit, guest?.rate_limit, true)

  form.member.enabled = member?.enabled != null ? !!member.enabled : true
  form.member.max_pending_orders_per_user = numberValue(member?.max_pending_orders_per_user, 5)
  form.member.max_pending_orders_per_ip = numberValue(member?.max_pending_orders_per_ip, 0)
  form.member.max_quantity_per_product_per_order = numberValue(member?.max_quantity_per_product_per_order, 0)
  applyRateLimit(form.member.rate_limit, member?.rate_limit, false)
}

const loadFlatConfig = (data: Record<string, unknown>) => {
  form.enabled = !!data.enabled
  form.common.ip_blacklist_text = ((data.ip_blacklist as string[] | undefined) || []).join('\n')
  form.guest.enabled = true
  form.guest.max_pending_orders_per_ip = numberValue(data.max_pending_orders_per_ip, 5)
  form.guest.max_quantity_per_product_per_order = 0
  form.guest.max_pending_quantity_per_ip_product = 0
  form.guest.payment_expire_minutes = 0
  applyLegacyRateLimit(form.guest.rate_limit, data.order_rate_limit)
  form.member.enabled = true
  form.member.max_pending_orders_per_user = numberValue(data.max_pending_orders_per_user, 3)
  form.member.max_pending_orders_per_ip = numberValue(data.max_pending_orders_per_ip, 5)
  form.member.max_quantity_per_product_per_order = 0
  applyLegacyRateLimit(form.member.rate_limit, data.order_rate_limit)
}

const loadConfig = async () => {
  loading.value = true
  try {
    const res = await adminAPI.getSettings({ key: 'order_risk_control_config' })
    const data = res.data?.data as Record<string, unknown> | undefined
    if (data && Object.keys(data).length > 0) {
      if (data.guest && data.member) loadNestedConfig(data)
      else loadFlatConfig(data)
    }
  } catch {
    // 读取失败时保留安全的推荐表单值，保存仍由用户主动触发。
  } finally {
    loading.value = false
  }
}

const normalizeLines = (value: string) => Array.from(new Set(value
  .split('\n')
  .map((line: string) => line.trim())
  .filter((line: string) => line.length > 0)))

const save = async () => {
  submitting.value = true
  try {
    await adminAPI.updateSettings({
      key: 'order_risk_control_config',
      value: {
        version: 2,
        enabled: form.enabled,
        common: {
          ip_blacklist: normalizeLines(form.common.ip_blacklist_text),
          attempt_limit: {
            enabled: form.common.attempt_limit.enabled,
            window_seconds: Number(form.common.attempt_limit.window_seconds),
            max_requests: Number(form.common.attempt_limit.max_requests),
            block_seconds: Number(form.common.attempt_limit.block_seconds),
          },
        },
        guest: {
          enabled: form.guest.enabled,
          max_pending_orders_per_ip: Number(form.guest.max_pending_orders_per_ip),
          max_quantity_per_product_per_order: Number(form.guest.max_quantity_per_product_per_order),
          max_pending_quantity_per_ip_product: Number(form.guest.max_pending_quantity_per_ip_product),
          payment_expire_minutes: Number(form.guest.payment_expire_minutes),
          rate_limit: {
            enabled: form.guest.rate_limit.enabled,
            window_seconds: Number(form.guest.rate_limit.window_seconds),
            max_requests: Number(form.guest.rate_limit.max_requests),
            block_seconds: Number(form.guest.rate_limit.block_seconds),
          },
        },
        member: {
          enabled: form.member.enabled,
          max_pending_orders_per_user: Number(form.member.max_pending_orders_per_user),
          max_pending_orders_per_ip: Number(form.member.max_pending_orders_per_ip),
          max_quantity_per_product_per_order: Number(form.member.max_quantity_per_product_per_order),
          rate_limit: {
            enabled: form.member.rate_limit.enabled,
            window_seconds: Number(form.member.rate_limit.window_seconds),
            max_requests: Number(form.member.rate_limit.max_requests),
            block_seconds: Number(form.member.rate_limit.block_seconds),
          },
        },
      },
    })
    notifySuccess(t('admin.settings.alerts.saveSuccess'))
  } catch (err) {
    const known = err as Error & { __notified?: boolean }
    if (!known?.__notified) notifyError(known?.message || t('admin.settings.alerts.saveFailed'))
  } finally {
    submitting.value = false
  }
}

const loadAttempts = async () => {
  attemptsLoading.value = true
  try {
    const [attemptsRes, identityRes] = await Promise.all([
      adminAPI.getOrderRiskAttempts(),
      adminAPI.getOrderRiskClientIP(),
    ])
    const report = attemptsRes.data?.data as Record<string, unknown> | undefined
    activeAttempts.value = (report?.active as Array<Record<string, unknown>> | undefined) || []
    attemptEvents.value = (report?.events as Array<Record<string, unknown>> | undefined) || []
    clientIdentity.value = (identityRes.data?.data as Record<string, string> | undefined) || {}
  } catch (err) {
    const known = err as Error & { __notified?: boolean }
    if (!known?.__notified) notifyError(known?.message || t('admin.settings.alerts.saveFailed'))
  } finally {
    attemptsLoading.value = false
  }
}

const unbanAttempt = async () => {
  const ip = unbanIP.value.trim()
  if (!ip) return
  unbanSubmitting.value = true
  try {
    await adminAPI.unbanOrderRiskAttempt(ip)
    unbanIP.value = ''
    notifySuccess(t('admin.settings.alerts.saveSuccess'))
    await loadAttempts()
  } catch (err) {
    const known = err as Error & { __notified?: boolean }
    if (!known?.__notified) notifyError(known?.message || t('admin.settings.alerts.saveFailed'))
  } finally {
    unbanSubmitting.value = false
  }
}

const sourceLabel = (source: string) => {
  if (source === 'forwarded') return t('admin.settings.orderRiskControl.clientIdentity.forwarded')
  if (source === 'remote') return t('admin.settings.orderRiskControl.clientIdentity.remote')
  return t('admin.settings.orderRiskControl.clientIdentity.unavailable')
}

const formatLimit = (value: number) => value > 0 ? String(value) : t('admin.settings.orderRiskControl.noLimit')
const guestSummary = computed(() => t('admin.settings.orderRiskControl.guest.summary', {
  orders: formatLimit(Number(form.guest.max_pending_orders_per_ip)),
  quantity: formatLimit(Number(form.guest.max_quantity_per_product_per_order)),
  pending: formatLimit(Number(form.guest.max_pending_quantity_per_ip_product)),
}))

defineExpose({ save })
onMounted(() => {
  loadConfig()
  loadAttempts()
})
</script>

<template>
  <div class="space-y-6" :class="{ 'pointer-events-none opacity-60': loading }">
    <section class="overflow-hidden rounded-xl border bg-card">
      <div class="flex flex-col gap-5 border-b bg-muted/30 p-5 sm:flex-row sm:items-center sm:justify-between">
        <div class="flex items-start gap-3">
          <div class="rounded-lg border bg-background p-2 text-foreground shadow-sm">
            <ShieldCheck class="h-5 w-5" />
          </div>
          <div>
            <h2 class="text-base font-semibold">{{ t('admin.settings.orderRiskControl.master.title') }}</h2>
            <p class="mt-1 max-w-2xl text-xs leading-5 text-muted-foreground">{{ t('admin.settings.orderRiskControl.master.subtitle') }}</p>
          </div>
        </div>
        <div class="flex items-center gap-3 rounded-full border bg-background px-4 py-2">
          <span class="text-xs font-medium" :class="form.enabled ? 'text-emerald-600' : 'text-muted-foreground'">
            {{ form.enabled ? t('admin.settings.orderRiskControl.statusEnabled') : t('admin.settings.orderRiskControl.statusDisabled') }}
          </span>
          <Switch v-model="form.enabled" />
        </div>
      </div>
    </section>

    <section class="rounded-xl border bg-card p-5">
      <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h3 class="text-base font-semibold">{{ t('admin.settings.orderRiskControl.attemptLimit.title') }}</h3>
          <p class="mt-1 max-w-3xl text-xs leading-5 text-muted-foreground">{{ t('admin.settings.orderRiskControl.attemptLimit.subtitle') }}</p>
          <p class="mt-2 max-w-3xl text-xs leading-5 text-muted-foreground">{{ t('admin.settings.orderRiskControl.attemptLimit.deviceHint') }}</p>
        </div>
        <div class="flex items-center gap-3">
          <span class="text-xs">{{ t('admin.settings.orderRiskControl.attemptLimit.enabled') }}</span>
          <Switch v-model="form.common.attempt_limit.enabled" />
        </div>
      </div>
      <div v-show="form.common.attempt_limit.enabled" class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-3">
        <div class="space-y-2"><Label>{{ t('admin.settings.orderRiskControl.rateLimit.windowSeconds') }}</Label><Input v-model.number="form.common.attempt_limit.window_seconds" type="number" min="10" max="3600" /></div>
        <div class="space-y-2"><Label>{{ t('admin.settings.orderRiskControl.rateLimit.maxRequests') }}</Label><Input v-model.number="form.common.attempt_limit.max_requests" type="number" min="1" max="100" /></div>
        <div class="space-y-2"><Label>{{ t('admin.settings.orderRiskControl.rateLimit.blockSeconds') }}</Label><Input v-model.number="form.common.attempt_limit.block_seconds" type="number" min="0" max="86400" /></div>
      </div>

      <div class="mt-6 rounded-lg border bg-muted/20 p-4">
        <h4 class="text-sm font-semibold">{{ t('admin.settings.orderRiskControl.clientIdentity.title') }}</h4>
        <p class="mt-1 text-xs leading-5 text-muted-foreground">{{ t('admin.settings.orderRiskControl.clientIdentity.subtitle') }}</p>
        <dl class="mt-3 grid grid-cols-1 gap-2 text-xs sm:grid-cols-2">
          <div><dt class="text-muted-foreground">{{ t('admin.settings.orderRiskControl.clientIdentity.ip') }}</dt><dd class="font-mono">{{ clientIdentity.ip || '-' }}</dd></div>
          <div><dt class="text-muted-foreground">{{ t('admin.settings.orderRiskControl.clientIdentity.riskIp') }}</dt><dd class="font-mono">{{ clientIdentity.risk_ip || '-' }}</dd></div>
          <div><dt class="text-muted-foreground">{{ t('admin.settings.orderRiskControl.clientIdentity.source') }}</dt><dd>{{ sourceLabel(clientIdentity.source || '') }}</dd></div>
          <div><dt class="text-muted-foreground">{{ t('admin.settings.orderRiskControl.clientIdentity.peer') }}</dt><dd class="font-mono">{{ clientIdentity.peer || '-' }}</dd></div>
        </dl>
      </div>

      <div class="mt-6">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <h4 class="text-sm font-semibold">{{ t('admin.settings.orderRiskControl.attempts.title') }}</h4>
            <p class="mt-1 text-xs leading-5 text-muted-foreground">{{ t('admin.settings.orderRiskControl.attempts.subtitle') }}</p>
          </div>
          <div class="flex gap-2">
            <Input v-model="unbanIP" :placeholder="t('admin.settings.orderRiskControl.attempts.unbanPlaceholder')" class="w-48 font-mono" />
            <Button type="button" variant="outline" :disabled="unbanSubmitting" @click="unbanAttempt">{{ t('admin.settings.orderRiskControl.attempts.unbanAction') }}</Button>
          </div>
        </div>
        <p v-if="!activeAttempts.length" class="mt-3 text-xs text-muted-foreground">{{ t('admin.settings.orderRiskControl.attempts.empty') }}</p>
        <div v-else class="mt-3 overflow-x-auto">
          <table class="w-full text-left text-xs">
            <thead class="text-muted-foreground">
              <tr>
                <th class="py-2 pr-3">{{ t('admin.settings.orderRiskControl.attempts.ip') }}</th>
                <th class="py-2 pr-3">{{ t('admin.settings.orderRiskControl.attempts.count') }}</th>
                <th class="py-2 pr-3">{{ t('admin.settings.orderRiskControl.attempts.status') }}</th>
                <th class="py-2 pr-3">{{ t('admin.settings.orderRiskControl.attempts.reason') }}</th>
                <th class="py-2 pr-3">{{ t('admin.settings.orderRiskControl.attempts.retry') }}</th>
                <th class="py-2 pr-3">{{ t('admin.settings.orderRiskControl.attempts.device') }}</th>
                <th class="py-2">{{ t('admin.settings.orderRiskControl.attempts.backend') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in activeAttempts" :key="String(item.ip)" class="border-t">
                <td class="py-2 pr-3 font-mono">{{ item.ip }}</td>
                <td class="py-2 pr-3">{{ item.count }}/{{ item.limit }}</td>
                <td class="py-2 pr-3">{{ item.banned ? t('admin.settings.orderRiskControl.attempts.banned') : t('admin.settings.orderRiskControl.attempts.watching') }}</td>
                <td class="py-2 pr-3">{{ item.reason }}</td>
                <td class="py-2 pr-3">{{ item.retry_after_seconds }}</td>
                <td class="py-2 pr-3 font-mono">{{ item.device || '-' }}</td>
                <td class="py-2">{{ item.backend }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <h4 class="mt-4 text-sm font-semibold">{{ t('admin.settings.orderRiskControl.attempts.events') }}</h4>
        <p v-if="!attemptEvents.length" class="mt-2 text-xs text-muted-foreground">{{ t('admin.settings.orderRiskControl.attempts.noEvents') }}</p>
        <ul v-else class="mt-2 space-y-1 text-xs">
          <li v-for="(event, index) in attemptEvents" :key="index" class="font-mono">
            {{ event.at }} {{ event.action }} {{ event.ip }} {{ event.reason }} {{ event.device || '' }}
          </li>
        </ul>
        <p v-if="attemptsLoading" class="mt-2 text-xs text-muted-foreground">...</p>
      </div>
    </section>

    <section v-show="form.enabled" class="overflow-hidden rounded-xl border border-amber-200/70 bg-card shadow-sm dark:border-amber-900/60">
      <div class="border-b border-amber-200/70 bg-amber-50/70 p-5 dark:border-amber-900/60 dark:bg-amber-950/20">
        <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div class="flex items-start gap-3">
            <div class="rounded-lg bg-amber-500 p-2 text-white shadow-sm"><Network class="h-5 w-5" /></div>
            <div>
              <div class="flex items-center gap-2">
                <h3 class="text-base font-semibold">{{ t('admin.settings.orderRiskControl.guest.title') }}</h3>
                <span class="rounded-full bg-amber-100 px-2 py-0.5 text-[11px] font-semibold text-amber-800 dark:bg-amber-900/50 dark:text-amber-200">
                  {{ t('admin.settings.orderRiskControl.guest.priority') }}
                </span>
              </div>
              <p class="mt-1 max-w-2xl text-xs leading-5 text-muted-foreground">{{ t('admin.settings.orderRiskControl.guest.subtitle') }}</p>
            </div>
          </div>
          <div class="flex items-center gap-3">
            <Button type="button" variant="outline" size="sm" @click="applyRecommendedGuestPolicy">
              {{ t('admin.settings.orderRiskControl.guest.applyRecommended') }}
            </Button>
            <Switch v-model="form.guest.enabled" />
          </div>
        </div>
      </div>

      <div v-show="form.guest.enabled" class="space-y-6 p-5">
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
          <div class="space-y-2">
            <Label>{{ t('admin.settings.orderRiskControl.guest.maxPendingPerIP') }}</Label>
            <Input v-model.number="form.guest.max_pending_orders_per_ip" type="number" min="0" max="100" />
            <p class="text-xs leading-5 text-muted-foreground">{{ t('admin.settings.orderRiskControl.guest.maxPendingPerIPHint') }}</p>
          </div>
          <div class="space-y-2">
            <Label>{{ t('admin.settings.orderRiskControl.guest.maxQuantityPerProduct') }}</Label>
            <Input v-model.number="form.guest.max_quantity_per_product_per_order" type="number" min="0" max="100000" />
            <p class="text-xs leading-5 text-muted-foreground">{{ t('admin.settings.orderRiskControl.guest.maxQuantityPerProductHint') }}</p>
          </div>
          <div class="space-y-2">
            <Label>{{ t('admin.settings.orderRiskControl.guest.maxPendingQuantityPerIPProduct') }}</Label>
            <Input v-model.number="form.guest.max_pending_quantity_per_ip_product" type="number" min="0" max="100000" />
            <p class="text-xs leading-5 text-muted-foreground">{{ t('admin.settings.orderRiskControl.guest.maxPendingQuantityPerIPProductHint') }}</p>
          </div>
          <div class="space-y-2">
            <Label>{{ t('admin.settings.orderRiskControl.guest.paymentExpireMinutes') }}</Label>
            <Input v-model.number="form.guest.payment_expire_minutes" type="number" min="0" max="10080" />
            <p class="text-xs leading-5 text-muted-foreground">{{ t('admin.settings.orderRiskControl.guest.paymentExpireMinutesHint') }}</p>
          </div>
        </div>

        <div class="rounded-lg border border-dashed bg-muted/25 px-4 py-3 text-xs leading-5 text-muted-foreground">
          {{ guestSummary }}
        </div>

        <div class="rounded-lg border p-4">
          <div class="mb-4 flex items-center justify-between gap-4">
            <div class="flex items-center gap-2">
              <Gauge class="h-4 w-4 text-amber-600" />
              <div>
                <Label>{{ t('admin.settings.orderRiskControl.guest.rateLimitTitle') }}</Label>
                <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.orderRiskControl.guest.rateLimitHint') }}</p>
              </div>
            </div>
            <Switch v-model="form.guest.rate_limit.enabled" />
          </div>
          <div v-show="form.guest.rate_limit.enabled" class="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <div class="space-y-2"><Label>{{ t('admin.settings.orderRiskControl.rateLimit.windowSeconds') }}</Label><Input v-model.number="form.guest.rate_limit.window_seconds" type="number" min="10" max="3600" /></div>
            <div class="space-y-2"><Label>{{ t('admin.settings.orderRiskControl.rateLimit.maxRequests') }}</Label><Input v-model.number="form.guest.rate_limit.max_requests" type="number" min="1" max="100" /></div>
            <div class="space-y-2"><Label>{{ t('admin.settings.orderRiskControl.rateLimit.blockSeconds') }}</Label><Input v-model.number="form.guest.rate_limit.block_seconds" type="number" min="0" max="86400" /></div>
          </div>
        </div>
      </div>
    </section>

    <section v-show="form.enabled" class="overflow-hidden rounded-xl border bg-card">
      <div class="border-b bg-sky-50/50 p-5 dark:bg-sky-950/10">
        <div class="flex items-start justify-between gap-4">
          <div class="flex items-start gap-3">
            <div class="rounded-lg bg-sky-600 p-2 text-white shadow-sm"><UserRound class="h-5 w-5" /></div>
            <div>
              <h3 class="text-base font-semibold">{{ t('admin.settings.orderRiskControl.member.title') }}</h3>
              <p class="mt-1 max-w-2xl text-xs leading-5 text-muted-foreground">{{ t('admin.settings.orderRiskControl.member.subtitle') }}</p>
            </div>
          </div>
          <Switch v-model="form.member.enabled" />
        </div>
      </div>
      <div v-show="form.member.enabled" class="space-y-6 p-5">
        <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
          <div class="space-y-2"><Label>{{ t('admin.settings.orderRiskControl.member.maxPendingPerUser') }}</Label><Input v-model.number="form.member.max_pending_orders_per_user" type="number" min="0" max="100" /><p class="text-xs leading-5 text-muted-foreground">{{ t('admin.settings.orderRiskControl.member.maxPendingPerUserHint') }}</p></div>
          <div class="space-y-2"><Label>{{ t('admin.settings.orderRiskControl.member.maxPendingPerIP') }}</Label><Input v-model.number="form.member.max_pending_orders_per_ip" type="number" min="0" max="100" /><p class="text-xs leading-5 text-muted-foreground">{{ t('admin.settings.orderRiskControl.member.maxPendingPerIPHint') }}</p></div>
          <div class="space-y-2"><Label>{{ t('admin.settings.orderRiskControl.member.maxQuantityPerProduct') }}</Label><Input v-model.number="form.member.max_quantity_per_product_per_order" type="number" min="0" max="100000" /><p class="text-xs leading-5 text-muted-foreground">{{ t('admin.settings.orderRiskControl.member.maxQuantityPerProductHint') }}</p></div>
        </div>
        <div class="rounded-lg border p-4">
          <div class="mb-4 flex items-center justify-between gap-4">
            <div><Label>{{ t('admin.settings.orderRiskControl.member.rateLimitTitle') }}</Label><p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.orderRiskControl.member.rateLimitHint') }}</p></div>
            <Switch v-model="form.member.rate_limit.enabled" />
          </div>
          <div v-show="form.member.rate_limit.enabled" class="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <div class="space-y-2"><Label>{{ t('admin.settings.orderRiskControl.rateLimit.windowSeconds') }}</Label><Input v-model.number="form.member.rate_limit.window_seconds" type="number" min="10" max="3600" /></div>
            <div class="space-y-2"><Label>{{ t('admin.settings.orderRiskControl.rateLimit.maxRequests') }}</Label><Input v-model.number="form.member.rate_limit.max_requests" type="number" min="1" max="100" /></div>
            <div class="space-y-2"><Label>{{ t('admin.settings.orderRiskControl.rateLimit.blockSeconds') }}</Label><Input v-model.number="form.member.rate_limit.block_seconds" type="number" min="0" max="86400" /></div>
          </div>
        </div>
      </div>
    </section>

    <section v-show="form.enabled" class="rounded-xl border bg-card p-5">
      <div class="mb-4">
        <h3 class="text-sm font-semibold">{{ t('admin.settings.orderRiskControl.ipBlacklist.title') }}</h3>
        <p class="mt-1 text-xs leading-5 text-muted-foreground">{{ t('admin.settings.orderRiskControl.ipBlacklist.subtitle') }}</p>
      </div>
      <Textarea v-model="form.common.ip_blacklist_text" :placeholder="t('admin.settings.orderRiskControl.ipBlacklist.placeholder')" rows="6" class="font-mono text-sm" />
    </section>
  </div>
</template>
