<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { ButtonConfig, SimulationMessage, SimulationStatus } from '@/types/flow-preview'
import { ScrollArea } from '@/components/ui/scroll-area'
import { MessageSquare } from 'lucide-vue-next'
import PreviewMessage from './PreviewMessage.vue'
import PreviewButtonGroup from './PreviewButtonGroup.vue'
import PreviewListPicker from './PreviewListPicker.vue'
import PreviewCarousel from './PreviewCarousel.vue'
import PreviewInputBar from './PreviewInputBar.vue'

const props = defineProps<{
  name: string
  status: SimulationStatus
  statusLabel?: string
  messages: SimulationMessage[]
  waiting: boolean
  inputType: string | null
  flowCta?: string
  busy?: boolean
}>()

const emit = defineEmits<{
  select: [button: ButtonConfig]
  submit: [value: string]
  completeFlow: []
}>()

const chatScrollRef = ref<InstanceType<typeof ScrollArea> | null>(null)

watch(
  () => props.messages.length,
  async () => {
    await nextTick()
    const root = chatScrollRef.value?.$el as HTMLElement | undefined
    if (!root) return
    const scrollArea = root.querySelector('[data-reka-scroll-area-viewport]') ||
      root.querySelector('[data-radix-scroll-area-viewport]') ||
      root.querySelector('[style*="overflow"]')
    if (scrollArea) {
      scrollArea.scrollTop = scrollArea.scrollHeight
    }
  }
)

const lastButtonMessage = computed(() => {
  if (!props.waiting || props.inputType !== 'button') return null
  return [...props.messages].reverse().find(m =>
    m.type === 'bot'
    && m.interactive !== 'cta_url'
    && (Array.isArray(m.buttons) || Array.isArray(m.cards))
  ) || null
})

const showReplyButtons = computed(() => {
  const message = lastButtonMessage.value
  if (!message?.buttons) return false
  return message.interactive !== 'list'
    && message.interactive !== 'carousel'
    && message.interactive !== 'cta_url'
    && message.buttons.length > 0
    && message.buttons.length <= 3
})

const showCarousel = computed(() => lastButtonMessage.value?.interactive === 'carousel')

const showListPicker = computed(() => {
  const message = lastButtonMessage.value
  if (!message?.buttons) return false
  if (message.interactive === 'list') return true
  return message.buttons.length > 3
})

const inputDisabled = computed(() => props.busy || !props.waiting || props.inputType === 'whatsapp_flow' || props.inputType === 'location')
</script>

