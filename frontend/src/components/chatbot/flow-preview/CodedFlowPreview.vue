<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from 'vue-sonner'
import { Play, RotateCcw, Braces, ChevronDown, ChevronRight, Sparkles, Copy } from 'lucide-vue-next'
import {
  chatbotService,
  type CodedFlowBinding,
  type CodedPreviewAICall,
  type CodedPreviewMessage,
  type CodedPreviewRequest,
  type CodedPreviewResponse,
} from '@/services/api'
import type { ButtonConfig, SimulationMessage, SimulationStatus } from '@/types/flow-preview'
import { getErrorMessage } from '@/lib/api-utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import PreviewPhone from './PreviewPhone.vue'
import JsonTree from './JsonTree.vue'

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
const sessionId = ref('')
const step = ref('')
const input = ref<CodedPreviewResponse['input']>('')
const flowCta = ref('')
const status = ref<SimulationStatus>('idle')
const busy = ref(false)
const messages = ref<SimulationMessage[]>([])
const sessionContext = ref<Record<string, unknown>>({})
const turnAICalls = ref<CodedPreviewAICall[]>([])
const contextExpanded = ref(false)
const aiExpanded = ref(true)
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

const waiting = computed(() => status.value === 'waiting_input')
const hasContext = computed(() => Object.keys(sessionContext.value || {}).length > 0)

function formatDebugValue(value: unknown) {
  if (value === null || value === undefined) return String(value)
  if (typeof value === 'object') return JSON.stringify(value, null, 2)
  return String(value)
}

async function copyJSON(value: unknown, emptyMessage: string) {
  if (value == null || (typeof value === 'object' && Object.keys(value as object).length === 0)
    || (Array.isArray(value) && value.length === 0)) {
    toast.error(emptyMessage)
    return
  }
  try {
    await navigator.clipboard.writeText(JSON.stringify(value, null, 2))
    toast.success(t('common.copiedToClipboard'))
  } catch {
    toast.error(t('common.clipboardFailed'))
  }
}

function copyContext(event: Event) {
  event.preventDefault()
  event.stopPropagation()
  copyJSON(sessionContext.value, t('codedFlows.previewContextEmpty'))
}

function copyAI(event: Event) {
  event.preventDefault()
  event.stopPropagation()
  copyJSON(turnAICalls.value, t('codedFlows.previewAIEmpty'))
}

