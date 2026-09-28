<script setup lang="ts">
import { computed, ref } from 'vue'
import ItemTextHint from '@/components/chatbot/ItemTextHint.vue'
import KeyValueRows from '@/components/chatbot/KeyValueRows.vue'
import type { ChatNode } from '@/services/api'
import { tiqrStoreOperationDef, tiqrStoreOperationsFor, tiqrStoreApiType } from '@/components/chatbot/tiqrStoreApiCatalog'
import { useTeamsStore } from '@/stores/teams'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { Trash2, Plus } from 'lucide-vue-next'

const props = defineProps<{
  node: ChatNode
  currentFlowId?: string
  availableFlows?: { id: string; name: string }[]
}>()

const emit = defineEmits<{
  'update:node': [node: ChatNode]
  'delete': []
}>()

const teamsStore = useTeamsStore()
if (teamsStore.teams.length === 0) teamsStore.fetchTeams()

const config = computed(() => props.node.config || {})

function updateConfig(key: string, value: any) {
  emit('update:node', {
    ...props.node,
    config: { ...props.node.config, [key]: value },
  })
}

function updateLabel(label: string) {
  emit('update:node', { ...props.node, label })
}

// "Text" in the palette covers both v2 `message` (fire-and-forget) and
// v2 `prompt` (blocking + validating). The author chooses by picking an
// expected response type — anything other than "none" flips the node to
// `prompt` under the hood and exposes validation + store_as fields.
const isTextNode = computed(() => props.node.type === 'message' || props.node.type === 'prompt')

const expectedResponse = computed<string>(() => {
  if (props.node.type === 'message') return 'none'
  return (props.node.config?.input_type as string) || 'text'
})

function setExpectedResponse(value: string) {
  if (value === 'none') {
    // Drop prompt-only fields, switch type back to message.
    const { input_type: _unused, validation_regex: _r, validation_error: _e, store_as: _s, max_retries: _m, body, ...rest } = props.node.config || {}
    void _unused; void _r; void _e; void _s; void _m
    emit('update:node', {
      ...props.node,
      type: 'message',
      config: {
        ...rest,
        // The text in the message body lived under either `body` (prompt
        // shape) or `message` (message shape) depending on history —
        // collapse to `message` for fire-and-forget.
        message: body || props.node.config?.message || '',
      },
    })
    return
  }
  // Switch to prompt and remember the response variant.
  const { message, ...rest } = props.node.config || {}
  emit('update:node', {
    ...props.node,
    type: 'prompt',
    config: {
      ...rest,
      body: rest.body || message || '',
      input_type: value,
    },
  })
}

const textBodyValue = computed(() => {
  if (props.node.type === 'prompt') return (props.node.config?.body as string) || ''
  return (props.node.config?.message as string) || ''
})

function updateTextBody(value: string) {
  const key = props.node.type === 'prompt' ? 'body' : 'message'
  updateConfig(key, value)
}

// Buttons helpers
function addReplyButton() {
  const buttons = [...(config.value.buttons || [])]
  const id = `btn_${Date.now()}_${buttons.length}`
  buttons.push({ id, title: '', type: 'reply' })
  updateConfig('buttons', buttons)
}

function addUrlButton() {
  const buttons = [...(config.value.buttons || [])]
  const id = `btn_${Date.now()}_${buttons.length}`
  buttons.push({ id, title: '', type: 'url', url: '' })
  updateConfig('buttons', buttons)
}

function addPhoneButton() {
  const buttons = [...(config.value.buttons || [])]
  const id = `btn_${Date.now()}_${buttons.length}`
  buttons.push({ id, title: '', type: 'phone', phone_number: '' })
  updateConfig('buttons', buttons)
}

function updateButton(idx: number, field: string, value: any) {
  const buttons = [...(config.value.buttons || [])]
  buttons[idx] = { ...buttons[idx], [field]: value }
  updateConfig('buttons', buttons)
}

function removeButton(idx: number) {
  const buttons = [...(config.value.buttons || [])]
  buttons.splice(idx, 1)
  updateConfig('buttons', buttons)
}

const hasReplyButtons = computed(() =>
  (config.value.buttons || []).some((b: any) => !b.type || b.type === 'reply'),
)
const hasCtaButtons = computed(() =>
  (config.value.buttons || []).some((b: any) => b.type === 'url' || b.type === 'phone'),
)
const replyCount = computed(() =>
  (config.value.buttons || []).filter((b: any) => !b.type || b.type === 'reply').length,
)
const ctaCount = computed(() =>
  (config.value.buttons || []).filter((b: any) => b.type === 'url' || b.type === 'phone').length,
)

const buttonMode = computed(() => {
  const mode = config.value.mode
  if (mode === 'list' || mode === 'carousel') return mode
  return 'reply'
})
const buttonSource = computed(() => (config.value.source === 'dynamic' ? 'dynamic' : 'static'))
const cardAction = computed(() => (config.value.card_action === 'url' ? 'url' : 'reply'))
const carouselMediaType = computed(() => (config.value.media_type === 'video' ? 'video' : 'image'))

const headerRows = ref<InstanceType<typeof KeyValueRows> | null>(null)
const responseRows = ref<InstanceType<typeof KeyValueRows> | null>(null)
const selectionRows = ref<InstanceType<typeof KeyValueRows> | null>(null)

function setButtonMode(value: string) {
  if (value === 'list' || value === 'carousel') updateConfig('mode', value)
  else updateConfig('mode', 'reply')
}

function addCarouselCard() {
  const buttons = [...(config.value.buttons || [])]
  if (buttons.length >= 10) return
  const id = `card_${Date.now()}_${buttons.length}`
  buttons.push({
    id,
    title: '',
    media_type: 'image',
    media_url: '',
    body: '',
    url: '',
    title_2: '',
    id_2: '',
  })
  updateConfig('buttons', buttons)
}
const dynamicType = computed(() => {
  const t = config.value.dynamic_type
  if (t === 'url' || t === 'phone') return t
  return 'reply'
})

function addListRow() {
  const buttons = [...(config.value.buttons || [])]
  if (buttons.length >= 10) return
  const id = `row_${Date.now()}_${buttons.length}`
  buttons.push({ id, title: '', description: '' })
  updateConfig('buttons', buttons)
}

function updateParam(key: string, value: string) {
  updateConfig('params', { ...(config.value.params || {}), [key]: value })
}

