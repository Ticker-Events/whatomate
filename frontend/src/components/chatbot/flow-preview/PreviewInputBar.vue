<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from 'vue-sonner'
import { Send, Mic, Paperclip, FileText, X } from 'lucide-vue-next'

export type PreviewMediaPayload = {
  type: 'image' | 'video' | 'audio' | 'document'
  mimeType: string
  filename: string
  previewUrl: string | null
}

const props = defineProps<{
  inputType: string | null
  disabled?: boolean
  placeholder?: string
  allowMedia?: boolean
}>()

const emit = defineEmits<{
  submit: [value: string]
  submitMedia: [caption: string, media: PreviewMediaPayload]
}>()

const { t } = useI18n()

const inputValue = ref('')
const fileInputRef = ref<HTMLInputElement | null>(null)
const selectedFile = ref<File | null>(null)
const filePreviewUrl = ref<string | null>(null)

const inputPlaceholder = computed(() => {
  if (props.placeholder) return props.placeholder

  switch (props.inputType) {
    case 'email':
      return 'Enter your email address...'
    case 'phone':
      return 'Enter your phone number...'
    case 'number':
      return 'Enter a number...'
    case 'date':
      return 'Enter a date (YYYY-MM-DD)...'
    case 'text':
      return 'Type a message...'
    default:
      return 'Type a message...'
  }
})

const inputTypeAttr = computed(() => {
  switch (props.inputType) {
    case 'email':
      return 'email'
    case 'phone':
      return 'tel'
    case 'number':
      return 'number'
    case 'date':
      return 'date'
    default:
      return 'text'
  }
})

const isEnabled = computed(() => {
  return !props.disabled && props.inputType && props.inputType !== 'none'
})

const canAttach = computed(() => {
  return !!props.allowMedia && isEnabled.value
})

const inputString = computed(() => String(inputValue.value ?? '').trim())

const canSend = computed(() => {
  if (!isEnabled.value) return false
  if (selectedFile.value) return true
  return !!inputString.value
})

function mediaTypeFromMime(mimeType: string): PreviewMediaPayload['type'] {
  if (mimeType.startsWith('image/')) return 'image'
  if (mimeType.startsWith('video/')) return 'video'
  if (mimeType.startsWith('audio/')) return 'audio'
  return 'document'
}

function clearSelectedFile() {
  if (filePreviewUrl.value) {
    URL.revokeObjectURL(filePreviewUrl.value)
    filePreviewUrl.value = null
  }
  selectedFile.value = null
}

function openFilePicker() {
  if (!canAttach.value) return
  fileInputRef.value?.click()
}

function handleFileSelect(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return

  const allowedTypes = [
    'image/',
    'video/',
    'audio/',
    'application/pdf',
    'application/msword',
    'application/vnd.openxmlformats-officedocument',
  ]
  const isAllowed = allowedTypes.some((type) => file.type.startsWith(type))
  if (!isAllowed) {
    toast.error(t('chat.unsupportedFileType'), {
      description: t('chat.unsupportedFileTypeDesc'),
    })
    return
  }

  const maxSize = 16 * 1024 * 1024
  if (file.size > maxSize) {
    toast.error(t('chat.fileTooLarge'), {
      description: t('chat.fileTooLargeDesc'),
    })
    return
  }

  clearSelectedFile()
  selectedFile.value = file
  if (file.type.startsWith('image/') || file.type.startsWith('video/')) {
    filePreviewUrl.value = URL.createObjectURL(file)
  }
}

function handleSubmit() {
  if (!canSend.value) return

  if (selectedFile.value) {
    const file = selectedFile.value
    const previewUrl = filePreviewUrl.value
    // Keep the blob URL for the chat bubble; clear local selection without revoking.
    selectedFile.value = null
    filePreviewUrl.value = null
    const caption = inputString.value
    inputValue.value = ''
    emit('submitMedia', caption, {
      type: mediaTypeFromMime(file.type),
      mimeType: file.type || 'application/octet-stream',
      filename: file.name,
      previewUrl,
    })
    return
  }

  if (!inputString.value) return
  emit('submit', inputString.value)
  inputValue.value = ''
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key === 'Enter' && !event.shiftKey) {
    event.preventDefault()
    handleSubmit()
  }
}

