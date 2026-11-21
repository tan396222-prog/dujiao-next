<template>
  <div>
    <!-- 原图按自身比例铺满，不拉伸；无图时用首字母占位 -->
    <div
      class="product-stage relative overflow-hidden rounded-[22px] md:rounded-[28px]"
      :class="shownImage ? 'bg-muted' : 'flex aspect-square items-center justify-center bg-[hsl(var(--h)_62%_94%)] dark:bg-[hsl(var(--h)_32%_17%)]'"
      :style="{ '--h': hue }"
      @touchstart.passive="onTouchStart"
      @touchend.passive="onTouchEnd"
    >
      <img
        v-if="shownImage"
        :src="currentImage"
        :alt="productTitle"
        class="block h-auto max-h-[78vh] w-full object-contain"
        @error="failedImage = currentImage"
      />
      <span v-else class="select-none text-8xl font-black text-[hsl(var(--h)_55%_38%)] md:text-[160px] dark:text-[hsl(var(--h)_60%_76%)]" aria-hidden="true">
        {{ Array.from(productTitle.trim())[0] }}
      </span>
    </div>

    <div v-if="images.length > 1" class="mt-3 flex gap-2 overflow-x-auto p-1 scrollbar-hide">
      <button
        v-for="(image, index) in images"
        :key="index"
        type="button"
        class="h-16 w-16 shrink-0 overflow-hidden rounded-xl bg-muted transition-opacity focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
        :class="currentImage === image ? 'ring-2 ring-foreground ring-offset-2 ring-offset-background' : 'opacity-60 hover:opacity-100'"
        :aria-label="`${productTitle} ${index + 1}`"
        :aria-pressed="currentImage === image"
        @click="emit('update:currentImage', image)"
      >
        <img :src="image" alt="" loading="lazy" class="h-full w-full object-cover" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { placeholderHue } from '../../utils/image'

const props = defineProps<{
  images: string[]
  currentImage: string
  productTitle: string
  productId?: number | string
}>()

const emit = defineEmits<{
  'update:currentImage': [value: string]
}>()

const hue = computed(() => placeholderHue(props.productId))

// 图片加载失败时退回首字母占位，与商品卡一致
const failedImage = ref('')
const shownImage = computed(() => (props.currentImage !== failedImage.value ? props.currentImage : ''))

// 左右滑动切图
let touchStartX = 0
const onTouchStart = (e: TouchEvent) => {
  touchStartX = e.touches[0]?.clientX ?? 0
}
const onTouchEnd = (e: TouchEvent) => {
  const diff = touchStartX - (e.changedTouches[0]?.clientX ?? 0)
  const count = props.images.length
  const index = props.images.indexOf(props.currentImage)
  if (Math.abs(diff) < 50 || count <= 1 || index === -1) return
  emit('update:currentImage', props.images[(index + (diff > 0 ? 1 : -1) + count) % count] ?? '')
}
</script>