const tiqrOperation = computed(() => tiqrStoreOperationDef(config.value.operation))
const tiqrApiType = computed(() => tiqrStoreApiType(config.value.api_type))
const tiqrOperations = computed(() => tiqrStoreOperationsFor(config.value.api_type))

function updateTiqrApiType(value: string) {
  const next = tiqrStoreApiType(value)
  const allowed = tiqrStoreOperationsFor(next)
  const current = String(config.value.operation || '')
  const stillValid = allowed.some((op) => op.value === current)
  emit('update:node', {
    ...props.node,
    config: {
      ...props.node.config,
      api_type: next,
      operation: stillValid ? current : (allowed[0]?.value || 'list_products'),
    },
  })
}

// Timing schedule
const defaultSchedule = [
  { day: 'monday', enabled: true, start_time: '09:00', end_time: '17:00' },
  { day: 'tuesday', enabled: true, start_time: '09:00', end_time: '17:00' },
  { day: 'wednesday', enabled: true, start_time: '09:00', end_time: '17:00' },
  { day: 'thursday', enabled: true, start_time: '09:00', end_time: '17:00' },
  { day: 'friday', enabled: true, start_time: '09:00', end_time: '17:00' },
  { day: 'saturday', enabled: false, start_time: '09:00', end_time: '17:00' },
  { day: 'sunday', enabled: false, start_time: '09:00', end_time: '17:00' },
]
const schedule = computed(() => config.value.schedule || defaultSchedule)

function updateScheduleEntry(idx: number, field: string, value: any) {
  const sched = [...schedule.value]
  sched[idx] = { ...sched[idx], [field]: value }
  updateConfig('schedule', sched)
}

const gotoFlowTargets = computed(() =>
  (props.availableFlows || []).filter((f) => f.id !== props.currentFlowId),
)

type SetAssignmentOp = 'set' | 'append'
type SetAssignmentValueType = 'expression' | 'json'
type SetAssignment = { name: string; value: string; op: SetAssignmentOp; value_type: SetAssignmentValueType }

const jsonAssignmentPlaceholder = `{
  "id": "{{product_id}}",
  "qty": 1
}`

