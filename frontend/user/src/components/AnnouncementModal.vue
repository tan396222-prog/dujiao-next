<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import DOMPurify from 'dompurify'
import { AlertTriangle, CheckCircle2, Info, Megaphone, X } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { useLocalized } from '../composables/useProduct'
import { processHtmlForDisplay } from '../utils/content'
import { useAnnouncement, type HomeAnnouncement } from '../composables/useAnnouncement'

const props = defineProps<{
  announcement: HomeAnnouncement
  visible: boolean
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
}>()

const { t } = useI18n()
const { getLocalizedText } = useLocalized()
const { dismissForever, dismissToday, closeForSession } = useAnnouncement()

const title = computed(() => getLocalizedText(props.announcement.title))

const sanitizedContent = computed(() => {
  const raw = getLocalizedText(props.announcement.content)
  const withImages = processHtmlForDisplay(String(raw || ''))
  return DOMPurify.sanitize(withImages, {
    ALLOWED_TAGS: ['p', 'br', 'strong', 'em', 'u', 's', 'code', 'pre', 'blockquote', 'ul', 'ol', 'li', 'a', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'span', 'div', 'img', 'hr', 'table', 'thead', 'tbody', 'tr', 'td', 'th', 'colgroup', 'col'],
    ALLOWED_ATTR: ['href', 'target', 'rel', 'src', 'alt', 'title', 'style', 'colspan', 'rowspan', 'width'],
    ALLOW_DATA_ATTR: false,
    ALLOWED_URI_REGEXP: /^(?:https?:|mailto:|tel:|#|\/(?!\/))/i,
  })
})

const typeStyle = computed(() => {
  switch (props.announcement.type) {
    case 'warning':
      return {
        iconWrap: 'bg-warning-soft text-warning ring-warning/20',
        icon: AlertTriangle,
      }
    case 'info':
      return {
        iconWrap: 'bg-info-soft text-info ring-info/20',
        icon: Info,
      }
    case 'success':
      return {
        iconWrap: 'bg-success-soft text-success ring-success/20',
        icon: CheckCircle2,
      }
    default:
      return {
        iconWrap: 'bg-primary-soft text-primary ring-primary/20',
        icon: Megaphone,
      }
  }
})

const close = () => emit('update:visible', false)

const handleClose = () => {
  closeForSession(props.announcement.version)
  close()
}

const handleDismissToday = () => {
  dismissToday(props.announcement.version)
  close()
}

const handleDismissForever = () => {
  dismissForever(props.announcement.version)
  close()
}

const wechatID = 'TCY15216367909'
const copied = ref(false)
const dialogRef = ref<HTMLElement | null>(null)

const copyWechat = async () => {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(wechatID)
    }
    copied.value = true
  } catch {
    copied.value = false
  }
}

const onKeydown = (e: KeyboardEvent) => {
  if (e.key === 'Escape') {
    handleClose()
  }
}

