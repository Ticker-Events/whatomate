<script setup lang="ts">
import { computed } from 'vue'
import { Variable } from 'lucide-vue-next'
import BaseNode from '@/components/calling/nodes/BaseNode.vue'

defineOptions({ inheritAttrs: false })

const props = defineProps<{ data: any }>()

type Assignment = { name: string; value: string }

function assignmentRows(set: unknown): Assignment[] {
  if (Array.isArray(set)) {
    return set.flatMap((row) => {
      if (!row || typeof row !== 'object') return []
      const name = typeof row.name === 'string' ? row.name : ''
      const value = row.value == null ? '' : String(row.value)
      if (!name && !value) return []
      return [{ name, value }]
    })
  }
  if (set && typeof set === 'object') {
    return Object.entries(set as Record<string, unknown>).flatMap(([name, raw]) => {
      const value = raw == null ? '' : String(raw)
      if (!name && !value) return []
      return [{ name, value }]
    })
  }
  return []
}

const summary = computed(() => {
  const rows = assignmentRows(props.data?.config?.set)
  if (rows.length === 0) return 'No assignments'
  if (rows.length === 1) {
    const text = `${rows[0].name || '?'} = ${rows[0].value || '?'}`
    return text.length > 60 ? text.slice(0, 60) + '…' : text
  }
  return `${rows.length} assignments`
})
</script>

<template>
  <BaseNode
    :label="data?.label || 'Assign'"
    header-class="bg-violet-600"
    :has-input="!data?.isEntryNode"
  >
    <template #icon><Variable class="w-4 h-4" /></template>
    <p class="truncate" :title="summary">{{ summary }}</p>
  </BaseNode>
</template>
