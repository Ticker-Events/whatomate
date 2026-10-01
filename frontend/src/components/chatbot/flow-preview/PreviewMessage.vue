<script setup lang="ts">
import { computed, ref } from 'vue'
import type { SimulationMessage } from '@/types/flow-preview'
import { Bug, Braces, ChevronDown, ChevronRight, ExternalLink, Info, Sparkles } from 'lucide-vue-next'
import JsonTree from './JsonTree.vue'

const props = defineProps<{
  message: SimulationMessage
}>()

const showContext = ref(false)
const showAI = ref(false)

const formattedTime = computed(() => {
  return props.message.timestamp.toLocaleTimeString('en-US', {
    hour: 'numeric',
    minute: '2-digit',
    hour12: true
  })
})

const isBot = computed(() => props.message.type === 'bot')
const isUser = computed(() => props.message.type === 'user')
const isSystem = computed(() => props.message.type === 'system')
const isDebug = computed(() => props.message.type === 'debug')

const contextEntries = computed(() => Object.entries(props.message.context || {}))
const hasContext = computed(() => contextEntries.value.length > 0)
const hasAI = computed(() => Array.isArray(props.message.ai) && props.message.ai.length > 0)

const ctaButtons = computed(() => {
  if (props.message.interactive !== 'cta_url' || !props.message.buttons?.length) return []
  return props.message.buttons.filter((btn) => btn.type === 'url' && btn.url)
})

function formatValue(value: unknown) {
  if (value === null || value === undefined) return String(value)
  if (typeof value === 'object') return JSON.stringify(value, null, 2)
  return String(value)
}
</script>

