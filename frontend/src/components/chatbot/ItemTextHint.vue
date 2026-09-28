<script setup lang="ts">
import { ref } from 'vue'
import { CircleHelp } from 'lucide-vue-next'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'

const open = ref(false)
let closeTimer: ReturnType<typeof setTimeout> | undefined

function show() {
  if (closeTimer) clearTimeout(closeTimer)
  open.value = true
}

function hide() {
  closeTimer = setTimeout(() => {
    open.value = false
  }, 120)
}

function setOpen(value: boolean) {
  open.value = value
}
</script>

<template>
  <Popover :open="open" @update:open="setOpen">
    <PopoverTrigger as-child>
      <button
        type="button"
        class="inline-flex text-muted-foreground hover:text-foreground"
        aria-label="How this field is filled in"
        @mouseenter="show"
        @mouseleave="hide"
        @focus="show"
        @blur="hide"
      >
        <CircleHelp class="h-3.5 w-3.5" />
      </button>
    </PopoverTrigger>
    <PopoverContent
      class="w-80 p-3"
      align="end"
      @mouseenter="show"
      @mouseleave="hide"
      @open-auto-focus.prevent
    >
      <ul v-pre class="space-y-1.5 text-xs text-muted-foreground">
        <li>A column name, such as <span class="font-mono text-foreground">name</span>, uses that column on the row.</li>
        <li>Plain text, such as <span class="font-mono text-foreground">Buy now</span>, is shown as written when the row has no column with that exact name.</li>
        <li>Mix columns and text with <span class="font-mono text-foreground">{{name}}({{price}})</span>, which becomes Aloe(250).</li>
        <li>When the row and the session both have <span class="font-mono text-foreground">name</span>, <span class="font-mono text-foreground">{{name}}</span> uses the row. The session value is used only when the row has no such column.</li>
        <li>A <span class="font-mono text-foreground">{{placeholder}}</span> that matches nothing is left blank. An empty title skips that row.</li>
      </ul>
    </PopoverContent>
  </Popover>
</template>
