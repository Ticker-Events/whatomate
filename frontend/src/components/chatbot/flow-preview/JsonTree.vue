<script setup lang="ts">
import { computed, ref } from 'vue'
import { ChevronDown, ChevronRight } from 'lucide-vue-next'

const props = withDefaults(defineProps<{
  value: unknown
  label?: string
  depth?: number
}>(), {
  depth: 0,
})

// Nested objects and arrays stay collapsed until the user expands them.
const expanded = ref(false)

const kind = computed(() => {
  if (props.value === null) return 'null'
  if (Array.isArray(props.value)) return 'array'
  return typeof props.value
})

const isExpandable = computed(() => kind.value === 'object' || kind.value === 'array')

const entries = computed(() => {
  if (kind.value === 'array') {
    return (props.value as unknown[]).map((item, index) => [String(index), item] as const)
  }
  if (kind.value === 'object' && props.value) {
    return Object.entries(props.value as Record<string, unknown>)
  }
  return [] as Array<readonly [string, unknown]>
})

const summary = computed(() => {
  if (kind.value === 'array') {
    const n = (props.value as unknown[]).length
    return n === 0 ? '[]' : `Array(${n})`
  }
  if (kind.value === 'object') {
    const n = entries.value.length
    return n === 0 ? '{}' : `{${n}}`
  }
  return ''
})

function formatPrimitive(value: unknown) {
  if (value === null) return 'null'
  if (typeof value === 'string') return JSON.stringify(value)
  return String(value)
}

function primitiveClass(value: unknown) {
  if (value === null) return 'text-muted-foreground'
  if (typeof value === 'string') return 'text-emerald-700 dark:text-emerald-400'
  if (typeof value === 'number') return 'text-amber-700 dark:text-amber-400'
  if (typeof value === 'boolean') return 'text-sky-700 dark:text-sky-400'
  return 'text-muted-foreground'
}
</script>

<template>
  <div class="font-mono text-[11px] leading-snug">
    <button
      v-if="isExpandable"
      type="button"
      class="flex items-start gap-0.5 text-left w-full hover:bg-muted/50 rounded px-0.5 -mx-0.5"
      @click="expanded = !expanded"
    >
      <ChevronDown v-if="expanded" class="h-3 w-3 mt-0.5 shrink-0 text-muted-foreground" />
      <ChevronRight v-else class="h-3 w-3 mt-0.5 shrink-0 text-muted-foreground" />
      <span v-if="label != null" class="text-purple-600 dark:text-purple-400 shrink-0">{{ label }}:</span>
      <span class="text-muted-foreground ml-1">{{ summary }}</span>
    </button>
    <div v-else class="flex items-start gap-1 px-0.5">
      <span class="w-3 shrink-0" />
      <span v-if="label != null" class="text-purple-600 dark:text-purple-400 shrink-0">{{ label }}:</span>
      <span :class="primitiveClass(value)" class="break-all">{{ formatPrimitive(value) }}</span>
    </div>
    <div v-if="isExpandable && expanded" class="ml-3 border-l border-border/60 pl-2 mt-0.5 space-y-0.5">
      <div v-if="entries.length === 0" class="text-muted-foreground px-0.5">
        {{ kind === 'array' ? '[]' : '{}' }}
      </div>
      <JsonTree
        v-for="[childLabel, childValue] in entries"
        :key="childLabel"
        :label="childLabel"
        :value="childValue"
        :depth="depth + 1"
      />
    </div>
  </div>
</template>
