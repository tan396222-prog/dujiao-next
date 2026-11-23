<template>
  <div class="pointer-events-none fixed inset-0 z-[70] overflow-hidden" :data-checkin-celebration="mode" aria-hidden="true">
    <span
      v-for="(spark, index) in sparks"
      :key="index"
      class="checkin-spark"
      :style="{ '--x': spark.x, '--y': spark.y, backgroundColor: spark.color }"
    ></span>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'
import { browserVibrate } from '../utils/openingVibration'
import { CHECKIN_SPARKS, createCheckInBurst, storedVibrationPreference } from '../utils/checkInCelebration'

const props = defineProps<{ mode: 'fireworks' | 'static' }>()
const emit = defineEmits<{ done: [] }>()
const sparks = props.mode === 'fireworks' ? CHECKIN_SPARKS : []
const burst = createCheckInBurst({
  celebration: props.mode,
  vibrate: browserVibrate() ?? undefined,
  preference: storedVibrationPreference(),
  onDone: () => emit('done'),
})

const stop = () => burst.stop()
const onHide = () => {
  if (document.visibilityState === 'hidden') stop()
}

onMounted(() => {
  burst.start()
  document.addEventListener('visibilitychange', onHide)
  window.addEventListener('pagehide', stop)
})

onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onHide)
  window.removeEventListener('pagehide', stop)
  stop()
})
</script>

<style scoped>
.checkin-spark {
  position: absolute;
  left: 50%;
  top: 38%;
  width: 8px;
  height: 8px;
  border-radius: 9999px;
  opacity: 0.95;
  animation: checkin-burst 1.5s ease-out forwards;
}

@keyframes checkin-burst {
  to {
    transform: translate(var(--x), var(--y)) scale(0.25);
    opacity: 0;
  }
}

@media (prefers-reduced-motion: reduce) {
  .checkin-spark {
    animation: none;
    display: none;
  }
}
</style>
