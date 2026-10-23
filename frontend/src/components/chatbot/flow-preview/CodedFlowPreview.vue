<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from 'vue-sonner'
import { Play, RotateCcw } from 'lucide-vue-next'
import {
  chatbotService,
  type CodedFlowBinding,
  type CodedPreviewAICall,
  type CodedPreviewAPICall,
  type CodedPreviewMessage,
  type CodedPreviewRequest,
  type CodedPreviewResponse,
} from '@/services/api'
import type { ButtonConfig, SimulationMessage, SimulationStatus } from '@/types/flow-preview'
import { getErrorMessage } from '@/lib/api-utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import PreviewPhone from './PreviewPhone.vue'
import type { PreviewMediaPayload } from './PreviewInputBar.vue'
import FlowDebugSections from './FlowDebugSections.vue'

const props = defineProps<{
  flow: CodedFlowBinding
  account: string
}>()

const { t } = useI18n()

const buyAsks = new Set(['collection', 'product', 'option', 'quantity', 'next', 'details'])

const detailFields = [
  { key: 'customer_name', label: 'Name' },
  { key: 'customer_phone', label: 'Phone' },
  { key: 'customer_email', label: 'Email' },
  { key: 'address_line_one', label: 'Address line 1' },
  { key: 'address_line_two', label: 'Address line 2' },
  { key: 'city', label: 'City' },
  { key: 'state', label: 'State' },
  { key: 'country', label: 'Country' },
  { key: 'pincode', label: 'Pincode' },
  { key: 'customer_notes', label: 'Notes' },
] as const

type DetailKey = (typeof detailFields)[number]['key']

const phone = ref('')
const mockResponses = ref(true)
const needsMock = ref(false)
const mockOperation = ref('')
const mockBody = ref('{\n  \n}')
const importMode = ref(false)
const importBody = ref('')
const sessionId = ref('')
const step = ref('')
const input = ref<CodedPreviewResponse['input']>('')
const flowCta = ref('')
const status = ref<SimulationStatus>('idle')
const busy = ref(false)
const messages = ref<SimulationMessage[]>([])
const sessionContext = ref<Record<string, unknown>>({})
const turnAICalls = ref<CodedPreviewAICall[]>([])
const previewAPICalls = ref<CodedPreviewAPICall[]>([])
const locationLat = ref('12.9716')
const locationLng = ref('77.5946')
const details = reactive<Record<DetailKey, string>>({
  customer_name: 'Preview Customer',
  customer_phone: '',
  customer_email: 'preview@example.com',
  address_line_one: '1 Preview Street',
  address_line_two: '',
  city: 'Bengaluru',
  state: 'KA',
  country: 'India',
  pincode: '560001',
  customer_notes: '',
})

const waiting = computed(() => !importMode.value && status.value === 'waiting_input')

type ImportedSessionLog = {
  version: number
  turns: Array<{
    at?: string
    inbound?: Record<string, unknown>
    messages?: CodedPreviewMessage[]
    ai_calls?: CodedPreviewAICall[]
    api_calls?: CodedPreviewAPICall[]
    context?: Record<string, unknown>
    step?: string
    status?: string
  }>
  session_id?: string
  flow_key?: string
  status?: string
}

const statusLabel = computed(() => {
  const match = (props.flow.steps || []).find((item) => outlineActive(item.name))
  return match?.label || step.value
})

function outlineActive(name: string) {
  if (name === step.value) return true
  if (name === 'buy' && buyAsks.has(step.value)) return true
  if (name === 'agent' && step.value === 'transfer') return true
  return false
}

function newId() {
  return Math.random().toString(36).slice(2)
}

function toButton(button: { id: string; title: string; description?: string; type?: string; url?: string; phone_number?: string }): ButtonConfig {
  return {
    id: button.id,
    title: button.title,
    description: button.description,
    type: (button.type || 'reply') as ButtonConfig['type'],
    url: button.url,
    phone_number: button.phone_number,
  }
}

function toMessage(message: CodedPreviewMessage): SimulationMessage {
  const buttons = (message.buttons || []).map(toButton)
  const cards = (message.cards || []).map((card) => ({
    mediaType: card.media_type === 'video' ? 'video' as const : 'image' as const,
    mediaUrl: card.media_url,
    body: card.body,
    buttons: (card.buttons || []).map(toButton),
  }))
  const type = message.type === 'debug' || message.type === 'system' || message.type === 'user'
    ? message.type
    : 'bot'
  return {
    id: newId(),
    type,
    content: message.content,
    timestamp: new Date(),
    stepName: message.step,
    buttons: buttons.length ? buttons : undefined,
    interactive: message.interactive,
    cards: cards.length ? cards : undefined,
    header: message.header || undefined,
    headerImage: message.header_image || undefined,
    footer: message.footer || undefined,
    listButton: message.list_button || undefined,
    context: message.context,
    ai: message.ai,
  }
}