<template>
  <div class="flex-1 flex flex-col min-w-0 min-h-0">
    <div class="flex-1 flex items-center justify-center p-6 bg-gray-100 dark:bg-gray-900 min-h-0">
      <div
        id="preview-phone-frame"
        class="w-full max-w-[400px] h-full max-h-[760px] bg-black rounded-[40px] p-[10px] shadow-2xl flex flex-col"
      >
        <div class="flex-1 rounded-[32px] overflow-hidden flex flex-col bg-[#efeae2] dark:bg-[#0b141a]">
          <div class="bg-[#008069] dark:bg-[#202c33] text-white px-5 py-1 flex items-center justify-between text-[11px] font-medium flex-shrink-0">
            <span>9:41</span>
            <div class="flex items-center gap-1">
              <svg width="14" height="10" viewBox="0 0 18 12" fill="currentColor"><rect x="0" y="8" width="3" height="4" rx="0.5"/><rect x="5" y="5" width="3" height="7" rx="0.5"/><rect x="10" y="2" width="3" height="10" rx="0.5"/><rect x="15" y="0" width="3" height="12" rx="0.5" opacity="0.4"/></svg>
              <svg width="22" height="10" viewBox="0 0 28 12" fill="none"><rect x="0.5" y="0.5" width="24" height="11" rx="2" stroke="currentColor"/><rect x="25.5" y="3.5" width="2" height="5" rx="0.5" fill="currentColor"/><rect x="2" y="2" width="19" height="8" rx="1" fill="currentColor"/></svg>
            </div>
          </div>

          <div class="bg-[#008069] dark:bg-[#202c33] text-white px-3 py-2 flex items-center gap-3 flex-shrink-0">
            <div class="w-9 h-9 rounded-full bg-white/20 flex items-center justify-center flex-shrink-0">
              <MessageSquare class="h-4 w-4" />
            </div>
            <div class="flex-1 min-w-0">
              <p class="font-medium text-sm truncate">{{ name || 'Flow Preview' }}</p>
              <p class="text-[11px] text-white/80 truncate">
                <template v-if="status === 'idle'">tap Start to begin</template>
                <template v-else-if="statusLabel">{{ statusLabel }}</template>
                <template v-else>{{ status }}</template>
              </p>
            </div>
          </div>

          <ScrollArea ref="chatScrollRef" class="flex-1 p-4 whatsapp-bg">
            <div class="space-y-3">
              <div v-if="status === 'idle' && messages.length === 0" class="text-center py-12">
                <MessageSquare class="h-12 w-12 mx-auto text-gray-300 dark:text-gray-600 mb-4" />
                <p class="text-sm text-gray-500 dark:text-gray-400">
                  Start the preview to simulate the flow
                </p>
              </div>

              <PreviewMessage
                v-for="message in messages"
                :key="message.id"
                :message="message"
              />

              <div
                v-if="waiting && inputType === 'whatsapp_flow'"
                class="flex justify-start"
              >
                <div class="max-w-[85%]">
                  <button
                    class="px-4 py-2 bg-[#075e54] text-white text-sm rounded-lg hover:bg-[#064e46] transition-colors disabled:opacity-50"
                    :disabled="busy"
                    @click="emit('completeFlow')"
                  >
                    {{ flowCta || 'Open Form' }}
                  </button>
                  <p class="text-[10px] text-gray-500 mt-1 italic">Simulated: clicks complete the flow</p>
                </div>
              </div>

              <div
                v-if="lastButtonMessage && lastButtonMessage.buttons"
                class="flex justify-start"
              >
                <div class="max-w-[85%]">
                  <PreviewCarousel
                    v-if="showCarousel"
                    :cards="lastButtonMessage.cards || []"
                    :disabled="!waiting || busy"
                    @select="emit('select', $event)"
                  />
                  <PreviewButtonGroup
                    v-else-if="showReplyButtons"
                    :buttons="lastButtonMessage.buttons"
                    :disabled="!waiting || busy"
                    @select="emit('select', $event)"
                  />
                  <PreviewListPicker
                    v-else-if="showListPicker"
                    :buttons="lastButtonMessage.buttons"
                    :button-text="lastButtonMessage.listButton"
                    :disabled="!waiting || busy"
                    @select="emit('select', $event)"
                  />
                  <p v-else class="text-xs text-gray-500 px-1">No options</p>
                </div>
              </div>
            </div>
          </ScrollArea>

          <PreviewInputBar
            :input-type="inputType"
            :disabled="inputDisabled"
            @submit="emit('submit', $event)"
          />
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.whatsapp-bg {
  background-image: url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='140' height='140' viewBox='0 0 140 140'><g fill='none' stroke='%2300000010' stroke-width='1.3' stroke-linejoin='round'><path d='M18 24c0-3 2-5 5-5h18c3 0 5 2 5 5v12c0 3-2 5-5 5h-12l-6 6v-6c-3 0-5-2-5-5z'/><rect x='86' y='30' width='32' height='18' rx='1.5'/><path d='M86 30l16 11 16-11'/><path d='M30 92c0-3 2-5 5-5h14c3 0 5 2 5 5v9c0 3-2 5-5 5h-9l-5 5v-5c-3 0-5-2-5-5z'/><rect x='90' y='95' width='30' height='17' rx='1.5'/><path d='M90 95l15 10 15-10'/></g></svg>");
  background-size: 140px 140px;
  background-repeat: repeat;
}

:global(.dark) .whatsapp-bg {
  background-image: url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='140' height='140' viewBox='0 0 140 140'><g fill='none' stroke='%23ffffff0a' stroke-width='1.3' stroke-linejoin='round'><path d='M18 24c0-3 2-5 5-5h18c3 0 5 2 5 5v12c0 3-2 5-5 5h-12l-6 6v-6c-3 0-5-2-5-5z'/><rect x='86' y='30' width='32' height='18' rx='1.5'/><path d='M86 30l16 11 16-11'/><path d='M30 92c0-3 2-5 5-5h14c3 0 5 2 5 5v9c0 3-2 5-5 5h-9l-5 5v-5c-3 0-5-2-5-5z'/><rect x='90' y='95' width='30' height='17' rx='1.5'/><path d='M90 95l15 10 15-10'/></g></svg>");
}
</style>
