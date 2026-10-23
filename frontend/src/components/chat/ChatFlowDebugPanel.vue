<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from 'vue-sonner'
import { Bug, GitBranch, Loader2, X } from 'lucide-vue-next'
import {
  contactsService,
  type ContactCommerceSession,
  type ContactSessionArchive,
  type SessionArchiveTurn,
} from '@/services/api'
import type { Message } from '@/stores/contacts'
import { getErrorMessage } from '@/lib/api-utils'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import FlowDebugSections from '@/components/chatbot/flow-preview/FlowDebugSections.vue'

type PathEntry = {
  node?: string
  type?: string
  label?: string
  outcome?: string
  expression?: string
  skip_condition?: boolean
  action?: string
  flow?: string
  flow_id?: string
}

const props = defineProps<{
  contactId: string
  messages: Message[]
  selectedMessageId?: string | null
}>()

const emit = defineEmits<{
  close: []
  'update:matchedMessageIds': [ids: string[]]
  'update:selectedMessageId': [id: string | null]
}>()

const { t } = useI18n()

const DEBUG_WIDTH = 360
const loadingSessions = ref(false)
const loadingArchive = ref(false)
const archiveSessions = ref<ContactCommerceSession[]>([])
const selectedSessionId = ref('')
const archive = ref<ContactSessionArchive | null>(null)
const selectedTurnIndex = ref(0)

const selectedTurn = computed<SessionArchiveTurn | null>(() => {
  const turns = archive.value?.turns || []
  if (!turns.length) return null
  const idx = Math.min(Math.max(selectedTurnIndex.value, 0), turns.length - 1)
  return turns[idx] || null
})

const pathEntries = computed<PathEntry[]>(() => {
  const raw = selectedTurn.value?.context?.__path__
  if (!Array.isArray(raw)) return []
  return raw.filter((item): item is PathEntry => !!item && typeof item === 'object') as PathEntry[]
})

const flowName = computed(() => {
  const ctx = selectedTurn.value?.context || {}
  const fromCtx = typeof ctx._flow_name === 'string' ? ctx._flow_name.trim() : ''
  return fromCtx || archive.value?.flow_key || '—'
})

function normalizeText(value: unknown): string {
  if (value == null) return ''
  return String(value).replace(/\s+/g, ' ').trim().toLowerCase()
}

function messageText(message: Message): string {
  const content = message.content
  if (typeof content === 'string') return content
  if (message.interactive_data?.body) return message.interactive_data.body
  if (content && typeof content === 'object') {
    const body = (content as { body?: string; text?: string }).body
      || (content as { text?: string }).text
    if (body) return body
  }
  return ''
}

/** Map chat message id → archive turn index (best-effort content match). */
const messageTurnMap = computed(() => {
  const map = new Map<string, number>()
  const turns = archive.value?.turns || []
  if (!turns.length || !props.messages.length) return map

  const usedMessageIds = new Set<string>()
  for (let turnIdx = 0; turnIdx < turns.length; turnIdx++) {
    const turn = turns[turnIdx]
    const candidates: Array<{ text: string; prefer: 'incoming' | 'outgoing' }> = []
    const inboundText = normalizeText(turn.inbound?.text)
    if (inboundText) candidates.push({ text: inboundText, prefer: 'incoming' })
    for (const msg of turn.messages || []) {
      const text = normalizeText(msg.content)
      if (text) candidates.push({ text, prefer: 'outgoing' })
    }
    for (const candidate of candidates) {
      const match = props.messages.find((m) => {
        if (usedMessageIds.has(m.id)) return false
        if (m.direction !== candidate.prefer) return false
        return normalizeText(messageText(m)) === candidate.text
      })
      if (match) {
        map.set(match.id, turnIdx)
        usedMessageIds.add(match.id)
      }
    }
  }
  return map
})

const matchedMessageIds = computed(() => Array.from(messageTurnMap.value.keys()))

watch(matchedMessageIds, (ids) => {
  emit('update:matchedMessageIds', ids)
}, { immediate: true })

watch(
  () => props.selectedMessageId,
  (messageId) => {
    if (!messageId) return
    const turnIdx = messageTurnMap.value.get(messageId)
    if (turnIdx != null) selectedTurnIndex.value = turnIdx
  },
)

async function loadSessions() {
  loadingSessions.value = true
  archiveSessions.value = []
  selectedSessionId.value = ''
  archive.value = null
  try {
    const response = await contactsService.getCommerceSessions(props.contactId)
    const data = response.data.data || response.data
    const sessions = (data?.sessions || []) as ContactCommerceSession[]
    archiveSessions.value = sessions.filter((s) => s.archive_available)
    if (archiveSessions.value.length > 0) {
      selectedSessionId.value = String(archiveSessions.value[0].session_id)
    }
  } catch (e) {
    toast.error(getErrorMessage(e, t('chat.debugLoadSessionsFailed')))
  } finally {
    loadingSessions.value = false
  }
}

