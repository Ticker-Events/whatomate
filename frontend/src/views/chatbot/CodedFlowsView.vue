<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Code2, Loader2, Play } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { accountsService, chatbotService, flowsService, type CodedFlowBinding } from '@/services/api'
import { useAuthStore } from '@/stores/auth'
import { getErrorMessage } from '@/lib/api-utils'
import { PageHeader, ErrorState } from '@/components/shared'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import CodedFlowPreview from '@/components/chatbot/flow-preview/CodedFlowPreview.vue'

const TIQR_ECOMMERCE_KEY = 'tiqr_ecommerce'
const USE_DEFAULT = '__default__'

type AccountOption = {
  name: string
}

type WhatsAppFlowOption = {
  id: string
  name: string
  meta_flow_id: string
  status?: string
}

type KeywordDraft = {
  keywords: string
  enabled: boolean
  pickupFlowId: string
  deliveryFlowId: string
  saving: boolean
}

const { t } = useI18n()
const authStore = useAuthStore()

const accounts = ref<AccountOption[]>([])
const account = ref('')
const flows = ref<CodedFlowBinding[]>([])
const drafts = ref<Record<string, KeywordDraft>>({})
const whatsappFlows = ref<WhatsAppFlowOption[]>([])
const isLoading = ref(true)
const error = ref<string | null>(null)
const showPreview = ref(false)
const previewFlow = ref<CodedFlowBinding | null>(null)

const canWrite = computed(() => authStore.hasPermission('flows.chatbot', 'write'))

const publishedFlows = computed(() =>
  whatsappFlows.value.filter((f) => f.meta_flow_id && f.status?.toUpperCase() === 'PUBLISHED'),
)

function setEnabled(key: string, enabled: boolean) {
  const draft = drafts.value[key]
  if (draft) draft.enabled = enabled
}

function draftPickupValue(flow: CodedFlowBinding): string {
  return flow.stored_pickup_flow_id || USE_DEFAULT
}

function draftDeliveryValue(flow: CodedFlowBinding): string {
  return flow.stored_delivery_flow_id || USE_DEFAULT
}

function applyFlows(rows: CodedFlowBinding[]) {
  flows.value = rows
  const next: Record<string, KeywordDraft> = {}
  for (const flow of rows) {
    next[flow.key] = {
      keywords: (flow.keywords || []).join(', '),
      enabled: flow.is_enabled,
      pickupFlowId: draftPickupValue(flow),
      deliveryFlowId: draftDeliveryValue(flow),
      saving: false,
    }
  }
  drafts.value = next
}

function flowOptionsFor(selectedId: string): WhatsAppFlowOption[] {
  const options = [...publishedFlows.value]
  const seen = new Set(options.map((f) => f.meta_flow_id))
  if (selectedId && selectedId !== USE_DEFAULT && !seen.has(selectedId)) {
    options.push({ id: selectedId, name: selectedId, meta_flow_id: selectedId })
  }
  return options
}

function pickupOptions(flow: CodedFlowBinding): WhatsAppFlowOption[] {
  const draft = drafts.value[flow.key]
  return flowOptionsFor(draft?.pickupFlowId || '')
}

function deliveryOptions(flow: CodedFlowBinding): WhatsAppFlowOption[] {
  const draft = drafts.value[flow.key]
  return flowOptionsFor(draft?.deliveryFlowId || '')
}

async function loadAccounts() {
  const response = await accountsService.list()
  const body = response.data as { data?: { accounts?: AccountOption[] }; accounts?: AccountOption[] }
  accounts.value = body.data?.accounts ?? body.accounts ?? []
  if (!account.value && accounts.value[0]) {
    account.value = accounts.value[0].name
  }
}

