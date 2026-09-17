<script setup lang="ts">
import { computed } from 'vue'
import { ShoppingBag } from 'lucide-vue-next'
import BaseNode from '@/components/calling/nodes/BaseNode.vue'
import { tiqrStoreOperationLabel } from '@/components/chatbot/tiqrStoreApiCatalog'

defineOptions({ inheritAttrs: false })

const props = defineProps<{ data: any }>()

const summary = computed(() => tiqrStoreOperationLabel(props.data?.config?.operation))

const outputHandles = [
  { id: 'http:2xx', label: '2xx', title: 'Success' },
  { id: 'http:non2xx', label: 'Error', title: 'Error or missing store settings' },
]
</script>

<template>
  <BaseNode
    :label="data?.label || 'TiQR Store API'"
    header-class="bg-emerald-600"
    :output-handles="outputHandles"
    :has-input="!data?.isEntryNode"
  >
    <template #icon><ShoppingBag class="w-4 h-4" /></template>
    <p class="truncate text-[10px]" :title="summary">{{ summary }}</p>
  </BaseNode>
</template>