async function loadArchive(sessionId: string) {
  if (!sessionId) return
  loadingArchive.value = true
  archive.value = null
  selectedTurnIndex.value = 0
  try {
    const response = await contactsService.getSessionArchive(props.contactId, sessionId)
    const data = (response.data.data || response.data) as ContactSessionArchive
    archive.value = data
    if ((data.turns || []).length > 0) {
      selectedTurnIndex.value = data.turns.length - 1
    }
  } catch (e) {
    toast.error(getErrorMessage(e, t('chat.debugLoadArchiveFailed')))
  } finally {
    loadingArchive.value = false
  }
}

watch(
  () => props.contactId,
  () => {
    loadSessions()
  },
  { immediate: true },
)

watch(selectedSessionId, (id, prev) => {
  if (!id || id === prev) return
  loadArchive(id)
})

function selectTurn(index: number) {
  selectedTurnIndex.value = index
  const turns = archive.value?.turns || []
  const turn = turns[index]
  if (!turn) {
    emit('update:selectedMessageId', null)
    return
  }
  for (const [messageId, turnIdx] of messageTurnMap.value.entries()) {
    if (turnIdx === index) {
      emit('update:selectedMessageId', messageId)
      return
    }
  }
  emit('update:selectedMessageId', null)
}

function formatTurnLabel(turn: SessionArchiveTurn, index: number) {
  const step = turn.step || t('chat.debugTurn', { n: index + 1 })
  const inbound = turn.inbound?.text ? String(turn.inbound.text) : ''
  if (inbound) return `${step} · ${inbound.slice(0, 40)}`
  return step
}

function inboundSummary(turn: SessionArchiveTurn | null) {
  if (!turn?.inbound) return ''
  const parts: string[] = []
  if (turn.inbound.text) parts.push(String(turn.inbound.text))
  if (turn.inbound.button_id) parts.push(`button:${turn.inbound.button_id}`)
  return parts.join(' · ')
}

function pathTitle(entry: PathEntry) {
  if (entry.action === 'goto_flow') {
    return entry.flow || entry.flow_id || t('chat.debugGotoFlow')
  }
  return entry.label || entry.node || entry.type || '—'
}

function isConditionEntry(entry: PathEntry) {
  return entry.type === 'condition' || entry.skip_condition === true || !!entry.expression
}

function outcomeClass(outcome?: string) {
  if (outcome === 'true' || outcome === 'http:2xx' || outcome?.startsWith('button:')) {
    return 'text-emerald-600 dark:text-emerald-400'
  }
  if (outcome === 'false' || outcome === 'http:non2xx' || outcome === 'skipped') {
    return 'text-amber-600 dark:text-amber-400'
  }
  return 'text-muted-foreground'
}
</script>