async function loadWhatsAppFlows() {
  if (!account.value) {
    whatsappFlows.value = []
    return
  }
  try {
    const response = await flowsService.list({ account: account.value, limit: 100 })
    const data = (response.data as { data?: { flows?: WhatsAppFlowOption[] }; flows?: WhatsAppFlowOption[] }).data
      || response.data as { flows?: WhatsAppFlowOption[] }
    whatsappFlows.value = (data.flows || []).filter((f) => !!f.meta_flow_id)
  } catch (err) {
    console.error('Failed to load WhatsApp flows:', err)
    whatsappFlows.value = []
  }
}

async function loadFlows() {
  if (!account.value) {
    flows.value = []
    drafts.value = {}
    whatsappFlows.value = []
    isLoading.value = false
    return
  }
  isLoading.value = true
  error.value = null
  try {
    const [codedRes] = await Promise.all([
      chatbotService.listCodedFlows(account.value),
      loadWhatsAppFlows(),
    ])
    const body = codedRes.data as { data?: { flows?: CodedFlowBinding[] }; flows?: CodedFlowBinding[] }
    const rows = body.data?.flows ?? body.flows ?? []
    applyFlows(rows)
  } catch (err) {
    console.error('Failed to load coded flows:', err)
    error.value = t('codedFlows.fetchError')
    flows.value = []
  } finally {
    isLoading.value = false
  }
}

function openPreview(flow: CodedFlowBinding) {
  previewFlow.value = flow
  showPreview.value = true
}

function resolveStoredFlowId(draftValue: string): string {
  if (!draftValue || draftValue === USE_DEFAULT) return ''
  return draftValue.trim()
}

async function save(flow: CodedFlowBinding) {
  const draft = drafts.value[flow.key]
  if (!draft || !account.value) return
  draft.saving = true
  try {
    const keywords = draft.keywords.split(',').map((keyword) => keyword.trim()).filter(Boolean)
    const payload: {
      keywords: string[]
      is_enabled: boolean
      pickup_flow_id?: string
      delivery_flow_id?: string
    } = {
      keywords,
      is_enabled: draft.enabled,
    }
    if (flow.key === TIQR_ECOMMERCE_KEY) {
      payload.pickup_flow_id = resolveStoredFlowId(draft.pickupFlowId)
      payload.delivery_flow_id = resolveStoredFlowId(draft.deliveryFlowId)
    }
    const response = await chatbotService.updateCodedFlow(flow.key, account.value, payload)
    const body = response.data as { data?: CodedFlowBinding } & CodedFlowBinding
    const saved = body.data ?? body
    flow.keywords = saved.keywords ?? keywords
    flow.is_enabled = saved.is_enabled ?? draft.enabled
    flow.pickup_flow_id = saved.pickup_flow_id
    flow.delivery_flow_id = saved.delivery_flow_id
    flow.stored_pickup_flow_id = saved.stored_pickup_flow_id
    flow.stored_delivery_flow_id = saved.stored_delivery_flow_id
    draft.keywords = (flow.keywords || []).join(', ')
    draft.enabled = flow.is_enabled
    draft.pickupFlowId = draftPickupValue(flow)
    draft.deliveryFlowId = draftDeliveryValue(flow)
    toast.success(t('codedFlows.saved'))
  } catch (err) {
    toast.error(getErrorMessage(err, t('codedFlows.saveError')))
  } finally {
    draft.saving = false
  }
}

onMounted(async () => {
  try {
    await loadAccounts()
  } catch (err) {
    console.error('Failed to load WhatsApp accounts:', err)
    accounts.value = []
  }
  if (!account.value) {
    isLoading.value = false
  }
})

watch(account, (name) => {
  showPreview.value = false
  if (name) {
    loadFlows()
  }
})

watch(showPreview, (open) => {
  if (!open) previewFlow.value = null
})
</script>

