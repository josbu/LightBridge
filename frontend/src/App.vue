<script setup lang="ts">
import { RouterView, useRouter, useRoute } from 'vue-router'
import { onMounted, onBeforeUnmount, watch } from 'vue'
import Toast from '@/components/common/Toast.vue'
import NavigationProgress from '@/components/common/NavigationProgress.vue'
import { resolveDocumentTitle } from '@/router/title'
import AnnouncementPopup from '@/components/common/AnnouncementPopup.vue'
import { useAppStore, useAuthStore, useSubscriptionStore, useAnnouncementStore } from '@/stores'
import { getSetupStatus } from '@/api/setup'
import { syncDistributionRoutes } from '@/router'
import { useDeploymentMode, isDistributionModeNow } from '@/composables/useDeploymentMode'

const router = useRouter()
const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const subscriptionStore = useSubscriptionStore()
const announcementStore = useAnnouncementStore()
const { isDistributionMode } = useDeploymentMode()

// 运行时切换部署模式：管理员在设置页改动 deployment_mode 并刷新 public settings 后，
// 这里据新值动态注册 / 注销分发路由（分发模式按需下载 chunk，个人模式结构性移除）。
watch(
  () => appStore.cachedPublicSettings?.deployment_mode,
  () => {
    syncDistributionRoutes(isDistributionModeNow())
  },
)

/**
 * Update favicon dynamically
 * @param logoUrl - URL of the logo to use as favicon
 */
function updateFavicon(logoUrl: string) {
  // Find existing favicon link or create new one
  let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (!link) {
    link = document.createElement('link')
    link.rel = 'icon'
    document.head.appendChild(link)
  }
  link.type = logoUrl.endsWith('.svg') ? 'image/svg+xml' : 'image/x-icon'
  link.href = logoUrl
}

// Watch for site settings changes and update favicon/title
watch(
  () => appStore.siteLogo,
  (newLogo) => {
    if (newLogo) {
      updateFavicon(newLogo)
    }
  },
  { immediate: true }
)

// Watch for authentication state and manage subscription data + announcements
function onVisibilityChange() {
  if (document.visibilityState === 'visible' && authStore.isAuthenticated) {
    announcementStore.fetchAnnouncements()
  }
}

watch(
  () => authStore.isAuthenticated,
  (isAuthenticated, oldValue) => {
    if (isAuthenticated) {
      // 订阅与公告均属分发功能：个人模式下不预载、不轮询、不拉取。
      if (isDistributionModeNow()) {
        // User logged in: preload subscriptions and start polling
        subscriptionStore.fetchActiveSubscriptions().catch((error) => {
          console.error('Failed to preload subscriptions:', error)
        })
        subscriptionStore.startPolling()

        // Announcements: new login vs page refresh restore
        if (oldValue === false) {
          // New login: delay 3s then force fetch
          setTimeout(() => announcementStore.fetchAnnouncements(true), 3000)
        } else {
          // Page refresh restore (oldValue was undefined)
          announcementStore.fetchAnnouncements()
        }

        // Register visibility change listener
        document.addEventListener('visibilitychange', onVisibilityChange)
      }
    } else {
      // User logged out: clear data and stop polling
      subscriptionStore.clear()
      announcementStore.reset()
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  },
  { immediate: true }
)

// Route change trigger (throttled by store)
router.afterEach(() => {
  if (authStore.isAuthenticated && isDistributionModeNow()) {
    announcementStore.fetchAnnouncements()
  }
})

onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibilityChange)
})

onMounted(async () => {
  // Check if setup is needed
  try {
    const status = await getSetupStatus()
    if (status.needs_setup && route.path !== '/setup') {
      router.replace('/setup')
      return
    }
  } catch {
    // If setup endpoint fails, assume normal mode and continue
  }

  // Load public settings into appStore (will be cached for other components)
  await appStore.fetchPublicSettings()

  // SSR 注入缺失时，public settings 通过异步接口才到位；此处依据最新值再同步一次
  // 分发路由的注册状态（与 main.ts 的同步互补，确保两条通道都被覆盖）。
  syncDistributionRoutes(isDistributionModeNow())

  // Re-resolve document title now that siteName is available
  document.title = resolveDocumentTitle(route.meta.title, appStore.siteName, route.meta.titleKey as string)
})
</script>

<template>
  <NavigationProgress />
  <RouterView />
  <Toast />
  <!-- 公告弹窗属分发功能：个人模式下不挂载 -->
  <AnnouncementPopup v-if="isDistributionMode" />
</template>