<template>
  <div
    class="flex flex-col bg-card h-full min-h-0 border-l shrink-0 overflow-hidden"
    :style="{ width: `${DEBUG_WIDTH}px` }"
  >
    <div class="h-12 shrink-0 px-3 border-b flex items-center justify-between gap-2">
      <div class="flex items-center gap-2 min-w-0">
        <Bug class="h-4 w-4 shrink-0 text-muted-foreground" />
        <h3 class="font-medium text-sm truncate">{{ $t('chat.debugTitle') }}</h3>
      </div>
      <Button variant="ghost" size="icon" class="h-8 w-8 shrink-0" @click="emit('close')">
        <X class="h-4 w-4" />
      </Button>
    </div>

    <ScrollArea class="flex-1 min-h-0">
      <div class="p-3 space-y-4">
        <div v-if="loadingSessions" class="flex items-center gap-2 text-sm text-muted-foreground py-6 justify-center">
          <Loader2 class="h-4 w-4 animate-spin" />
          {{ $t('chat.debugLoading') }}
        </div>

        <template v-else-if="archiveSessions.length === 0">
          <p class="text-sm text-muted-foreground py-4">
            {{ $t('chat.debugNoArchive') }}
          </p>
        </template>

        <template v-else>
          <div class="space-y-1.5">
            <label class="text-xs font-medium text-muted-foreground">{{ $t('chat.debugSession') }}</label>
            <select
              v-model="selectedSessionId"
              class="w-full rounded-md border bg-background px-2 py-1.5 text-xs"
              :disabled="loadingArchive"
            >
              <option
                v-for="session in archiveSessions"
                :key="session.session_id"
                :value="String(session.session_id)"
              >
                {{ session.flow_key || $t('chat.debugUntitledFlow') }}
                · {{ session.status }}
                · {{ new Date(session.started_at).toLocaleString() }}
              </option>
            </select>
          </div>

          <div v-if="loadingArchive" class="flex items-center gap-2 text-sm text-muted-foreground py-4 justify-center">
            <Loader2 class="h-4 w-4 animate-spin" />
            {{ $t('chat.debugLoading') }}
          </div>

          <template v-else-if="archive">
            <div class="rounded-md border bg-muted/30 px-2 py-1.5 text-[11px] space-y-0.5">
              <p>
                <span class="text-muted-foreground">{{ $t('chat.debugFlow') }}:</span>
                {{ flowName }}
              </p>
              <p>
                <span class="text-muted-foreground">{{ $t('chat.debugTurns') }}:</span>
                {{ archive.turn_count }}
              </p>
              <p v-if="archive.status">
                <span class="text-muted-foreground">{{ $t('chat.debugStatus') }}:</span>
                {{ archive.status }}
              </p>
            </div>

            <div v-if="!(archive.turns || []).length" class="text-sm text-muted-foreground">
              {{ $t('chat.debugNoTurns') }}
            </div>

            <div v-else class="space-y-2">
              <p class="text-xs font-medium text-muted-foreground">{{ $t('chat.debugTurnList') }}</p>
              <div class="space-y-1 max-h-40 overflow-auto">
                <button
                  v-for="(turn, index) in archive.turns"
                  :key="index"
                  type="button"
                  class="w-full text-left rounded-md px-2 py-1.5 text-[11px] border transition-colors"
                  :class="index === selectedTurnIndex
                    ? 'bg-primary/10 border-primary/30 text-foreground'
                    : 'bg-background hover:bg-muted/50 text-muted-foreground'"
                  @click="selectTurn(index)"
                >
                  <span class="font-medium">#{{ index + 1 }}</span>
                  {{ formatTurnLabel(turn, index) }}
                  <span
                    v-if="(turn.api_calls || []).length"
                    class="ml-1 text-[10px] opacity-70"
                  >
                    · {{ (turn.api_calls || []).length }} API
                  </span>
                </button>
              </div>

              <div v-if="selectedTurn" class="space-y-3 border-t pt-3">
                <div class="text-[11px] space-y-1">
                  <p>
                    <span class="text-muted-foreground">{{ $t('chat.debugStep') }}:</span>
                    {{ selectedTurn.step || '—' }}
                  </p>
                  <p v-if="inboundSummary(selectedTurn)">
                    <span class="text-muted-foreground">{{ $t('chat.debugInbound') }}:</span>
                    {{ inboundSummary(selectedTurn) }}
                  </p>
                </div>

                <div class="space-y-2">
                  <div class="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                    <GitBranch class="h-3.5 w-3.5" />
                    {{ $t('chat.debugPath') }}
                    <span class="ml-auto text-[10px]">{{ pathEntries.length }}</span>
                  </div>
                  <div v-if="pathEntries.length === 0" class="text-[11px] text-muted-foreground">
                    {{ $t('chat.debugPathEmpty') }}
                  </div>
                  <ol v-else class="space-y-1.5 max-h-64 overflow-auto">
                    <li
                      v-for="(entry, idx) in pathEntries"
                      :key="`${entry.node || entry.action}-${idx}`"
                      class="rounded-md border px-2 py-1.5 text-[11px]"
                      :class="isConditionEntry(entry)
                        ? 'border-sky-500/30 bg-sky-50/60 dark:bg-sky-950/20'
                        : 'bg-muted/30'"
                    >
                      <div class="flex items-start justify-between gap-2">
                        <div class="min-w-0">
                          <p class="font-medium truncate">
                            {{ pathTitle(entry) }}
                            <span v-if="entry.type" class="font-normal text-muted-foreground">
                              · {{ entry.type }}
                            </span>
                          </p>
                          <p
                            v-if="entry.expression"
                            class="mt-0.5 font-mono text-[10px] text-muted-foreground break-all"
                          >
                            {{ entry.expression }}
                          </p>
                        </div>
                        <span
                          v-if="entry.outcome != null && entry.outcome !== ''"
                          class="shrink-0 font-mono text-[10px]"
                          :class="outcomeClass(entry.outcome)"
                        >
                          {{ entry.outcome }}
                        </span>
                      </div>
                    </li>
                  </ol>
                </div>

                <FlowDebugSections
                  :context="selectedTurn.context || null"
                  :ai-calls="selectedTurn.ai_calls || []"
                  :api-calls="selectedTurn.api_calls || []"
                  :default-context-open="false"
                  :default-ai-open="false"
                  :default-api-open="true"
                />
              </div>
            </div>
          </template>
        </template>
      </div>
    </ScrollArea>
  </div>
</template>
