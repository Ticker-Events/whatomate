<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Code2, Loader2, Play } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { accountsService, chatbotService, type CodedFlowBinding } from '@/services/api'
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

type AccountOption = {
  name: string
}

type KeywordDraft = {
  keywords: string
  enabled: boolean
  saving: boolean
}

const { t } = useI18n()
const authStore = useAuthStore()

const accounts = ref<AccountOption[]>([])
const account = ref('')
const flows = ref<CodedFlowBinding[]>([])
const drafts = ref<Record<string, KeywordDraft>>({})
const isLoading = ref(true)
const error = ref<string | null>(null)
const showPreview = ref(false)
const previewFlow = ref<CodedFlowBinding | null>(null)

const canWrite = computed(() => authStore.hasPermission('flows.chatbot', 'write'))

function setEnabled(key: string, enabled: boolean) {
  const draft = drafts.value[key]
  if (draft) draft.enabled = enabled
}

function applyFlows(rows: CodedFlowBinding[]) {
  flows.value = rows
  const next: Record<string, KeywordDraft> = {}
  for (const flow of rows) {
    next[flow.key] = {
      keywords: (flow.keywords || []).join(', '),
      enabled: flow.is_enabled,
      saving: false,
    }
  }
  drafts.value = next
}

async function loadAccounts() {
  const response = await accountsService.list()
  const body = response.data as { data?: { accounts?: AccountOption[] }; accounts?: AccountOption[] }
  accounts.value = body.data?.accounts ?? body.accounts ?? []
  if (!account.value && accounts.value[0]) {
    account.value = accounts.value[0].name
  }
}

async function loadFlows() {
  if (!account.value) {
    flows.value = []
    drafts.value = {}
    isLoading.value = false
    return
  }
  isLoading.value = true
  error.value = null
  try {
    const response = await chatbotService.listCodedFlows(account.value)
    const body = response.data as { data?: { flows?: CodedFlowBinding[] }; flows?: CodedFlowBinding[] }
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

async function save(flow: CodedFlowBinding) {
  const draft = drafts.value[flow.key]
  if (!draft || !account.value) return
  draft.saving = true
  try {
    const keywords = draft.keywords.split(',').map((keyword) => keyword.trim()).filter(Boolean)
    await chatbotService.updateCodedFlow(flow.key, account.value, {
      keywords,
      is_enabled: draft.enabled,
    })
    flow.keywords = keywords
    flow.is_enabled = draft.enabled
    draft.keywords = keywords.join(', ')
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
