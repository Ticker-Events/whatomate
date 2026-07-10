import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { VueQueryPlugin } from '@tanstack/vue-query'

import App from './App.vue'
import router from './router'
import { i18n } from './i18n'
import { useAppStore } from './stores/app'

import './assets/fonts.css'
import './assets/index.css'

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
app.use(router)
app.use(VueQueryPlugin)
app.use(i18n)

useAppStore(pinia).fetchAppInfo()

app.mount('#app')