<template>
  <div class="flex flex-col h-full bg-[#0a0a0b] light:bg-gray-50">
    <PageHeader
      :title="$t('codedFlows.title')"
      :icon="Code2"
      icon-gradient="bg-gradient-to-br from-emerald-500 to-teal-600 shadow-emerald-500/20"
      back-link="/chatbot"
      :breadcrumbs="[{ label: $t('codedFlows.backToChatbot'), href: '/chatbot' }, { label: $t('nav.codedFlows') }]"
    >
      <template #actions>
        <div v-if="accounts.length" class="w-56">
          <Select v-model="account">
            <SelectTrigger>
              <SelectValue :placeholder="$t('codedFlows.selectAccount')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="item in accounts" :key="item.name" :value="item.name">
                {{ item.name }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
      </template>
    </PageHeader>

    <ScrollArea class="flex-1">
      <div class="p-6 space-y-4">
        <p class="text-sm text-white/50 light:text-gray-600">{{ $t('codedFlows.description') }}</p>
        <p v-if="!isLoading && !accounts.length" class="text-sm text-white/50 light:text-gray-600">
          {{ $t('codedFlows.noAccounts') }}
        </p>

        <ErrorState
          v-else-if="error"
          :title="$t('common.loadErrorTitle')"
          :description="error"
          :retry-label="$t('common.retry')"
          @retry="loadFlows"
        />

        <div v-else-if="isLoading" class="space-y-4">
          <Skeleton v-for="n in 2" :key="n" class="h-48 w-full rounded-xl" />
        </div>

        <p v-else-if="!flows.length" class="text-sm text-white/50 light:text-gray-600">
          {{ $t('codedFlows.noFlows') }}
        </p>

        <template v-else>
        <Card v-for="flow in flows" :key="flow.key">
          <CardHeader>
            <div class="flex items-start justify-between gap-4">
              <div>
                <CardTitle>{{ flow.name }}</CardTitle>
                <CardDescription class="mt-1">{{ flow.description }}</CardDescription>
              </div>
              <div class="flex items-center gap-2 shrink-0">
                <Switch
                  :checked="drafts[flow.key]?.enabled"
                  :disabled="!canWrite"
                  @update:checked="setEnabled(flow.key, $event)"
                />
                <span class="text-sm text-muted-foreground">
                  {{ drafts[flow.key]?.enabled ? $t('chatbotFlows.active') : $t('chatbotFlows.inactive') }}
                </span>
              </div>
            </div>
          </CardHeader>
          <CardContent class="space-y-4">
            <div>
              <p class="text-xs font-medium text-muted-foreground mb-2">{{ $t('codedFlows.steps') }}</p>
              <div class="flex flex-wrap gap-1.5">
                <Badge v-for="step in flow.steps" :key="step.name" variant="secondary" class="text-xs font-normal">
                  {{ step.label }}
                </Badge>
              </div>
            </div>
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('codedFlows.keywords') }}</Label>
              <Input
                v-if="drafts[flow.key]"
                v-model="drafts[flow.key].keywords"
                :disabled="!canWrite"
                :placeholder="$t('codedFlows.keywordsHint')"
              />
              <p class="text-xs text-muted-foreground">{{ $t('codedFlows.keywordsHint') }}</p>
            </div>

            <div v-if="flow.key === TIQR_ECOMMERCE_KEY && drafts[flow.key]" class="space-y-3 rounded-lg border p-3">
              <div>
                <p class="text-xs font-medium">{{ $t('codedFlows.customerDetailsFlows') }}</p>
                <p class="text-xs text-muted-foreground mt-0.5">{{ $t('codedFlows.customerDetailsFlowsHint') }}</p>
              </div>
              <div class="grid gap-3 sm:grid-cols-2">
                <div class="space-y-1.5">
                  <Label class="text-xs">{{ $t('codedFlows.pickupFlow') }}</Label>
                  <Select
                    v-if="publishedFlows.length || flow.pickup_flow_id"
                    :model-value="drafts[flow.key].pickupFlowId"
                    :disabled="!canWrite"
                    @update:model-value="(v: any) => { drafts[flow.key].pickupFlowId = String(v) }"
                  >
                    <SelectTrigger>
                      <SelectValue :placeholder="$t('codedFlows.selectFlow')" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem :value="USE_DEFAULT">
                        {{ $t('codedFlows.useDefaultFlow', { id: flow.pickup_flow_id || '—' }) }}
                      </SelectItem>
                      <SelectItem
                        v-for="option in pickupOptions(flow)"
                        :key="option.meta_flow_id"
                        :value="option.meta_flow_id"
                      >
                        {{ option.name }}
                      </SelectItem>
                    </SelectContent>
                  </Select>
                  <Input
                    v-else
                    :model-value="drafts[flow.key].pickupFlowId === USE_DEFAULT ? '' : drafts[flow.key].pickupFlowId"
                    :disabled="!canWrite"
                    :placeholder="$t('codedFlows.flowIdPlaceholder')"
                    @update:model-value="(v: string | number) => { drafts[flow.key].pickupFlowId = String(v || '') }"
                  />
                </div>
                <div class="space-y-1.5">
                  <Label class="text-xs">{{ $t('codedFlows.deliveryFlow') }}</Label>
                  <Select
                    v-if="publishedFlows.length || flow.delivery_flow_id"
                    :model-value="drafts[flow.key].deliveryFlowId"
                    :disabled="!canWrite"
                    @update:model-value="(v: any) => { drafts[flow.key].deliveryFlowId = String(v) }"
                  >
                    <SelectTrigger>
                      <SelectValue :placeholder="$t('codedFlows.selectFlow')" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem :value="USE_DEFAULT">
                        {{ $t('codedFlows.useDefaultFlow', { id: flow.delivery_flow_id || '—' }) }}
                      </SelectItem>
                      <SelectItem
                        v-for="option in deliveryOptions(flow)"
                        :key="option.meta_flow_id"
                        :value="option.meta_flow_id"
                      >
                        {{ option.name }}
                      </SelectItem>
                    </SelectContent>
                  </Select>
                  <Input
                    v-else
                    :model-value="drafts[flow.key].deliveryFlowId === USE_DEFAULT ? '' : drafts[flow.key].deliveryFlowId"
                    :disabled="!canWrite"
                    :placeholder="$t('codedFlows.flowIdPlaceholder')"
                    @update:model-value="(v: string | number) => { drafts[flow.key].deliveryFlowId = String(v || '') }"
                  />
                </div>
              </div>
            </div>

            <div class="flex justify-end gap-2">
              <Button size="sm" variant="outline" @click="openPreview(flow)">
                <Play class="h-4 w-4 mr-1" />
                {{ $t('codedFlows.preview') }}
              </Button>
              <Button size="sm" :disabled="!canWrite || drafts[flow.key]?.saving" @click="save(flow)">
                <Loader2 v-if="drafts[flow.key]?.saving" class="h-4 w-4 mr-2 animate-spin" />
                {{ $t('codedFlows.save') }}
              </Button>
            </div>
          </CardContent>
        </Card>
        </template>
      </div>
    </ScrollArea>

    <Dialog v-model:open="showPreview">
      <DialogContent class="left-0 top-0 flex h-[100dvh] w-full max-w-none translate-x-0 translate-y-0 flex-col gap-0 rounded-none border-0 p-0 shadow-none ring-0 light:ring-0 sm:rounded-none">
        <div class="flex shrink-0 items-center border-b px-4 py-3 pr-12">
          <DialogTitle class="truncate">{{ previewFlow?.name || $t('codedFlows.preview') }}</DialogTitle>
        </div>
        <CodedFlowPreview
          v-if="previewFlow && account"
          :key="`${account}:${previewFlow.key}`"
          class="min-h-0 flex-1"
          :flow="previewFlow"
          :account="account"
        />
      </DialogContent>
    </Dialog>
  </div>
</template>