function unwrap(payload: { data?: CodedPreviewResponse } & Partial<CodedPreviewResponse>): CodedPreviewResponse {
  if (payload.data?.session_id) return payload.data
  return payload as CodedPreviewResponse
}

async function turn(
  extra: Partial<CodedPreviewRequest>,
  userText?: string,
  media?: PreviewMediaPayload,
) {
  if (!props.account || busy.value) return
  if (userText || media) {
    messages.value.push({
      id: newId(),
      type: 'user',
      content: userText || '',
      timestamp: new Date(),
      media: media
        ? {
            type: media.type,
            url: media.previewUrl,
            filename: media.filename,
          }
        : undefined,
    })
  }
  busy.value = true
  if (status.value === 'idle') status.value = 'running'
  try {
    const response = await chatbotService.previewCodedFlow(props.flow.key, {
      account: props.account,
      session_id: sessionId.value || undefined,
      phone: phone.value.trim() || undefined,
      mock: mockResponses.value,
      ...extra,
    })
    const body = unwrap(response.data as { data?: CodedPreviewResponse } & Partial<CodedPreviewResponse>)
    sessionId.value = body.session_id
    step.value = body.step || ''
    input.value = body.input || ''
    flowCta.value = body.flow_cta || ''
    sessionContext.value = body.context || {}
    turnAICalls.value = body.ai_calls || []
    if (body.api_calls?.length) {
      previewAPICalls.value = [...previewAPICalls.value, ...body.api_calls]
    }
    messages.value.push(...(body.messages || []).map(toMessage))
    if (body.status === 'needs_mock') {
      needsMock.value = true
      mockOperation.value = body.mock_operation || ''
      mockBody.value = '{\n  \n}'
      status.value = 'running'
      return
    }
    needsMock.value = false
    mockOperation.value = ''
    if (body.status === 'waiting_input') status.value = 'waiting_input'
    else if (body.status === 'completed') status.value = 'completed'
    else status.value = 'error'
    if (body.input === 'whatsapp_flow' && !details.customer_phone) {
      details.customer_phone = phone.value.trim()
    }
  } catch (err) {
    status.value = 'error'
    toast.error(getErrorMessage(err, t('codedFlows.previewError')))
  } finally {
    busy.value = false
  }
}

function start() {
  if (status.value !== 'idle') return
  turn({})
}

function revokeMessageMedia() {
  for (const message of messages.value) {
    const url = message.media?.url
    if (url?.startsWith('blob:')) URL.revokeObjectURL(url)
  }
}

function reset() {
  revokeMessageMedia()
  sessionId.value = ''
  step.value = ''
  input.value = ''
  flowCta.value = ''
  status.value = 'idle'
  messages.value = []
  sessionContext.value = {}
  turnAICalls.value = []
  previewAPICalls.value = []
  needsMock.value = false
  mockOperation.value = ''
  mockBody.value = '{\n  \n}'
  mockResponses.value = true
  importMode.value = false
}

function inboundUserContent(inbound: Record<string, unknown> | undefined): string {
  if (!inbound) return ''
  const parts: string[] = []
  if (typeof inbound.text === 'string' && inbound.text.trim()) {
    parts.push(inbound.text.trim())
  }
  if (typeof inbound.button_id === 'string' && inbound.button_id.trim()) {
    parts.push(`[button:${inbound.button_id.trim()}]`)
  }
  if (inbound.media && typeof inbound.media === 'object') {
    parts.push('[media]')
  }
  if (inbound.flow_response) {
    parts.push('[flow response]')
  }
  return parts.join(' ')
}

