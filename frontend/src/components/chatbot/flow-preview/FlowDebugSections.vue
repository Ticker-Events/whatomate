<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from 'vue-sonner'
import { Braces, ChevronDown, ChevronRight, Copy, Globe, Sparkles } from 'lucide-vue-next'
import type { CodedPreviewAICall, CodedPreviewAPICall } from '@/services/api'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import JsonTree from './JsonTree.vue'

const props = withDefaults(defineProps<{
  context?: Record<string, unknown> | null
  aiCalls?: CodedPreviewAICall[]
  apiCalls?: CodedPreviewAPICall[]
  defaultContextOpen?: boolean
  defaultAiOpen?: boolean
  defaultApiOpen?: boolean
}>(), {
  context: null,
  aiCalls: () => [],
  apiCalls: () => [],
  defaultContextOpen: false,
  defaultAiOpen: true,
  defaultApiOpen: true,
})

const { t } = useI18n()

const contextExpanded = ref(props.defaultContextOpen)
const aiExpanded = ref(props.defaultAiOpen)
const apiExpanded = ref(props.defaultApiOpen)

watch(
  () => [props.defaultContextOpen, props.defaultAiOpen, props.defaultApiOpen],
  ([ctx, ai, api]) => {
    contextExpanded.value = Boolean(ctx)
    aiExpanded.value = Boolean(ai)
    apiExpanded.value = Boolean(api)
  },
)

const sessionContext = computed(() => props.context || {})
const turnAICalls = computed(() => props.aiCalls || [])
const previewAPICalls = computed(() => props.apiCalls || [])
const hasContext = computed(() => Object.keys(sessionContext.value).length > 0)

function formatDebugValue(value: unknown) {
  if (value === null || value === undefined) return String(value)
  if (typeof value === 'object') return JSON.stringify(value, null, 2)
  return String(value)
}

async function copyJSON(value: unknown, emptyMessage: string) {
  if (value == null
    || (typeof value === 'object' && Object.keys(value as object).length === 0)
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

async function copyCurl(curl: string) {
  if (!curl) {
    toast.error(t('codedFlows.previewAPIEmpty'))
    return
  }
  try {
    await navigator.clipboard.writeText(curl)
    toast.success(t('common.copiedToClipboard'))
  } catch {
    toast.error(t('common.clipboardFailed'))
  }
}
</script>

<template>
  <div class="space-y-4">
    <Collapsible v-model:open="contextExpanded">
      <div class="flex items-center gap-1">
        <CollapsibleTrigger class="flex items-center gap-2 flex-1 text-xs font-medium text-muted-foreground hover:text-foreground">
          <ChevronDown v-if="contextExpanded" class="h-3.5 w-3.5" />
          <ChevronRight v-else class="h-3.5 w-3.5" />
          <Braces class="h-3.5 w-3.5" />
          {{ $t('codedFlows.previewContext') }}
          <span class="ml-auto text-[10px]">{{ Object.keys(sessionContext).length }}</span>
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

    <Collapsible v-model:open="apiExpanded">
      <CollapsibleTrigger class="flex items-center gap-2 w-full text-xs font-medium text-muted-foreground hover:text-foreground">
        <ChevronDown v-if="apiExpanded" class="h-3.5 w-3.5" />
        <ChevronRight v-else class="h-3.5 w-3.5" />
        <Globe class="h-3.5 w-3.5" />
        {{ $t('codedFlows.previewAPI') }}
        <span class="ml-auto text-[10px]">{{ previewAPICalls.length }}</span>
      </CollapsibleTrigger>
      <CollapsibleContent class="mt-2">
        <div v-if="previewAPICalls.length === 0" class="text-[11px] text-muted-foreground">
          {{ $t('codedFlows.previewAPIEmpty') }}
        </div>
        <div v-else class="space-y-2 max-h-72 overflow-auto">
          <div
            v-for="(call, idx) in previewAPICalls"
            :key="`${call.name}-${idx}`"
            class="rounded-md border bg-muted/30 p-2 text-[11px] space-y-1"
          >
            <div class="flex items-start gap-1">
              <div class="min-w-0 flex-1 space-y-0.5">
                <p class="font-medium truncate">
                  {{ call.name }}
                  <span v-if="call.http_status" class="text-muted-foreground font-normal">{{ call.http_status }}</span>
                </p>
                <p class="font-mono text-muted-foreground break-all">{{ call.path }}</p>
                <p v-if="call.error" class="text-destructive break-all">{{ call.error }}</p>
                <details v-if="call.response != null">
                  <summary class="cursor-pointer text-muted-foreground">response</summary>
                  <pre class="mt-1 whitespace-pre-wrap break-all">{{ formatDebugValue(call.response) }}</pre>
                </details>
              </div>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                class="h-7 w-7 shrink-0"
                :disabled="!call.curl"
                :title="$t('codedFlows.previewAPICopy')"
                @click="copyCurl(call.curl)"
              >
                <Copy class="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        </div>
      </CollapsibleContent>
    </Collapsible>
  </div>
</template>