const statusLabel = computed(() => {
  const match = props.flow.steps.find((item) => outlineActive(item.name))
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

async function turn(extra: Partial<CodedPreviewRequest>, userText?: string) {
  if (!props.account || busy.value) return
  if (userText) {
    messages.value.push({
      id: newId(),
      type: 'user',
      content: userText,
      timestamp: new Date(),
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

function reset() {
  sessionId.value = ''
  step.value = ''
  input.value = ''
  flowCta.value = ''
  status.value = 'idle'
  messages.value = []
  sessionContext.value = {}
  turnAICalls.value = []
  needsMock.value = false
  mockOperation.value = ''
  mockBody.value = '{\n  \n}'
  mockResponses.value = true
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
  <div class="flex flex-1 min-h-0 h-full">
    <PreviewPhone
      :name="flow.name"
      :status="status"
      :status-label="statusLabel"
      :messages="messages"
      :waiting="waiting"
      :busy="busy"
      :input-type="input || null"
      :flow-cta="flowCta"
      @select="selectButton"
      @submit="submitText"
      @complete-flow="submitFlow"
    />

    <aside class="w-80 shrink-0 border-l bg-background flex flex-col min-h-0">
      <div class="px-4 py-3 border-b flex items-center gap-2">
        <Button size="sm" :disabled="busy || status !== 'idle'" @click="start">
          <Play class="h-4 w-4 mr-1" />
          {{ $t('codedFlows.previewStart') }}
        </Button>
        <Button size="sm" variant="outline" :disabled="busy || status === 'idle'" @click="reset">
          <RotateCcw class="h-4 w-4 mr-1" />
          {{ $t('codedFlows.previewReset') }}
        </Button>
      </div>

      <ScrollArea class="flex-1">
        <div class="p-4 space-y-4">
          <div class="flex items-start justify-between gap-3">
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

          <div v-if="needsMock" class="space-y-2">
            <Label class="text-xs">{{ $t('codedFlows.previewMockFor', { operation: mockOperation }) }}</Label>
            <Textarea v-model="mockBody" class="min-h-28 font-mono text-xs" :disabled="busy" />
            <Button size="sm" class="w-full" :disabled="busy" @click="submitMock">
              {{ $t('codedFlows.previewMockSubmit') }}
            </Button>
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
                v-for="item in flow.steps"
                :key="item.name"
                class="text-sm rounded-md px-2 py-1"
                :class="outlineActive(item.name) ? 'bg-primary/10 text-foreground font-medium' : 'text-muted-foreground'"
              >
                {{ item.label }}
              </li>
            </ol>
          </div>

          <Collapsible v-model:open="contextExpanded">
            <div class="flex items-center gap-1">
              <CollapsibleTrigger class="flex items-center gap-2 flex-1 text-xs font-medium text-muted-foreground hover:text-foreground">
                <ChevronDown v-if="contextExpanded" class="h-3.5 w-3.5" />
                <ChevronRight v-else class="h-3.5 w-3.5" />
                <Braces class="h-3.5 w-3.5" />
                {{ $t('codedFlows.previewContext') }}
                <span class="ml-auto text-[10px]">{{ Object.keys(sessionContext || {}).length }}</span>
              </CollapsibleTrigger>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                class="h-7 w-7 shrink-0"
                :disabled="!hasContext"
                :title="$t('codedFlows.previewContextCopy')"
                @click="copyContext"
              >
                <Copy class="h-3.5 w-3.5" />
              </Button>
            </div>
            <CollapsibleContent class="mt-2">
              <div v-if="!hasContext" class="text-[11px] text-muted-foreground">
                {{ $t('codedFlows.previewContextEmpty') }}
              </div>
              <div v-else class="rounded-md border bg-muted/30 p-2 max-h-56 overflow-auto">
                <JsonTree :value="sessionContext" />
              </div>
            </CollapsibleContent>
          </Collapsible>

          <Collapsible v-model:open="aiExpanded">
            <div class="flex items-center gap-1">
              <CollapsibleTrigger class="flex items-center gap-2 flex-1 text-xs font-medium text-muted-foreground hover:text-foreground">
                <ChevronDown v-if="aiExpanded" class="h-3.5 w-3.5" />
                <ChevronRight v-else class="h-3.5 w-3.5" />
                <Sparkles class="h-3.5 w-3.5" />
                {{ $t('codedFlows.previewAI') }}
                <span class="ml-auto text-[10px]">{{ turnAICalls.length }}</span>
              </CollapsibleTrigger>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                class="h-7 w-7 shrink-0"
                :disabled="turnAICalls.length === 0"
                :title="$t('codedFlows.previewAICopy')"
                @click="copyAI"
              >
                <Copy class="h-3.5 w-3.5" />
              </Button>
            </div>
            <CollapsibleContent class="mt-2">
              <div v-if="turnAICalls.length === 0" class="text-[11px] text-muted-foreground">
                {{ $t('codedFlows.previewAIEmpty') }}
              </div>
              <div v-else class="space-y-2 max-h-72 overflow-auto">
                <div
                  v-for="(call, idx) in turnAICalls"
                  :key="`${call.role}-${idx}`"
                  class="rounded-md border bg-sky-50/60 dark:bg-sky-950/20 p-2 text-[11px] space-y-1"
                >
                  <p class="font-medium">
                    {{ call.role }}
                    <span v-if="call.route" class="text-muted-foreground">→ {{ call.route }}</span>
                  </p>
                  <p v-if="call.reasoning" class="whitespace-pre-wrap">{{ call.reasoning }}</p>
                  <p v-if="call.language">language: {{ call.language }}</p>
                  <p v-if="call.confidence != null">confidence: {{ call.confidence }}</p>
                  <p v-if="call.grounded != null">grounded: {{ call.grounded }}</p>
                  <p v-if="call.error" class="text-destructive">error: {{ call.error }}</p>
                  <details v-if="call.prompt">
                    <summary class="cursor-pointer text-muted-foreground">prompt</summary>
                    <pre class="mt-1 whitespace-pre-wrap break-all">{{ call.prompt }}</pre>
                  </details>
                  <details v-if="call.response">
                    <summary class="cursor-pointer text-muted-foreground">response</summary>
                    <pre class="mt-1 whitespace-pre-wrap break-all">{{ call.response }}</pre>
                  </details>
                  <details v-if="call.parsed">
                    <summary class="cursor-pointer text-muted-foreground">parsed</summary>
                    <pre class="mt-1 whitespace-pre-wrap break-all">{{ formatDebugValue(call.parsed) }}</pre>
                  </details>
                </div>
              </div>
            </CollapsibleContent>
          </Collapsible>

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
      </ScrollArea>
    </aside>
  </div>
</template>