function applyImportedLog() {
  let parsed: unknown
  try {
    parsed = JSON.parse(importBody.value)
  } catch {
    toast.error(t('codedFlows.previewImportInvalid'))
    return
  }
  if (
    !parsed ||
    typeof parsed !== 'object' ||
    Array.isArray(parsed) ||
    typeof (parsed as ImportedSessionLog).version !== 'number' ||
    !Array.isArray((parsed as ImportedSessionLog).turns)
  ) {
    toast.error(t('codedFlows.previewImportInvalid'))
    return
  }
  const log = parsed as ImportedSessionLog
  if (!log.turns.length) {
    toast.error(t('codedFlows.previewImportInvalid'))
    return
  }
  revokeMessageMedia()
  sessionId.value = typeof log.session_id === 'string' ? log.session_id : ''
  step.value = ''
  input.value = ''
  flowCta.value = ''
  messages.value = []
  sessionContext.value = {}
  turnAICalls.value = []
  previewAPICalls.value = []
  needsMock.value = false
  mockOperation.value = ''
  importMode.value = true
  status.value = 'completed'

  const allAI: CodedPreviewAICall[] = []
  const allAPI: CodedPreviewAPICall[] = []
  for (const turnItem of log.turns) {
    const userText = inboundUserContent(turnItem.inbound)
    if (userText) {
      messages.value.push({
        id: newId(),
        type: 'user',
        content: userText,
        timestamp: turnItem.at ? new Date(turnItem.at) : new Date(),
      })
    }
    messages.value.push(...(turnItem.messages || []).map(toMessage))
    if (turnItem.ai_calls?.length) {
      allAI.push(...turnItem.ai_calls)
    }
    if (turnItem.api_calls?.length) {
      allAPI.push(...turnItem.api_calls)
    }
    if (turnItem.context && typeof turnItem.context === 'object') {
      sessionContext.value = turnItem.context
    }
    if (turnItem.step) {
      step.value = turnItem.step
    }
  }
  turnAICalls.value = allAI
  previewAPICalls.value = allAPI
  toast.success(t('codedFlows.previewImportLoaded'))
}

function submitMock() {
  if (!needsMock.value || !mockOperation.value) return
  let parsed: unknown
  try {
    parsed = JSON.parse(mockBody.value)
  } catch {
    toast.error(t('codedFlows.previewMockInvalid'))
    return
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    toast.error(t('codedFlows.previewMockInvalid'))
    return
  }
  turn({ mocks: { [mockOperation.value]: parsed as Record<string, unknown> } })
}

function selectButton(button: ButtonConfig) {
  if (!waiting.value || input.value !== 'button') return
  turn({ text: button.title, button_id: button.id }, button.title)
}

function submitText(value: string) {
  const text = value.trim()
  // Reply buttons, lists, and carousels still accept a typed message on WhatsApp.
  if (!text || !waiting.value || (input.value !== 'text' && input.value !== 'button')) return
  turn({ text }, text)
}

function submitMedia(caption: string, media: PreviewMediaPayload) {
  if (!waiting.value || (input.value !== 'text' && input.value !== 'button')) return
  turn(
    {
      text: caption.trim(),
      media: {
        type: media.type,
        mime_type: media.mimeType,
        filename: media.filename,
      },
    },
    caption.trim(),
    media,
  )
}

function submitFlow() {
  if (!waiting.value || input.value !== 'whatsapp_flow') return
  const flowResponse: Record<string, string> = {}
  for (const field of detailFields) {
    const value = details[field.key].trim()
    if (value) flowResponse[field.key] = value
  }
  if (!flowResponse.customer_phone && phone.value.trim()) {
    flowResponse.customer_phone = phone.value.trim()
  }
  turn({ flow_response: flowResponse }, flowCta.value || t('codedFlows.previewFlowSubmit'))
}

function submitLocation() {
  if (!waiting.value || input.value !== 'location') return
  const lat = Number(locationLat.value)
  const lng = Number(locationLng.value)
  if (!Number.isFinite(lat) || !Number.isFinite(lng) || (lat === 0 && lng === 0)) {
    toast.error(t('codedFlows.previewLocationInvalid'))
    return
  }
  const payload = JSON.stringify({ latitude: lat, longitude: lng })
  turn({ text: payload }, t('codedFlows.previewLocationSubmit'))
}
</script>