watch(
  () => props.visible,
  async (isVisible) => {
    if (isVisible) {
      document.addEventListener('keydown', onKeydown)
      await nextTick()
      const firstButton = dialogRef.value?.querySelector('button') as HTMLElement | null
      firstButton?.focus()
    } else {
      document.removeEventListener('keydown', onKeydown)
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <Teleport to="body">
    <Transition
      enter-active-class="transition-opacity duration-300 ease-out"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition-opacity duration-200 ease-in"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div
        v-if="visible"
        class="fixed inset-0 z-[120] flex items-center justify-center p-4 sm:p-6"
        @click.self="handleClose"
      >
        <div class="fixed inset-0 bg-black/55 backdrop-blur-md" aria-hidden="true" />
        <Transition
          enter-active-class="transition duration-300 ease-[cubic-bezier(0.34,1.56,0.64,1)]"
          enter-from-class="opacity-0 scale-95 translate-y-3"
          enter-to-class="opacity-100 scale-100 translate-y-0"
          leave-active-class="transition duration-200 ease-in"
          leave-from-class="opacity-100 scale-100 translate-y-0"
          leave-to-class="opacity-0 scale-95 translate-y-2"
        >
          <div
            v-if="visible"
            ref="dialogRef"
            role="dialog"
            aria-modal="true"
            aria-labelledby="announcement-modal-title"
            class="announcement-sheet relative z-10 flex max-h-[calc(100dvh-2rem)] w-full max-w-lg flex-col overflow-hidden rounded-3xl"
            data-announcement-sheet
          >
            <!-- Header -->
            <div class="flex shrink-0 items-center gap-4 px-6 pb-5 pt-6">
              <div
                class="flex size-12 shrink-0 items-center justify-center rounded-2xl ring-1 ring-inset"
                :class="typeStyle.iconWrap"
              >
                <component :is="typeStyle.icon" class="size-6" :stroke-width="2" />
              </div>
              <h3
                id="announcement-modal-title"
                class="announcement-title line-clamp-2 min-w-0 flex-1 text-lg font-bold leading-snug"
              >
                {{ title }}
              </h3>
              <button
                type="button"
                class="announcement-icon-button flex size-8 shrink-0 items-center justify-center rounded-full"
                :aria-label="t('announcement.close')"
                @click="handleClose"
              >
                <X class="size-4" :stroke-width="2" />
              </button>
            </div>

            <!-- Body -->
            <div class="min-h-0 flex-1 overflow-y-auto px-6 pb-4">
              <div class="announcement-copy" v-html="sanitizedContent"></div>
              <section class="wechat-contact" data-wechat-contact>
                <p class="wechat-label">{{ t('announcement.wechatLabel') }}</p>
                <p class="wechat-id" data-wechat-id>{{ wechatID }}</p>
                <button type="button" class="wechat-copy" data-wechat-copy @click="copyWechat">
                  {{ copied ? t('announcement.wechatCopied') : t('announcement.wechatCopy') }}
                </button>
                <p class="wechat-hint">{{ t('announcement.wechatHint') }}</p>
                <img
                  class="wechat-qr"
                  src="/wechat-qr.jpg"
                  :alt="t('announcement.wechatQrAlt')"
                  width="240"
                />
              </section>
            </div>

            <!-- Footer -->
            <div class="flex shrink-0 flex-col gap-3 border-t px-6 pb-5 pt-4 sm:flex-row sm:items-center sm:justify-between">
              <div class="flex items-center justify-center gap-2 text-xs sm:justify-start">
                <Button
                  variant="link"
                  class="announcement-dismiss h-auto p-0 text-xs font-semibold"
                  @click="handleDismissToday"
                >
                  {{ t('announcement.dismissToday') }}
                </Button>
                <span class="announcement-dismiss select-none">·</span>
                <Button
                  variant="link"
                  class="announcement-dismiss h-auto p-0 text-xs font-semibold"
                  @click="handleDismissForever"
                >
                  {{ t('announcement.dismissForever') }}
                </Button>
              </div>
              <Button class="w-full rounded-xl sm:w-auto" @click="handleClose">
                {{ t('announcement.close') }}
              </Button>
            </div>
          </div>
        </Transition>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.announcement-sheet {
  background-color: #ffffff !important;
  color: #111111;
  opacity: 1;
  border: 1px solid #e6e6e6;
}

.announcement-title,
.announcement-icon-button,
.announcement-dismiss {
  color: #111111;
}

.announcement-sheet :deep(.announcement-copy),
.announcement-sheet :deep(.announcement-copy :where(p, li, span, div, h1, h2, h3, h4, h5, h6, blockquote, td, th, strong, em, a, code)) {
  color: #111111 !important;
  font-weight: 700;
  opacity: 1;
}

.wechat-contact {
  margin-top: 1.25rem;
  color: #111111;
}

.wechat-label,
.wechat-hint,
.wechat-copy,
.wechat-id {
  color: #111111;
  font-weight: 700;
}

.wechat-id {
  margin: 0.25rem 0;
  font-weight: 800;
  user-select: text;
  -webkit-user-select: text;
}

.wechat-copy {
  border: 0;
  background: transparent;
  padding: 0;
  text-decoration: underline;
}

.wechat-qr {
  display: block;
  width: min(240px, 72vw);
  height: auto;
  max-width: 100%;
  margin-top: 0.75rem;
  background: #ffffff;
  -webkit-touch-callout: default;
}
</style>
