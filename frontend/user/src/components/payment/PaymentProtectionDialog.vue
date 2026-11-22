<template>
  <div v-if="open" class="fixed inset-0 z-[80] flex items-end justify-center bg-black/45 p-3 sm:items-center" role="dialog" aria-modal="true">
    <div class="w-full max-w-md rounded-2xl border border-white/30 bg-background/95 p-5 shadow-2xl backdrop-blur">
      <h2 class="text-base font-semibold">{{ t('paymentProtection.title') }}</h2>
      <p class="mt-1 text-sm text-muted-foreground">{{ t('paymentProtection.waiting') }}</p>
      <p class="mt-3 text-sm">{{ t('paymentProtection.step', { current: step, total: total }) }}</p>
      <ol class="mt-4 space-y-2 text-sm">
        <li v-for="item in total" :key="item" :class="item === step ? 'font-medium' : 'text-muted-foreground'">
          {{ t(`paymentProtection.steps.${item}`) }}
          <span v-if="item < step"> · {{ t('paymentProtection.done') }}</span>
          <span v-else-if="item === step && error"> · {{ error }}</span>
        </li>
      </ol>
      <div class="mt-5 flex justify-end gap-2">
        <button type="button" class="rounded-full border px-4 py-2 text-sm" @click="$emit('cancel')">{{ t('paymentProtection.cancel') }}</button>
        <button v-if="error" type="button" class="rounded-full bg-foreground px-4 py-2 text-sm text-background" @click="$emit('retry')">{{ t('paymentProtection.retry') }}</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ open: boolean; step: number; total: number; error: string }>()
defineEmits<{ cancel: []; retry: [] }>()
const { t } = useI18n()
</script>