function assignmentText(value: unknown): string {
  if (value == null) return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

function assignmentOp(value: unknown): SetAssignmentOp {
  return value === 'append' ? 'append' : 'set'
}

function assignmentValueType(value: unknown): SetAssignmentValueType {
  return value === 'json' ? 'json' : 'expression'
}

const setAssignments = computed<SetAssignment[]>(() => {
  const set = config.value.set
  if (Array.isArray(set)) {
    return set.map((row: any) => ({
      name: typeof row?.name === 'string' ? row.name : '',
      value: assignmentText(row?.value),
      op: assignmentOp(row?.op),
      value_type: assignmentValueType(row?.value_type),
    }))
  }
  if (set && typeof set === 'object') {
    return Object.entries(set as Record<string, unknown>).map(([name, value]) => ({
      name,
      value: assignmentText(value),
      op: 'set' as const,
      value_type: 'expression' as const,
    }))
  }
  return []
})

function writeAssignments(rows: SetAssignment[]) {
  updateConfig('set', rows.map((row) => ({
    name: row.name,
    value: row.value,
    op: row.op,
    value_type: row.value_type,
  })))
}

function addAssignment() {
  writeAssignments([...setAssignments.value, { name: '', value: '', op: 'set', value_type: 'expression' }])
}

function updateAssignment(index: number, patch: Partial<SetAssignment>) {
  const rows = setAssignments.value.map((row, i) => (i === index ? { ...row, ...patch } : row))
  writeAssignments(rows)
}

function removeAssignment(index: number) {
  writeAssignments(setAssignments.value.filter((_, i) => i !== index))
}

const typeLabel: Record<string, string> = {
  start: 'Start',
  message: 'Message',
  prompt: 'Prompt',
  buttons: 'Buttons',
  api_call: 'API Call',
  tiqr_store_api: 'TiQR Store API',
  condition: 'Condition',
  set_variable: 'Assign',
  timing: 'Timing',
  transfer: 'Transfer',
  end: 'End',
  goto_flow: 'Go to Flow',
  whatsapp_flow: 'WhatsApp Flow',
  webhook: 'Webhook',
}
</script>

<template>
  <div class="space-y-4 p-4">
    <div class="flex items-center justify-between">
      <h3 class="font-semibold text-sm">{{ typeLabel[node.type] || node.type }}</h3>
      <Button v-if="node.type !== 'start'" variant="ghost" size="icon" class="h-7 w-7" @click="emit('delete')">
        <Trash2 class="h-3.5 w-3.5 text-destructive" />
      </Button>
    </div>

    <!-- Start: nothing to configure beyond the label. -->
    <p v-if="node.type === 'start'" class="text-xs text-muted-foreground">
      Flow entry point. Wire its output to the first node that should run.
    </p>

    <!-- Label -->
    <div v-if="node.type !== 'start'" class="space-y-1.5">
      <Label class="text-xs">Label</Label>
      <Input :model-value="node.label" @update:model-value="(v) => updateLabel(String(v ?? ''))" class="h-8 text-sm" />
    </div>

    <!-- text (message OR prompt) -->
    <template v-if="isTextNode">
      <div class="space-y-1.5">
        <Label class="text-xs">Message</Label>
        <Textarea
          :model-value="textBodyValue"
          @update:model-value="(v: string) => updateTextBody(String(v ?? ''))"
          placeholder="Enter your message"
          class="min-h-[80px] text-xs"
        />
        <p class="text-[10px] text-muted-foreground">Use double-brace placeholders (e.g. <code>customer_name</code>) to interpolate session variables.</p>
      </div>
      <div class="space-y-1.5">
        <Label class="text-xs">Expected response</Label>
        <Select :model-value="expectedResponse" @update:model-value="(v: any) => setExpectedResponse(v)">
          <SelectTrigger class="h-8 text-sm"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="none">None (fire-and-forget)</SelectItem>
            <SelectItem value="text">Text</SelectItem>
            <SelectItem value="number">Number</SelectItem>
            <SelectItem value="email">Email</SelectItem>
            <SelectItem value="phone">Phone</SelectItem>
            <SelectItem value="date">Date</SelectItem>
            <SelectItem value="select">Selection</SelectItem>
          </SelectContent>
        </Select>
        <p class="text-[10px] text-muted-foreground">When set, the flow waits for the user's reply before continuing.</p>
      </div>
      <template v-if="node.type === 'prompt'">
        <div class="space-y-1.5">
          <Label class="text-xs">Store response as</Label>
          <Input
            :model-value="config.store_as || ''"
            @update:model-value="(v: string) => updateConfig('store_as', v)"
            placeholder="variable_name"
            class="h-8 text-sm font-mono"
          />
        </div>
        <div class="space-y-1.5">
          <Label class="text-xs">Validation regex (optional)</Label>
          <Input
            :model-value="config.validation_regex || ''"
            @update:model-value="(v: string) => updateConfig('validation_regex', v)"
            placeholder="^[0-9]+$"
            class="h-8 text-xs font-mono"
          />
        </div>
        <div class="space-y-1.5">
          <Label class="text-xs">Validation error message</Label>
          <Input
            :model-value="config.validation_error || ''"
            @update:model-value="(v: string) => updateConfig('validation_error', v)"
            placeholder="Invalid input. Please try again."
            class="h-8 text-xs"
          />
        </div>
        <div class="space-y-1.5">
          <Label class="text-xs">Max retries</Label>
          <Input
            type="number"
            :model-value="String(config.max_retries ?? 3)"
            @update:model-value="(v: string) => updateConfig('max_retries', parseInt(v) || 3)"
            class="h-8 text-sm"
            min="1"
            max="10"
          />
        </div>
      </template>
    </template>

    <!-- buttons -->
    <template v-if="node.type === 'buttons'">
      <div class="space-y-1.5">
        <Label class="text-xs">Message style</Label>
        <Select :model-value="buttonMode" @update:model-value="(v: any) => setButtonMode(String(v))">
          <SelectTrigger class="h-8 text-sm"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="reply">Reply buttons</SelectItem>
            <SelectItem value="list">List</SelectItem>
            <SelectItem value="carousel">Carousel</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div class="space-y-1.5">
        <Label class="text-xs">Input</Label>
        <Select :model-value="buttonSource" @update:model-value="(v: any) => updateConfig('source', v === 'dynamic' ? 'dynamic' : 'static')">
          <SelectTrigger class="h-8 text-sm"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="static">Static</SelectItem>
            <SelectItem value="dynamic">Dynamic</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div v-if="buttonMode === 'carousel'" class="space-y-1.5">
        <Label class="text-xs">Card action</Label>
        <Select :model-value="cardAction" @update:model-value="(v: any) => updateConfig('card_action', v === 'url' ? 'url' : 'reply')">
          <SelectTrigger class="h-8 text-sm"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="reply">Quick reply</SelectItem>
            <SelectItem value="url">URL</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <template v-if="buttonMode === 'list'">
        <div class="space-y-1.5">
          <Label class="text-xs">Header</Label>
          <Input
            :model-value="config.header || ''"
            @update:model-value="(v: string) => updateConfig('header', v)"
            placeholder="Optional header"
            maxlength="60"
            class="h-8 text-sm"
          />
        </div>
        <div class="space-y-1.5">
          <Label class="text-xs">Footer</Label>
          <Input
            :model-value="config.footer || ''"
            @update:model-value="(v: string) => updateConfig('footer', v)"
            placeholder="Optional footer"
            maxlength="60"
            class="h-8 text-sm"
          />
        </div>
        <div class="space-y-1.5">
          <Label class="text-xs">List button</Label>
          <Input
            :model-value="config.list_button || ''"
            @update:model-value="(v: string) => updateConfig('list_button', v)"
            placeholder="Select"
            maxlength="20"
            class="h-8 text-sm"
          />
        </div>
        <div class="space-y-1.5">
          <Label class="text-xs">Section title</Label>
          <Input
            :model-value="config.section_title || ''"
            @update:model-value="(v: string) => updateConfig('section_title', v)"
            placeholder="Options"
            maxlength="24"
            class="h-8 text-sm"
          />
        </div>
      </template>

      <div class="space-y-1.5">
        <Label class="text-xs">Body</Label>
        <Textarea
          :model-value="config.body || ''"
          @update:model-value="(v: string) => updateConfig('body', v)"
          placeholder="Message shown above the buttons"
          :maxlength="buttonMode === 'carousel' ? 1024 : undefined"
          class="min-h-[60px] text-xs"
        />
      </div>

      <template v-if="buttonMode === 'reply'">
        <div class="space-y-1.5">
          <Label class="text-xs">Header image</Label>
          <Input
            :model-value="config.header_image || ''"
            @update:model-value="(v: string) => updateConfig('header_image', v)"
            placeholder="{{products[0].images[0].original_url}}"
            class="h-8 text-xs font-mono"
          />
          <p class="text-[10px] text-muted-foreground">JPEG or PNG shown above the message. Leave blank for a text-only button message.</p>
        </div>
        <div class="space-y-1.5">
          <Label class="text-xs">Fallback image URL</Label>
          <Input
            :model-value="config.fallback_media_url || ''"
            @update:model-value="(v: string) => updateConfig('fallback_media_url', v)"
            placeholder="https://example.com/fallback.jpg"
            class="h-8 text-xs font-mono"
          />
        </div>
      </template>

      <div v-if="buttonMode === 'reply' && buttonSource === 'static'" class="space-y-1.5">
        <div class="flex items-center justify-between">
          <Label class="text-xs">Button Options ({{ (config.buttons || []).length }}/{{ hasCtaButtons ? 2 : 10 }})</Label>
        </div>
        <div class="flex gap-1">
          <Button variant="outline" size="sm" class="h-7 text-xs" :disabled="hasCtaButtons || replyCount >= 10" @click="addReplyButton">
            <Plus class="h-3 w-3 mr-0.5" /> Reply
          </Button>
          <Button variant="outline" size="sm" class="h-7 text-xs" :disabled="hasReplyButtons || ctaCount >= 2" @click="addUrlButton">
            <Plus class="h-3 w-3 mr-0.5" /> URL
          </Button>
          <Button variant="outline" size="sm" class="h-7 text-xs" :disabled="hasReplyButtons || ctaCount >= 2" @click="addPhoneButton">
            <Plus class="h-3 w-3 mr-0.5" /> Phone
          </Button>
        </div>
        <div v-for="(btn, idx) in (config.buttons || [])" :key="btn.id || idx" class="p-2 border rounded-md space-y-2 bg-muted/30">
          <div class="flex items-center gap-1">
            <span class="text-[10px] uppercase text-muted-foreground w-12">{{ btn.type || 'reply' }}</span>
            <Input
              :model-value="btn.title || ''"
              @update:model-value="(v: string) => updateButton(Number(idx), 'title', v)"
              placeholder="Button Title"
              class="h-7 text-xs flex-1"
            />
            <Button variant="ghost" size="icon" class="h-6 w-6" @click="removeButton(Number(idx))">
              <Trash2 class="h-3 w-3 text-destructive" />
            </Button>
          </div>
          <Input
            :model-value="btn.id || ''"
            @update:model-value="(v: string) => updateButton(Number(idx), 'id', v)"
            placeholder="button_id"
            class="h-7 text-xs font-mono"
          />
          <Input
            v-if="btn.type === 'url'"
            :model-value="btn.url || ''"
            @update:model-value="(v: string) => updateButton(Number(idx), 'url', v)"
            placeholder="https://example.com"
            class="h-7 text-xs font-mono"
          />
          <Input
            v-if="btn.type === 'phone'"
            :model-value="btn.phone_number || ''"
            @update:model-value="(v: string) => updateButton(Number(idx), 'phone_number', v)"
            placeholder="+1234567890"
            class="h-7 text-xs font-mono"
          />
        </div>
        <p class="text-[10px] text-muted-foreground">Reply buttons (max 10) send the user's choice back. URL / Phone buttons (max 2 per node, mutually exclusive with Reply) open a link or call. Wire reply buttons to next nodes by dragging from the button handle on the canvas.</p>
      </div>

      <div v-if="buttonMode === 'list' && buttonSource === 'static'" class="space-y-1.5">
        <div class="flex items-center justify-between">
          <Label class="text-xs">List rows ({{ (config.buttons || []).length }}/10)</Label>
        </div>
        <Button variant="outline" size="sm" class="h-7 text-xs" :disabled="(config.buttons || []).length >= 10" @click="addListRow">
          <Plus class="h-3 w-3 mr-0.5" /> Row
        </Button>
        <div v-for="(btn, idx) in (config.buttons || [])" :key="btn.id || idx" class="p-2 border rounded-md space-y-2 bg-muted/30">
          <div class="flex items-center gap-1">
            <Input
              :model-value="btn.title || ''"
              @update:model-value="(v: string) => updateButton(Number(idx), 'title', v)"
              placeholder="Button Title"
              maxlength="24"
              class="h-7 text-xs flex-1"
            />
            <Button variant="ghost" size="icon" class="h-6 w-6" @click="removeButton(Number(idx))">
              <Trash2 class="h-3 w-3 text-destructive" />
            </Button>
          </div>
          <Input
            :model-value="btn.id || ''"
            @update:model-value="(v: string) => updateButton(Number(idx), 'id', v)"
            placeholder="button_id"
            maxlength="200"
            class="h-7 text-xs font-mono"
          />
          <Input
            :model-value="btn.description || ''"
            @update:model-value="(v: string) => updateButton(Number(idx), 'description', v)"
            placeholder="Description"
            maxlength="72"
            class="h-7 text-xs"
          />
        </div>
        <p class="text-[10px] text-muted-foreground">Up to 10 rows. Each row needs a title and id. Wire rows to next nodes from the handles on the canvas.</p>
      </div>

      <div v-if="buttonMode === 'carousel' && buttonSource === 'static'" class="space-y-1.5">
        <div class="space-y-1.5">
          <Label class="text-xs">Fallback media URL</Label>
          <Input
            :model-value="config.fallback_media_url || ''"
            @update:model-value="(v: string) => updateConfig('fallback_media_url', v)"
            placeholder="https://example.com/placeholder.jpg"
            class="h-8 text-sm font-mono"
          />
          <p class="text-[10px] text-muted-foreground">Used when a card's media URL is blank.</p>
        </div>
        <Label class="text-xs">Cards ({{ (config.buttons || []).length }}/10)</Label>
        <Button variant="outline" size="sm" class="h-7 text-xs" :disabled="(config.buttons || []).length >= 10" @click="addCarouselCard">
          <Plus class="h-3 w-3 mr-0.5" /> Card
        </Button>
        <div v-for="(card, idx) in (config.buttons || [])" :key="card.id || idx" class="p-2 border rounded-md space-y-2 bg-muted/30">
          <div class="flex items-center gap-1">
            <Select :model-value="card.media_type === 'video' ? 'video' : 'image'" @update:model-value="(v: any) => updateButton(Number(idx), 'media_type', v === 'video' ? 'video' : 'image')">
              <SelectTrigger class="h-7 text-xs w-24"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="image">Image</SelectItem>
                <SelectItem value="video">Video</SelectItem>
              </SelectContent>
            </Select>
            <Input
              :model-value="card.media_url || ''"
              @update:model-value="(v: string) => updateButton(Number(idx), 'media_url', v)"
              placeholder="https://example.com/image.jpg"
              class="h-7 text-xs flex-1 font-mono"
            />
            <Button variant="ghost" size="icon" class="h-6 w-6" @click="removeButton(Number(idx))">
              <Trash2 class="h-3 w-3 text-destructive" />
            </Button>
          </div>
          <Input
            :model-value="card.body || ''"
            @update:model-value="(v: string) => updateButton(Number(idx), 'body', v)"
            placeholder="Card text"
            maxlength="160"
            class="h-7 text-xs"
          />
          <Input
            :model-value="card.title || ''"
            @update:model-value="(v: string) => updateButton(Number(idx), 'title', v)"
            :placeholder="cardAction === 'url' ? 'Button label' : 'Button title'"
            maxlength="20"
            class="h-7 text-xs"
          />
          <Input
            v-if="cardAction === 'url'"
            :model-value="card.url || ''"
            @update:model-value="(v: string) => updateButton(Number(idx), 'url', v)"
            placeholder="https://example.com"
            class="h-7 text-xs font-mono"
          />
          <template v-else>
            <Input
              :model-value="card.id || ''"
              @update:model-value="(v: string) => updateButton(Number(idx), 'id', v)"
              placeholder="button_id"
              maxlength="256"
              class="h-7 text-xs font-mono"
            />
            <Input
              :model-value="card.title_2 || ''"
              @update:model-value="(v: string) => updateButton(Number(idx), 'title_2', v)"
              placeholder="Second button title"
              maxlength="20"
              class="h-7 text-xs"
            />
            <Input
              :model-value="card.id_2 || ''"
              @update:model-value="(v: string) => updateButton(Number(idx), 'id_2', v)"
              placeholder="second_button_id"
              maxlength="256"
              class="h-7 text-xs font-mono"
            />
          </template>
        </div>
        <p class="text-[10px] text-muted-foreground">2 to 10 cards. Each card needs a public image or video URL. Every card uses the same action and the same number of quick replies.</p>
      </div>

      <div v-if="buttonSource === 'dynamic' && buttonMode !== 'carousel'" class="space-y-1.5">
        <div v-if="buttonMode === 'reply'" class="space-y-1.5">
          <Label class="text-xs">Button type</Label>
          <Select :model-value="dynamicType" @update:model-value="(v: any) => updateConfig('dynamic_type', v)">
            <SelectTrigger class="h-8 text-sm"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="reply">Reply</SelectItem>
              <SelectItem value="url">URL</SelectItem>
              <SelectItem value="phone">Phone</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div class="space-y-1.5">
          <Label class="text-xs">Items variable</Label>
          <Input
            :model-value="config.items_var || ''"
            @update:model-value="(v: string) => updateConfig('items_var', v)"
            placeholder="products"
            class="h-8 text-sm font-mono"
          />
          <p class="text-[10px] text-muted-foreground">Session variable holding an array of objects.</p>
        </div>
        <div class="space-y-1.5">
          <div class="flex items-center gap-1">
            <Label class="text-xs">Title field</Label>
            <ItemTextHint />
          </div>
          <Input
            :model-value="config.title_field || ''"
            @update:model-value="(v: string) => updateConfig('title_field', v)"
            placeholder="name"
            class="h-8 text-sm font-mono"
          />
        </div>
        <div v-if="buttonMode === 'list' || dynamicType === 'reply'" class="space-y-1.5">
          <Label class="text-xs">ID field</Label>
          <Input
            :model-value="config.id_field || ''"
            @update:model-value="(v: string) => updateConfig('id_field', v)"
            placeholder="id"
            class="h-8 text-sm font-mono"
          />
        </div>
        <div v-if="buttonMode === 'list'" class="space-y-1.5">
          <div class="flex items-center gap-1">
            <Label class="text-xs">Description field</Label>
            <ItemTextHint />
          </div>
          <Input
            :model-value="config.description_field || ''"
            @update:model-value="(v: string) => updateConfig('description_field', v)"
            placeholder="description"
            class="h-8 text-sm font-mono"
          />
        </div>
        <div v-if="buttonMode === 'reply' && dynamicType === 'url'" class="space-y-1.5">
          <div class="flex items-center gap-1">
            <Label class="text-xs">URL field</Label>
            <ItemTextHint />
          </div>
          <Input
            :model-value="config.url_field || ''"
            @update:model-value="(v: string) => updateConfig('url_field', v)"
            placeholder="url"
            class="h-8 text-sm font-mono"
          />
        </div>
        <div v-if="buttonMode === 'reply' && dynamicType === 'phone'" class="space-y-1.5">
          <div class="flex items-center gap-1">
            <Label class="text-xs">Phone field</Label>
            <ItemTextHint />
          </div>
          <Input
            :model-value="config.phone_field || ''"
            @update:model-value="(v: string) => updateConfig('phone_field', v)"
            placeholder="phone_number"
            class="h-8 text-sm font-mono"
          />
        </div>
        <p class="text-[10px] text-muted-foreground">Rows are built from this array when the flow runs. Connect a single next step from the node.</p>
      </div>

      <div v-if="buttonSource === 'dynamic' && buttonMode === 'carousel'" class="space-y-1.5">
        <div class="space-y-1.5">
          <Label class="text-xs">Items variable</Label>
          <Input
            :model-value="config.items_var || ''"
            @update:model-value="(v: string) => updateConfig('items_var', v)"
            placeholder="products"
            class="h-8 text-sm font-mono"
          />
          <p class="text-[10px] text-muted-foreground">Session variable holding an array of objects.</p>
        </div>
        <div class="space-y-1.5">
          <Label class="text-xs">Media type</Label>
          <Select :model-value="carouselMediaType" @update:model-value="(v: any) => updateConfig('media_type', v === 'video' ? 'video' : 'image')">
            <SelectTrigger class="h-8 text-sm"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="image">Image</SelectItem>
              <SelectItem value="video">Video</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div class="space-y-1.5">
          <Label class="text-xs">Media field</Label>
          <Input
            :model-value="config.media_field || ''"
            @update:model-value="(v: string) => updateConfig('media_field', v)"
            placeholder="images[0].image"
            class="h-8 text-sm font-mono"
          />
          <p class="text-[10px] text-muted-foreground">Path on each item. Use dots and indexes, such as images[0].image.</p>
        </div>
        <div class="space-y-1.5">
          <Label class="text-xs">Fallback media URL</Label>
          <Input
            :model-value="config.fallback_media_url || ''"
            @update:model-value="(v: string) => updateConfig('fallback_media_url', v)"
            placeholder="https://example.com/placeholder.jpg"
            class="h-8 text-sm font-mono"
          />
          <p class="text-[10px] text-muted-foreground">Used when the media field is blank.</p>
        </div>
        <div class="space-y-1.5">
          <div class="flex items-center gap-1">
            <Label class="text-xs">Body field</Label>
            <ItemTextHint />
          </div>
          <Input
            :model-value="config.body_field || ''"
            @update:model-value="(v: string) => updateConfig('body_field', v)"
            placeholder="description"
            class="h-8 text-sm font-mono"
          />
        </div>
        <div v-if="cardAction === 'url'" class="space-y-1.5">
          <div class="flex items-center gap-1">
            <Label class="text-xs">Button label</Label>
            <ItemTextHint />
          </div>
          <Input
            :model-value="config.button_title || ''"
            @update:model-value="(v: string) => updateConfig('button_title', v)"
            placeholder="Buy now"
            maxlength="20"
            class="h-8 text-sm"
          />
        </div>
        <div v-if="cardAction === 'url'" class="space-y-1.5">
          <div class="flex items-center gap-1">
            <Label class="text-xs">URL field</Label>
            <ItemTextHint />
          </div>
          <Input
            :model-value="config.url_field || ''"
            @update:model-value="(v: string) => updateConfig('url_field', v)"
            placeholder="url"
            class="h-8 text-sm font-mono"
          />
        </div>
        <template v-else>
          <div class="space-y-1.5">
            <div class="flex items-center gap-1">
              <Label class="text-xs">Primary Action Title</Label>
              <ItemTextHint />
            </div>
            <Input
              :model-value="config.title_field || ''"
              @update:model-value="(v: string) => updateConfig('title_field', v)"
              placeholder="Add to cart"
              class="h-8 text-sm"
            />
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">ID field</Label>
            <Input
              :model-value="config.id_field || ''"
              @update:model-value="(v: string) => updateConfig('id_field', v)"
              placeholder="id"
              class="h-8 text-sm font-mono"
            />
          </div>
          <div class="space-y-1.5">
            <div class="flex items-center gap-1">
              <Label class="text-xs">Second Action Title</Label>
              <ItemTextHint />
            </div>
            <Input
              :model-value="config.title_field_2 || ''"
              @update:model-value="(v: string) => updateConfig('title_field_2', v)"
              placeholder="View details"
              class="h-8 text-sm"
            />
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">Second ID field</Label>
            <Input
              :model-value="config.id_field_2 || ''"
              @update:model-value="(v: string) => updateConfig('id_field_2', v)"
              placeholder="action_id"
              class="h-8 text-sm font-mono"
            />
          </div>
        </template>
        <p class="text-[10px] text-muted-foreground">Cards are built from this array when the flow runs. Connect a single next step from the node.</p>
      </div>

      <!-- Input — buttons always expect a button selection; surface this
           for visual consistency with text nodes. -->
      <div class="pt-2 border-t space-y-1.5">
        <Label class="text-xs">Expected response</Label>
        <Select model-value="button" disabled>
          <SelectTrigger class="h-8 text-sm"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="button">Selection (buttons)</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div class="space-y-1.5">
        <Label class="text-xs">Store response as (optional)</Label>
        <Input
          :model-value="config.store_as || ''"
          @update:model-value="(v: string) => updateConfig('store_as', v)"
          placeholder="variable_name"
          class="h-8 text-sm font-mono"
        />
        <p class="text-[10px] text-muted-foreground">Saves the tapped button's title into this variable so later nodes can reference it.</p>
      </div>

      <div class="space-y-1.5">
        <div class="flex items-center justify-between">
          <Label class="text-xs">Save selection fields</Label>
          <Button variant="outline" size="sm" class="h-6 text-xs" @click="selectionRows?.add()">
            <Plus class="h-3 w-3 mr-1" /> Add field
          </Button>
        </div>
        <p class="text-[10px] text-muted-foreground">Maps a field from the tapped row into a session variable. id and title are the tapped quick reply. Other names are the card body, media_url, or a source field such as name.</p>
        <KeyValueRows
          ref="selectionRows"
          :model-value="config.selection_mapping || {}"
          key-placeholder="selected_item_id"
          value-placeholder="id"
          mono
          @update:model-value="(v) => updateConfig('selection_mapping', v)"
        />
      </div>

    </template>

    <!-- api_call -->
    <template v-if="node.type === 'api_call'">
      <div class="space-y-1.5">
        <Label class="text-xs">URL</Label>
        <Input
          :model-value="config.url || ''"
          @update:model-value="(v: string) => updateConfig('url', v)"
          placeholder="https://api.example.com/endpoint"
          class="h-8 text-xs font-mono"
        />
      </div>
      <div class="space-y-1.5">
        <Label class="text-xs">Method</Label>
        <Select :model-value="config.method || 'GET'" @update:model-value="(v: any) => updateConfig('method', v)">
          <SelectTrigger class="h-8 text-sm"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="GET">GET</SelectItem>
            <SelectItem value="POST">POST</SelectItem>
            <SelectItem value="PUT">PUT</SelectItem>
            <SelectItem value="PATCH">PATCH</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div class="space-y-1.5">
        <div class="flex items-center justify-between">
          <Label class="text-xs">Headers</Label>
          <Button variant="outline" size="sm" class="h-6 text-xs" @click="headerRows?.add()">
            <Plus class="h-3 w-3 mr-1" /> Add
          </Button>
        </div>
        <KeyValueRows
          ref="headerRows"
          :model-value="config.headers || {}"
          key-placeholder="Key"
          value-placeholder="Value"
          @update:model-value="(v) => updateConfig('headers', v)"
        />
      </div>
      <div class="space-y-1.5">
        <Label class="text-xs">Body</Label>
        <Textarea
          :model-value="config.body || ''"
          @update:model-value="(v: string) => updateConfig('body', v)"
          placeholder='{"phone": "{{phone_number}}"}'
          class="min-h-[60px] text-xs font-mono"
        />
      </div>
      <div class="space-y-1.5">
        <div class="flex items-center justify-between">
          <Label class="text-xs">Response mapping</Label>
          <Button variant="outline" size="sm" class="h-6 text-xs" @click="responseRows?.add()">
            <Plus class="h-3 w-3 mr-1" /> Add
          </Button>
        </div>
        <p class="text-[10px] text-muted-foreground">Map JSON paths into session variables (e.g. <code>data.user.name</code>).</p>
        <KeyValueRows
          ref="responseRows"
          :model-value="config.response_mapping || {}"
          key-placeholder="var_name"
          value-placeholder="path.to.field"
          mono
          @update:model-value="(v) => updateConfig('response_mapping', v)"
        />
      </div>
      <div class="space-y-1.5">
        <Label class="text-xs">Message template (optional)</Label>
        <Textarea
          :model-value="config.message_template || ''"
          @update:model-value="(v: string) => updateConfig('message_template', v)"
          placeholder="Hello {{user_name}}!"
          class="min-h-[50px] text-xs"
        />
        <p class="text-[10px] text-muted-foreground">Sent on 2xx response after mappings are applied.</p>
      </div>
    </template>

    <!-- tiqr_store_api -->
    <template v-if="node.type === 'tiqr_store_api'">
      <div class="space-y-1.5">
        <Label class="text-xs">API type</Label>
        <Select :model-value="tiqrApiType" @update:model-value="(v: any) => updateTiqrApiType(String(v))">
          <SelectTrigger class="h-8 text-sm"><SelectValue>{{ tiqrApiType === 'rest' ? 'REST' : 'MCP' }}</SelectValue></SelectTrigger>
          <SelectContent>
            <SelectItem value="mcp">MCP</SelectItem>
            <SelectItem value="rest">REST</SelectItem>
          </SelectContent>
        </Select>
        <p class="text-[10px] text-muted-foreground">
          <template v-if="tiqrApiType === 'rest'">
            Uses Commerce REST Endpoint URL + Store ID from AI settings → Commerce.
          </template>
          <template v-else>
            Uses Commerce MCP URL, MCP API key, and Store ID from AI settings → Commerce.
          </template>
        </p>
      </div>
      <div class="space-y-1.5">
        <Label class="text-xs">Operation</Label>
        <Select :model-value="config.operation || 'list_products'" @update:model-value="(v: any) => updateConfig('operation', v)">
          <SelectTrigger class="h-8 text-sm"><SelectValue>{{ tiqrOperation?.label || 'Select operation' }}</SelectValue></SelectTrigger>
          <SelectContent>
            <SelectItem v-for="op in tiqrOperations" :key="op.value" :value="op.value">{{ op.label }}</SelectItem>
          </SelectContent>
        </Select>
        <p class="text-[10px] text-muted-foreground">Guest checkout uses create order; JWT cart is not available here.</p>
      </div>
      <div v-for="field in (tiqrOperation?.fields || [])" :key="field.key" class="space-y-1.5">
        <Label class="text-xs">{{ field.label }}<span v-if="field.required" class="text-destructive"> *</span></Label>
        <Textarea
          v-if="field.multiline"
          :model-value="(config.params || {})[field.key] || ''"
          @update:model-value="(v: string) => updateParam(field.key, v)"
          :placeholder="field.placeholder"
          class="min-h-[60px] text-xs font-mono"
        />
        <Input
          v-else
          :model-value="(config.params || {})[field.key] || ''"
          @update:model-value="(v: string) => updateParam(field.key, v)"
          :placeholder="field.placeholder"
          class="h-8 text-xs font-mono"
        />
        <p v-if="field.hint" class="text-[10px] text-muted-foreground">{{ field.hint }}</p>
      </div>
      <div class="space-y-1.5">
        <div class="flex items-center justify-between">
          <Label class="text-xs">Response mapping</Label>
          <Button variant="outline" size="sm" class="h-6 text-xs" @click="responseRows?.add()">
            <Plus class="h-3 w-3 mr-1" /> Add
          </Button>
        </div>
        <p class="text-[10px] text-muted-foreground">Map JSON paths into session variables (e.g. <code>products[0].name</code>).</p>
        <KeyValueRows
          ref="responseRows"
          :model-value="config.response_mapping || {}"
          key-placeholder="var_name"
          value-placeholder="path.to.field"
          mono
          @update:model-value="(v) => updateConfig('response_mapping', v)"
        />
      </div>
      <div class="space-y-1.5">
        <Label class="text-xs">Message template (optional)</Label>
        <Textarea
          :model-value="config.message_template || ''"
          @update:model-value="(v: string) => updateConfig('message_template', v)"
          placeholder="Found {{product_name}}"
          class="min-h-[50px] text-xs"
        />
        <p class="text-[10px] text-muted-foreground">Sent on success after mappings are applied. Fields support <code v-pre>{{variable}}</code> templates.</p>
      </div>
    </template>

    <!-- set_variable -->
    <template v-if="node.type === 'set_variable'">
      <div class="space-y-1.5">
        <div class="flex items-center justify-between">
          <Label class="text-xs">Assignments</Label>
          <Button variant="outline" size="sm" class="h-6 text-xs" @click="addAssignment">
            <Plus class="h-3 w-3 mr-1" /> Add
          </Button>
        </div>
        <div v-for="(row, idx) in setAssignments" :key="idx" class="space-y-1.5 rounded-md border p-2">
          <div class="flex items-center gap-1.5">
            <Input
              :model-value="row.name"
              @update:model-value="(v: string) => updateAssignment(Number(idx), { name: v })"
              placeholder="name"
              class="h-8 text-xs font-mono"
            />
            <Button variant="ghost" size="icon" class="h-8 w-8 shrink-0" @click="removeAssignment(Number(idx))">
              <Trash2 class="h-3.5 w-3.5 text-destructive" />
            </Button>
          </div>
          <div class="flex items-center gap-1.5">
            <Select :model-value="row.op" @update:model-value="(v: any) => updateAssignment(Number(idx), { op: v === 'append' ? 'append' : 'set' })">
              <SelectTrigger class="h-8 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="set">Set</SelectItem>
                <SelectItem value="append">Append to array</SelectItem>
              </SelectContent>
            </Select>
            <Select :model-value="row.value_type" @update:model-value="(v: any) => updateAssignment(Number(idx), { value_type: v === 'json' ? 'json' : 'expression' })">
              <SelectTrigger class="h-8 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="expression">Expression</SelectItem>
                <SelectItem value="json">JSON</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <Textarea
            v-if="row.value_type === 'json'"
            :model-value="row.value"
            @update:model-value="(v: string) => updateAssignment(Number(idx), { value: v })"
            :placeholder="jsonAssignmentPlaceholder"
            class="min-h-[88px] text-xs font-mono"
          />
          <Input
            v-else
            :model-value="row.value"
            @update:model-value="(v: string) => updateAssignment(Number(idx), { value: v })"
            placeholder="options[0].id"
            class="h-8 text-xs font-mono"
          />
        </div>
        <p class="text-[10px] text-muted-foreground">
          Rows run top to bottom. Expressions match Condition:
          <code>options[0].id</code>, <code>len(options)</code>, <code>price * quantity</code>.
          Quote string literals (<code>"premium"</code>). JSON may use <code v-pre>{{variable}}</code> placeholders.
          Append pushes one value onto the named array. Branch with a Condition node.
        </p>
      </div>
    </template>

    <!-- condition -->
    <template v-if="node.type === 'condition'">
      <div class="space-y-1.5">
        <Label class="text-xs">Expression</Label>
        <Textarea
          :model-value="config.expression || ''"
          @update:model-value="(v: string) => updateConfig('expression', v)"
          placeholder='tier == "premium" and amount > 100'
          class="min-h-[60px] text-xs font-mono"
        />
        <p class="text-[10px] text-muted-foreground">Routes via the <code>true</code> / <code>false</code> handle. Uses expr-lang syntax.</p>
      </div>
    </template>

    <!-- timing -->
    <template v-if="node.type === 'timing'">
      <div class="space-y-1.5">
        <Label class="text-xs">Schedule</Label>
        <div v-for="(entry, idx) in schedule" :key="idx" class="flex items-center gap-1.5 text-xs">
          <span class="w-12 capitalize">{{ entry.day.slice(0, 3) }}</span>
          <Switch :checked="entry.enabled" @update:checked="(v: boolean) => updateScheduleEntry(Number(idx), 'enabled', v)" />
          <Input
            v-if="entry.enabled"
            type="time"
            :model-value="entry.start_time"
            @update:model-value="(v: string) => updateScheduleEntry(Number(idx), 'start_time', v)"
            class="h-8 text-xs w-28"
          />
          <span v-if="entry.enabled" class="text-muted-foreground">-</span>
          <Input
            v-if="entry.enabled"
            type="time"
            :model-value="entry.end_time"
            @update:model-value="(v: string) => updateScheduleEntry(Number(idx), 'end_time', v)"
            class="h-8 text-xs w-28"
          />
        </div>
        <p class="text-[10px] text-muted-foreground">Routes via <code>in_hours</code> / <code>out_of_hours</code>.</p>
      </div>
    </template>

    <!-- transfer -->
    <template v-if="node.type === 'transfer'">
      <div class="space-y-1.5">
        <Label class="text-xs">Body (sent before handoff)</Label>
        <Textarea
          :model-value="config.body || ''"
          @update:model-value="(v: string) => updateConfig('body', v)"
          placeholder="Connecting you with a human..."
          class="min-h-[50px] text-xs"
        />
      </div>
      <div class="space-y-1.5">
        <Label class="text-xs">Team</Label>
        <Select :model-value="config.team_id || '_general'" @update:model-value="(v: any) => updateConfig('team_id', v)">
          <SelectTrigger class="h-8 text-sm"><SelectValue placeholder="General queue" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="_general">General queue</SelectItem>
            <SelectItem v-for="team in teamsStore.teams" :key="team.id" :value="team.id">
              {{ team.name }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div class="space-y-1.5">
        <Label class="text-xs">Notes (for agents)</Label>
        <Textarea
          :model-value="config.notes || ''"
          @update:model-value="(v: string) => updateConfig('notes', v)"
          placeholder="Customer asked about: {{topic}}"
          class="min-h-[50px] text-xs"
        />
      </div>
    </template>

    <!-- end -->
    <template v-if="node.type === 'end'">
      <div class="space-y-1.5">
        <Label class="text-xs">Final message (optional)</Label>
        <Textarea
          :model-value="config.message || ''"
          @update:model-value="(v: string) => updateConfig('message', v)"
          placeholder="Sent when the flow ends. Leave blank for silent terminal."
          class="min-h-[60px] text-xs"
        />
      </div>
    </template>

    <!-- goto_flow -->
    <template v-if="node.type === 'goto_flow'">
      <div class="space-y-1.5">
        <Label class="text-xs">Target flow</Label>
        <Select :model-value="config.flow_id || 'none'" @update:model-value="(v: any) => updateConfig('flow_id', v === 'none' ? '' : v)">
          <SelectTrigger class="h-8 text-sm"><SelectValue placeholder="Select flow" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="none">Select flow…</SelectItem>
            <SelectItem v-for="flow in gotoFlowTargets" :key="flow.id" :value="flow.id">
              {{ flow.name }}
            </SelectItem>
          </SelectContent>
        </Select>
        <p class="text-[10px] text-muted-foreground">Session variables carry forward into the target flow.</p>
      </div>
    </template>

    <!-- whatsapp_flow -->
    <template v-if="node.type === 'whatsapp_flow'">
      <div class="space-y-1.5">
        <Label class="text-xs">WhatsApp Flow ID</Label>
        <Input
          :model-value="config.flow_id || ''"
          @update:model-value="(v: string) => updateConfig('flow_id', v)"
          placeholder="Meta flow id"
          class="h-8 text-xs font-mono"
        />
      </div>
      <div class="space-y-1.5">
        <Label class="text-xs">Header</Label>
        <Input
          :model-value="config.header || ''"
          @update:model-value="(v: string) => updateConfig('header', v)"
          class="h-8 text-xs"
        />
      </div>
      <div class="space-y-1.5">
        <Label class="text-xs">Body</Label>
        <Textarea
          :model-value="config.body || ''"
          @update:model-value="(v: string) => updateConfig('body', v)"
          class="min-h-[50px] text-xs"
        />
      </div>
      <div class="space-y-1.5">
        <Label class="text-xs">CTA label</Label>
        <Input
          :model-value="config.cta || ''"
          @update:model-value="(v: string) => updateConfig('cta', v)"
          placeholder="Open form"
          class="h-8 text-xs"
        />
      </div>
    </template>

    <!-- webhook -->
    <template v-if="node.type === 'webhook'">
      <div class="space-y-1.5">
        <Label class="text-xs">URL</Label>
        <Input
          :model-value="config.url || ''"
          @update:model-value="(v: string) => updateConfig('url', v)"
          placeholder="https://example.com/hook"
          class="h-8 text-xs font-mono"
        />
      </div>
      <div class="space-y-1.5">
        <Label class="text-xs">Method</Label>
        <Select :model-value="config.method || 'POST'" @update:model-value="(v: any) => updateConfig('method', v)">
          <SelectTrigger class="h-8 text-sm"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="GET">GET</SelectItem>
            <SelectItem value="POST">POST</SelectItem>
            <SelectItem value="PUT">PUT</SelectItem>
            <SelectItem value="PATCH">PATCH</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div class="space-y-1.5">
        <div class="flex items-center justify-between">
          <Label class="text-xs">Headers</Label>
          <Button variant="outline" size="sm" class="h-6 text-xs" @click="headerRows?.add()">
            <Plus class="h-3 w-3 mr-1" /> Add
          </Button>
        </div>
        <KeyValueRows
          ref="headerRows"
          :model-value="config.headers || {}"
          key-placeholder="Key"
          value-placeholder="Value"
          @update:model-value="(v) => updateConfig('headers', v)"
        />
      </div>
      <div class="space-y-1.5">
        <Label class="text-xs">Body</Label>
        <Textarea
          :model-value="config.body || ''"
          @update:model-value="(v: string) => updateConfig('body', v)"
          class="min-h-[50px] text-xs font-mono"
        />
      </div>
    </template>

    <!-- Skip condition. Evaluated by the runner before executing the
         node; truthy → fall through via the default edge without
         sending anything.

         Hidden for nodes whose whole purpose is branching (condition /
         buttons / timing — they have no default edge) and terminal
         nodes (end / transfer / goto_flow) where there's nothing to
         skip past. -->
    <div
      v-if="!['start', 'end', 'transfer', 'goto_flow', 'condition', 'buttons', 'timing'].includes(node.type)"
      class="pt-2 border-t space-y-1.5"
    >
      <Label class="text-xs">Skip condition (optional)</Label>
      <Input
        :model-value="config.skip_condition || ''"
        @update:model-value="(v: string) => updateConfig('skip_condition', v)"
        placeholder='tier == "premium"'
        class="h-8 text-xs font-mono"
      />
      <p class="text-[10px] text-muted-foreground">Skip this node when the expression evaluates truthy — execution continues via the default edge.</p>
    </div>
  </div>
</template>
