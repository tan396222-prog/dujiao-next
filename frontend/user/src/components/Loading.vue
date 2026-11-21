<template>
  <transition name="fade">
    <div
      v-if="loading"
      class="loader-overlay fixed inset-0 z-50 flex items-center justify-center"
      role="status"
      aria-live="polite"
      :aria-busy="!stalled"
    >
      <div class="flex flex-col items-center gap-5">
        <div class="knot-stage" :class="stalled && 'is-stalled'">
          <GptKnotMark class="knot-spin" />
        </div>
        <p class="sr-only">{{ t('common.loading') }}</p>
        <button
          v-if="stalled"
          type="button"
          class="rounded-full bg-white/80 px-4 py-2 text-sm font-semibold text-[#3e3458] shadow"
          @click="retry"
        >
          {{ t('common.loadingRetry') }}
        </button>
      </div>
    </div>
  </transition>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import GptKnotMark from './GptKnotMark.vue'

const props = defineProps<{
  loading: boolean
}>()

const { t } = useI18n()
const stalled = ref(false)
let timer = 0

const arm = (active: boolean) => {
  window.clearTimeout(timer)
  stalled.value = false
  if (!active) return
  timer = window.setTimeout(() => {
    stalled.value = true
  }, 12000)
}

watch(() => props.loading, arm, { immediate: true })
onBeforeUnmount(() => window.clearTimeout(timer))

const retry = () => {
  window.location.reload()
}
</script>

<style scoped>
.loader-overlay {
  background:
    radial-gradient(420px 340px at 22% 28%, rgba(164, 98, 255, 0.55), transparent 62%),
    radial-gradient(380px 300px at 72% 30%, rgba(126, 196, 255, 0.4), transparent 64%),
    rgba(247, 243, 255, 0.72);
  backdrop-filter: blur(18px);
}

.knot-stage {
  display: grid;
  width: 92px;
  height: 92px;
  place-items: center;
  border-radius: 28px;
  color: #5b3d92;
  background: rgba(255, 255, 255, 0.46);
  border: 1px solid rgba(255, 255, 255, 0.78);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.9),
    0 18px 40px -24px rgba(86, 48, 150, 0.7);
}

.knot-spin {
  width: 48px;
  height: 48px;
  transform-origin: center;
  animation: gpt-knot-spin 2.8s linear infinite;
}

.knot-stage.is-stalled .knot-spin {
  animation: none;
}

@keyframes gpt-knot-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (prefers-reduced-motion: reduce) {
  .knot-spin {
    animation: none;
  }
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.3s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
