<script setup lang="ts">
import type { ButtonConfig, PreviewCarouselCard } from '@/types/flow-preview'
import { ExternalLink } from 'lucide-vue-next'

defineProps<{
  cards: PreviewCarouselCard[]
  disabled?: boolean
}>()

const emit = defineEmits<{
  select: [button: ButtonConfig]
}>()
</script>

<template>
  <div class="mt-1 flex gap-2 overflow-x-auto pb-1">
    <div
      v-for="(card, index) in cards"
      :key="index"
      class="w-44 shrink-0 overflow-hidden rounded-lg bg-white shadow-sm dark:bg-[#202c33]"
    >
      <img
        v-if="card.mediaType !== 'video' && card.mediaUrl"
        :src="card.mediaUrl"
        alt=""
        class="h-24 w-full object-cover"
      />
      <video
        v-else-if="card.mediaUrl"
        :src="card.mediaUrl"
        class="h-24 w-full object-cover"
      />
      <div v-else class="h-24 w-full bg-gray-200 dark:bg-[#2a3942]" />
      <p v-if="card.body" class="whitespace-pre-wrap px-2 py-1 text-xs">{{ card.body }}</p>
      <div class="space-y-1 p-1">
        <template v-for="btn in card.buttons" :key="btn.id">
          <a
            v-if="btn.type === 'url'"
            :href="btn.url || '#'"
            target="_blank"
            rel="noopener noreferrer"
            class="flex items-center justify-center gap-1 rounded-md py-2 text-sm font-medium text-[#00a884]"
          >
            <ExternalLink class="h-4 w-4" />
            {{ btn.title || 'Open' }}
          </a>
          <button
            v-else
            type="button"
            class="w-full rounded-md py-2 text-sm font-medium text-[#00a884]"
            :class="disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer hover:bg-gray-50 dark:hover:bg-[#2a3942]'"
            :disabled="disabled"
            @click="emit('select', btn)"
          >
            {{ btn.title || 'Option' }}
          </button>
        </template>
      </div>
    </div>
  </div>
</template>