<template>
  <!-- Bot Message -->
  <div v-if="isBot" class="flex justify-start">
    <div class="max-w-[85%]">
      <div
        class="bg-white dark:bg-[#202c33] rounded-lg rounded-tl-none p-3 shadow-[0_1px_0.5px_rgba(0,0,0,0.13)] dark:shadow-[0_1px_0.5px_rgba(0,0,0,0.5)] ring-1 ring-black/5 dark:ring-white/5"
        :class="{ 'border-l-2 border-red-400': message.isValidationError }"
      >
        <img
          v-if="message.headerImage"
          :src="message.headerImage"
          alt=""
          class="mb-2 max-h-40 w-full rounded-md object-cover"
        />
        <p v-if="message.header" class="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-1">
          {{ message.header }}
        </p>
        <p class="text-sm text-gray-800 dark:text-gray-200 whitespace-pre-wrap">
          {{ message.content }}
        </p>
        <p v-if="message.footer" class="text-[11px] text-gray-500 dark:text-gray-400 mt-1">
          {{ message.footer }}
        </p>
        <p class="text-[10px] text-gray-400 text-right mt-1">{{ formattedTime }}</p>
        <div
          v-if="ctaButtons.length"
          class="mt-2 -mx-3 -mb-3 border-t border-black/5 dark:border-white/10"
        >
          <a
            v-for="btn in ctaButtons"
            :key="btn.id"
            :href="btn.url"
            target="_blank"
            rel="noopener noreferrer"
            class="py-2.5 text-sm text-center font-medium text-[#00a884] flex items-center justify-center gap-1.5 hover:bg-gray-50 dark:hover:bg-[#2a3942] transition-colors"
          >
            <ExternalLink class="h-3.5 w-3.5" />
            {{ btn.title || 'Open' }}
          </a>
        </div>
      </div>

      <p v-if="message.stepName" class="text-[10px] text-gray-400 mt-0.5 ml-1">
        Step: {{ message.stepName }}
      </p>

      <div v-if="hasContext || hasAI" class="mt-1 ml-1 space-y-1">
        <button
          v-if="hasContext"
          type="button"
          class="flex items-center gap-1 text-[10px] text-purple-600 dark:text-purple-400 hover:underline"
          @click="showContext = !showContext"
        >
          <ChevronDown v-if="showContext" class="h-3 w-3" />
          <ChevronRight v-else class="h-3 w-3" />
          <Braces class="h-3 w-3" />
          Context ({{ contextEntries.length }})
        </button>
        <div
          v-if="showContext"
          class="text-[10px] leading-snug bg-purple-50 dark:bg-purple-950/40 text-purple-900 dark:text-purple-200 rounded p-2 overflow-x-auto max-h-40"
        >
          <JsonTree :value="message.context" />
        </div>

        <button
          v-if="hasAI"
          type="button"
          class="flex items-center gap-1 text-[10px] text-sky-600 dark:text-sky-400 hover:underline"
          @click="showAI = !showAI"
        >
          <ChevronDown v-if="showAI" class="h-3 w-3" />
          <ChevronRight v-else class="h-3 w-3" />
          <Sparkles class="h-3 w-3" />
          AI ({{ message.ai?.length }})
        </button>
        <div v-if="showAI" class="space-y-2">
          <div
            v-for="(call, idx) in message.ai"
            :key="`${call.role}-${idx}`"
            class="text-[10px] leading-snug bg-sky-50 dark:bg-sky-950/40 text-sky-900 dark:text-sky-200 rounded p-2 space-y-1"
          >
            <p class="font-medium">{{ call.role }}<span v-if="call.route"> → {{ call.route }}</span></p>
            <p v-if="call.reasoning" class="whitespace-pre-wrap">{{ call.reasoning }}</p>
            <p v-if="call.language">language: {{ call.language }}</p>
            <p v-if="call.confidence != null">confidence: {{ call.confidence }}</p>
            <p v-if="call.error" class="text-red-600 dark:text-red-400">error: {{ call.error }}</p>
            <p v-if="call.grounded != null">grounded: {{ call.grounded }}</p>
            <details v-if="call.prompt">
              <summary class="cursor-pointer">prompt</summary>
              <pre class="mt-1 whitespace-pre-wrap">{{ call.prompt }}</pre>
            </details>
            <details v-if="call.response">
              <summary class="cursor-pointer">response</summary>
              <pre class="mt-1 whitespace-pre-wrap">{{ call.response }}</pre>
            </details>
            <details v-if="call.parsed">
              <summary class="cursor-pointer">parsed</summary>
              <pre class="mt-1 whitespace-pre-wrap">{{ formatValue(call.parsed) }}</pre>
            </details>
          </div>
        </div>
      </div>
    </div>
  </div>

  <!-- User Message -->
  <div v-else-if="isUser" class="flex justify-end">
    <div class="max-w-[85%]">
      <div class="bg-[#d9fdd3] dark:bg-[#005c4b] rounded-lg rounded-tr-none p-3 shadow-[0_1px_0.5px_rgba(0,0,0,0.13)] dark:shadow-[0_1px_0.5px_rgba(0,0,0,0.5)] ring-1 ring-black/5 dark:ring-white/5">
        <p class="text-sm text-gray-800 dark:text-gray-100 whitespace-pre-wrap">{{ message.content }}</p>
        <p class="text-[10px] text-gray-500 dark:text-gray-300 text-right mt-1 flex items-center justify-end gap-1">
          {{ formattedTime }}
          <svg class="h-4 w-4 text-[#53bdeb]" viewBox="0 0 24 24" fill="currentColor">
            <path d="M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41L9 16.17z"/>
          </svg>
        </p>
      </div>
    </div>
  </div>

  <!-- System Message -->
  <div v-else-if="isSystem" class="flex justify-center">
    <div class="bg-amber-100 dark:bg-amber-900/30 text-xs text-amber-700 dark:text-amber-400 px-3 py-1.5 rounded-lg flex items-center gap-1.5">
      <Info class="h-3 w-3" />
      <span>{{ message.content }}</span>
    </div>
  </div>

  <!-- Debug Message -->
  <div v-else-if="isDebug" class="flex justify-center">
    <div class="bg-purple-100 dark:bg-purple-900/30 text-xs text-purple-700 dark:text-purple-400 px-3 py-1.5 rounded-lg max-w-[90%] space-y-1">
      <div class="flex items-center gap-1.5">
        <Bug class="h-3 w-3 flex-shrink-0" />
        <span class="break-all">{{ message.content }}</span>
      </div>
      <div v-if="hasContext || hasAI" class="space-y-1">
        <button
          v-if="hasContext"
          type="button"
          class="flex items-center gap-1 text-[10px] hover:underline"
          @click="showContext = !showContext"
        >
          <Braces class="h-3 w-3" />
          Context
        </button>
        <div v-if="showContext" class="text-[10px] max-h-32 overflow-auto">
          <JsonTree :value="message.context" />
        </div>
        <button
          v-if="hasAI"
          type="button"
          class="flex items-center gap-1 text-[10px] hover:underline"
          @click="showAI = !showAI"
        >
          <Sparkles class="h-3 w-3" />
          AI details
        </button>
        <pre v-if="showAI" class="text-[10px] whitespace-pre-wrap max-h-40 overflow-auto">{{ formatValue(message.ai) }}</pre>
      </div>
    </div>
  </div>
</template>
