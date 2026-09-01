import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '@/services/api'

export const useAppStore = defineStore('app', () => {
  const name = ref('Whatomate')
  const loaded = ref(false)

  const displayName = computed(() => name.value || 'Whatomate')

  async function fetchAppInfo() {
    try {
      const resp = await api.get('/app')
      const data = resp.data?.data || resp.data
      if (data?.name) {
        name.value = data.name
        document.title = data.name
      }
    } catch {
      // Keep default branding if the endpoint is unavailable
    } finally {
      loaded.value = true
    }
  }

  return { name, displayName, loaded, fetchAppInfo }
})