<template>
  <div class="flex h-full min-h-0 w-full flex-1 flex-row overflow-hidden">
    <PreviewPhone
      :name="flow.name"
      :status="status"
      :status-label="statusLabel"
      :messages="messages"
      :waiting="waiting"
      :busy="busy"
      :input-type="input || null"
      :flow-cta="flowCta"
      allow-media
      @select="selectButton"
      @submit="submitText"
      @submit-media="submitMedia"
      @complete-flow="submitFlow"
    />

    <aside class="flex h-full min-h-0 w-80 shrink-0 flex-col overflow-hidden border-l bg-background">
      <div class="flex shrink-0 items-center gap-2 border-b px-4 py-3">
        <Button size="sm" :disabled="busy || importMode || status !== 'idle'" @click="start">
          <Play class="h-4 w-4 mr-1" />
          {{ $t('codedFlows.previewStart') }}
        </Button>
        <Button size="sm" variant="outline" :disabled="busy || (status === 'idle' && !importMode)" @click="reset">
          <RotateCcw class="h-4 w-4 mr-1" />
          {{ $t('codedFlows.previewReset') }}
        </Button>
      </div>

      <div v-if="needsMock" class="shrink-0 space-y-2 border-b px-4 py-3">
        <Label class="text-xs">{{ $t('codedFlows.previewMockFor', { operation: mockOperation }) }}</Label>
        <Textarea v-model="mockBody" class="min-h-28 font-mono text-xs" :disabled="busy" />
        <Button size="sm" class="w-full" :disabled="busy" @click="submitMock">
          {{ $t('codedFlows.previewMockSubmit') }}
        </Button>
      </div>

      <div
        data-testid="coded-flow-preview-import"
        class="shrink-0 space-y-2 border-b px-4 py-3"
      >
        <div>
          <p class="text-sm font-medium">{{ $t('codedFlows.previewImport') }}</p>
          <p class="mt-1 text-[11px] text-muted-foreground">{{ $t('codedFlows.previewImportHint') }}</p>
        </div>
        <Textarea
          v-model="importBody"
          class="min-h-24 font-mono text-xs"
          :disabled="busy"
          :placeholder="$t('codedFlows.previewImportPlaceholder')"
        />
        <Button
          size="sm"
          class="w-full"
          data-testid="coded-flow-preview-import-load"
          :disabled="busy || !importBody.trim()"
          @click="applyImportedLog"
        >
          {{ $t('codedFlows.previewImportLoad') }}
        </Button>
        <p v-if="importMode" class="text-[11px] text-muted-foreground">
          {{ $t('codedFlows.previewImportReadOnly') }}
        </p>
      </div>

      <div class="min-h-0 flex-1 overflow-y-auto">
        <div class="space-y-4 p-4">
          <div v-if="!importMode" class="flex items-start justify-between gap-3">
            <div>
              <p class="text-sm font-medium">{{ $t('codedFlows.previewMock') }}</p>
              <p class="text-[11px] text-muted-foreground mt-1">{{ $t('codedFlows.previewMockHint') }}</p>
            </div>
            <Switch
              :checked="mockResponses"
              :disabled="status !== 'idle'"
              @update:checked="mockResponses = $event"
            />
          </div>

          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('codedFlows.previewPhone') }}</Label>
            <Input v-model="phone" :disabled="busy" placeholder="910000000000" />
            <p class="text-[11px] text-muted-foreground">{{ $t('codedFlows.previewPhoneHint') }}</p>
          </div>

          <div>
            <p class="text-xs font-medium text-muted-foreground mb-2">{{ $t('codedFlows.steps') }}</p>
            <ol class="space-y-1">
              <li
                v-for="item in flow.steps || []"
                :key="item.name"
                class="text-sm rounded-md px-2 py-1"
                :class="outlineActive(item.name) ? 'bg-primary/10 text-foreground font-medium' : 'text-muted-foreground'"
              >
                {{ item.label }}
              </li>
            </ol>
          </div>

          <FlowDebugSections
            :context="sessionContext"
            :ai-calls="turnAICalls"
            :api-calls="previewAPICalls"
          />

          <div v-if="waiting && input === 'whatsapp_flow'" class="space-y-2">
            <div>
              <p class="text-xs font-medium">{{ $t('codedFlows.previewFlow') }}</p>
              <p class="text-[11px] text-muted-foreground">{{ $t('codedFlows.previewFlowHint') }}</p>
            </div>
            <div v-for="field in detailFields" :key="field.key" class="space-y-1">
              <Label class="text-[11px]">{{ field.label }}</Label>
              <Input v-model="details[field.key]" class="h-8 text-xs" :disabled="busy" />
            </div>
            <Button size="sm" class="w-full" :disabled="busy" @click="submitFlow">
              {{ flowCta || $t('codedFlows.previewFlowSubmit') }}
            </Button>
          </div>

          <div v-if="waiting && input === 'location'" class="space-y-2">
            <div>
              <p class="text-xs font-medium">{{ $t('codedFlows.previewLocation') }}</p>
              <p class="text-[11px] text-muted-foreground">{{ $t('codedFlows.previewLocationHint') }}</p>
            </div>
            <div class="space-y-1">
              <Label class="text-[11px]">{{ $t('codedFlows.previewLatitude') }}</Label>
              <Input v-model="locationLat" class="h-8 text-xs" :disabled="busy" />
            </div>
            <div class="space-y-1">
              <Label class="text-[11px]">{{ $t('codedFlows.previewLongitude') }}</Label>
              <Input v-model="locationLng" class="h-8 text-xs" :disabled="busy" />
            </div>
            <Button size="sm" class="w-full" :disabled="busy" @click="submitLocation">
              {{ $t('codedFlows.previewLocationSubmit') }}
            </Button>
          </div>

          <p class="text-[11px] text-muted-foreground">{{ $t('codedFlows.previewNote') }}</p>
        </div>
      </div>
    </aside>
  </div>
</template>
