<script setup lang="ts">
import { ref, watch } from 'vue'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Trash2 } from 'lucide-vue-next'

type Row = { id: number; key: string; value: string }

const props = withDefaults(defineProps<{
  modelValue?: Record<string, string>
  keyPlaceholder?: string
  valuePlaceholder?: string
  mono?: boolean
}>(), {
  modelValue: () => ({}),
  keyPlaceholder: '',
  valuePlaceholder: '',
  mono: false,
})

const emit = defineEmits<{
  'update:modelValue': [value: Record<string, string>]
}>()

let nextId = 1
const rows = ref<Row[]>([])

function asMap(value: Record<string, string> | undefined): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [key, field] of Object.entries(value || {})) {
    out[key] = field == null ? '' : String(field)
  }
  return out
}

function rowsToMap(list: Row[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const row of list) out[row.key] = row.value
  return out
}

function signature(map: Record<string, string>): string {
  return Object.entries(map).map(([key, value]) => `${key}\0${value}`).join('\n')
}

function syncFrom(map: Record<string, string>) {
  rows.value = Object.entries(map).map(([key, value]) => ({
    id: nextId++,
    key,
    value,
  }))
}

watch(
  () => props.modelValue,
  (value) => {
    const incoming = asMap(value)
    if (signature(rowsToMap(rows.value)) === signature(incoming)) return
    syncFrom(incoming)
  },
  { immediate: true, deep: true },
)

function emitRows() {
  emit('update:modelValue', rowsToMap(rows.value))
}

function updateKey(row: Row, value: string) {
  row.key = value
  emitRows()
}

function updateValue(row: Row, value: string) {
  row.value = value
  emitRows()
}

function removeRow(id: number) {
  rows.value = rows.value.filter((row) => row.id !== id)
  emitRows()
}

function add() {
  rows.value.push({ id: nextId++, key: '', value: '' })
  emitRows()
}

defineExpose({ add })
</script>

<template>
  <div v-for="row in rows" :key="row.id" class="flex items-center gap-1">
    <Input
      :model-value="row.key"
      :placeholder="keyPlaceholder"
      :class="['h-7 text-xs flex-1', mono ? 'font-mono' : '']"
      @update:model-value="(v: string) => updateKey(row, v)"
    />
    <Input
      :model-value="row.value"
      :placeholder="valuePlaceholder"
      :class="['h-7 text-xs flex-1', mono ? 'font-mono' : '']"
      @update:model-value="(v: string) => updateValue(row, v)"
    />
    <Button variant="ghost" size="icon" class="h-6 w-6" @click="removeRow(row.id)">
      <Trash2 class="h-3 w-3 text-destructive" />
    </Button>
  </div>
</template>