watch(
  () => props.disabled,
  (disabled) => {
    if (disabled) clearSelectedFile()
  },
)

onBeforeUnmount(() => {
  clearSelectedFile()
})
</script>

<template>
  <div class="bg-[#f0f2f5] dark:bg-[#202c33]">
    <div
      v-if="selectedFile"
      class="px-3 pt-2 pb-1 border-b border-black/5 dark:border-white/10"
    >
      <div class="relative rounded-lg bg-white dark:bg-[#2a3942] p-2">
        <button
          type="button"
          class="absolute top-1.5 right-1.5 z-10 w-6 h-6 rounded-full bg-black/50 text-white flex items-center justify-center hover:bg-black/70"
          :aria-label="$t('common.cancel')"
          @click="clearSelectedFile"
        >
          <X class="h-3.5 w-3.5" />
        </button>
        <div v-if="selectedFile.type.startsWith('image/') && filePreviewUrl" class="flex justify-center">
          <img
            :src="filePreviewUrl"
            :alt="selectedFile.name"
            class="max-h-36 max-w-full rounded-md object-contain"
          />
        </div>
        <div v-else-if="selectedFile.type.startsWith('video/') && filePreviewUrl" class="flex justify-center">
          <video
            :src="filePreviewUrl"
            controls
            class="max-h-36 max-w-full rounded-md"
          />
        </div>
        <div v-else class="flex items-center gap-2 pr-6">
          <div class="h-9 w-9 rounded-full bg-primary/10 flex items-center justify-center shrink-0">
            <FileText class="h-4 w-4 text-primary" />
          </div>
          <div class="min-w-0">
            <p class="text-sm font-medium truncate">{{ selectedFile.name }}</p>
            <p class="text-[11px] text-muted-foreground">
              {{ (selectedFile.size / 1024).toFixed(1) }} KB
            </p>
          </div>
        </div>
      </div>
    </div>

    <div class="px-3 py-2 flex items-center gap-2">
      <button
        v-if="allowMedia"
        type="button"
        class="w-9 h-9 rounded-full flex items-center justify-center shrink-0 transition-colors"
        :class="canAttach
          ? 'hover:bg-black/5 dark:hover:bg-white/10 text-gray-600 dark:text-gray-300 cursor-pointer'
          : 'text-gray-300 dark:text-gray-600 cursor-not-allowed'"
        :disabled="!canAttach"
        :aria-label="$t('chat.attachFile')"
        @click="openFilePicker"
      >
        <Paperclip class="h-[18px] w-[18px]" />
      </button>
      <input
        ref="fileInputRef"
        type="file"
        accept="image/*,video/*,audio/*,.pdf,.doc,.docx"
        class="hidden"
        @change="handleFileSelect"
      />

      <div class="flex-1 bg-white dark:bg-[#2a3942] rounded-full px-4 py-2">
        <input
          v-if="isEnabled"
          v-model="inputValue"
          :type="inputTypeAttr"
          :placeholder="selectedFile ? $t('chat.mediaCaption') + '...' : inputPlaceholder"
          class="w-full text-sm bg-transparent border-none outline-none text-gray-800 dark:text-gray-200 placeholder:text-gray-400"
          @keydown="handleKeydown"
        />
        <p v-else class="text-sm text-gray-400">
          {{ disabled ? 'Waiting...' : 'Type a message' }}
        </p>
      </div>

      <button
        class="w-10 h-10 rounded-full flex items-center justify-center transition-colors"
        :class="{
          'bg-[#00a884] hover:bg-[#008f6d] cursor-pointer': canSend,
          'bg-gray-300 dark:bg-gray-600 cursor-not-allowed': !canSend
        }"
        :disabled="!canSend"
        @click="handleSubmit"
      >
        <Send v-if="canSend" class="h-5 w-5 text-white" />
        <Mic v-else class="h-5 w-5 text-white" />
      </button>
    </div>
  </div>
</template>
