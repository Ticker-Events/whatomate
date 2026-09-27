<script setup lang="ts">
import { computed } from 'vue'
import { MousePointerClick } from 'lucide-vue-next'
import BaseNode from '@/components/calling/nodes/BaseNode.vue'

defineOptions({ inheritAttrs: false })

const props = defineProps<{ data: any }>()

const buttons = computed(() => props.data?.config?.buttons || [])
const isList = computed(() => props.data?.config?.mode === 'list')
const isCarousel = computed(() => props.data?.config?.mode === 'carousel')
const isDynamic = computed(() => props.data?.config?.source === 'dynamic')
const cardAction = computed(() => (props.data?.config?.card_action === 'url' ? 'url' : 'reply'))

const outputHandles = computed(() => {
  // Dynamic rows are only known at runtime, so the node has one default
  // outgoing handle. Static handles stay namespaced "button:<id>" so they
  // match the graph runner's button-tap outcome. URL carousels do not send
  // a selection back, so they use that same default handle.
  if (isDynamic.value) return undefined
  if (isCarousel.value && cardAction.value === 'url') return undefined
  if (isCarousel.value) {
    const handles: { id: string; label: string; title: string }[] = []
    for (const card of buttons.value) {
      handles.push({
        id: `button:${card.id}`,
        label: card.title || '—',
        title: card.title || '—',
      })
      if (card.id_2) {
        handles.push({
          id: `button:${card.id_2}`,
          label: card.title_2 || '—',
          title: card.title_2 || '—',
        })
      }
    }
    return handles
  }
  return buttons.value.map((b: any) => ({
    id: `button:${b.id}`,
    label: b.title || '—',
    title: b.title || '—',
  }))
})

const dynamicLabel = computed(() => {
  const variable = props.data?.config?.items_var || 'items'
  if (isCarousel.value) return `Carousel · ${variable}`
  if (isList.value) return `List · ${variable}`
  const kind = props.data?.config?.dynamic_type || 'reply'
  return `${kind} · ${variable}`
})
</script>

<template>
  <BaseNode :label="data?.label || 'Buttons'" header-class="bg-purple-600" :output-handles="outputHandles" :has-input="!data?.isEntryNode">
    <template #icon><MousePointerClick class="w-4 h-4" /></template>
    <div v-if="isDynamic" class="truncate" :title="dynamicLabel">{{ dynamicLabel }}</div>
    <div v-else-if="buttons.length > 0" class="space-y-0.5">
      <p v-if="isList" class="text-[10px] uppercase tracking-wide">List</p>
      <p v-else-if="isCarousel" class="text-[10px] uppercase tracking-wide">Carousel</p>
      <div v-for="(btn, idx) in buttons" :key="btn.id" class="flex gap-1" :title="btn.title">
        <span class="font-mono font-bold">{{ Number(idx) + 1 }}:</span>
        <span class="truncate">{{ btn.title || '—' }}</span>
      </div>
    </div>
    <p v-else class="text-muted-foreground italic">No buttons</p>
  </BaseNode>
</template>
