import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router, { syncDistributionRoutes } from './router'
import i18n, { initI18n } from './i18n'
import { useAppStore } from '@/stores/app'
import { isDistributionModeNow } from '@/composables/useDeploymentMode'
import './style.css'

function initThemeClass() {
  const savedTheme = localStorage.getItem('theme')
  const shouldUseDark =
    savedTheme === 'dark' ||
    (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.classList.toggle('dark', shouldUseDark)
}

async function bootstrap() {
  // Apply theme class globally before app mount to keep all routes consistent.
  initThemeClass()

  const app = createApp(App)
  const pinia = createPinia()
  app.use(pinia)

  // Initialize settings from injected config BEFORE mounting (prevents flash)
  // This must happen after pinia is installed but before router and i18n
  const appStore = useAppStore()
  appStore.initFromInjectedConfig()

  // 依据注入的 public settings 决定是否注册分发模式路由。
  // 个人模式下不注册 → 对应 chunk 永不下载（结构性移除）；分发模式注册后按需下载。
  syncDistributionRoutes(isDistributionModeNow())

  // Set document title immediately after config is loaded
  if (appStore.siteName && appStore.siteName !== 'LightBridge') {
    document.title = `${appStore.siteName} - AI API Gateway`
  }

  await initI18n()

  app.use(router)
  app.use(i18n)

  // 等待路由器完成初始导航后再挂载，避免竞态条件导致的空白渲染
  await router.isReady()
  app.mount('#app')
}

bootstrap()
