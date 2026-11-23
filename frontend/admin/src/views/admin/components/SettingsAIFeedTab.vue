<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import { notifyError, notifySuccess } from '@/utils/notify'

const { t } = useI18n()
const loading = ref(false)
const submitting = ref(false)
const enabled = ref(true)

const loadConfig = async () => {
  loading.value = true
  try {
    const res = await adminAPI.getSettings({ key: 'ai_feed_config' })
    const data = res.data?.data as { enabled?: boolean } | undefined
    enabled.value = data?.enabled !== false
  } catch {
    enabled.value = true
  } finally {
    loading.value = false
  }
}

const save = async () => {
  submitting.value = true
  try {
    await adminAPI.updateSettings({ key: 'ai_feed_config', value: { enabled: enabled.value } })
    notifySuccess(t('admin.settings.alerts.saveSuccess'))
  } catch (err) {
    const known = err as Error & { __notified?: boolean }
    if (!known?.__notified) notifyError(known?.message || t('admin.settings.alerts.saveFailed'))
  } finally {
    submitting.value = false
  }
}

onMounted(loadConfig)
defineExpose({ save, submitting })
</script>

<template>
  <div class="rounded-xl border border-border bg-card">
    <div class="border-b border-border bg-muted/40 px-6 py-4">
      <h2 class="text-lg font-semibold">{{ t('admin.settings.aiFeed.title') }}</h2>
      <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.aiFeed.subtitle') }}</p>
    </div>
    <div class="space-y-4 p-6">
      <div class="flex items-center justify-between gap-4">
        <Label>{{ t('admin.settings.aiFeed.enabled') }}</Label>
        <Switch v-model="enabled" :disabled="loading" />
      </div>
      <Button type="button" :disabled="submitting" @click="save">{{ t('admin.settings.aiFeed.save') }}</Button>
    </div>
  </div>
</template>
