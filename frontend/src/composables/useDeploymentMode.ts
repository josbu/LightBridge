/**
 * Deployment mode — 个人模式 (personal) vs 分发模式 (distribution).
 *
 * ## 设计意图
 *
 * 个人模式并非「禁用」分发功能，而是 **渐进式结构性移除**：分发相关的路由不被
 * 注册，其 lazy `import()` 对应的 JS chunk 永远不会被浏览器下载。切换到分发模式
 * 时，通过 `router.addRoute()` 动态注册这些路由，浏览器在导航到对应页面时才按需
 * 下载 chunk —— 即「在分发模式下再下载」。
 *
 * 因此本模块是「哪些功能属于分发」的唯一事实来源，被三处消费：
 *   1. `router/index.ts` —— 动态注册 / 注销分发路由（结构性移除的核心）。
 *   2. `components/layout/AppSidebar.vue` —— 菜单项可见性（通过 featureFlag）。
 *   3. `views/admin/SettingsView.vue` —— 模式切换开关。
 *
 * 部署模式来自 public settings 的 `deployment_mode` 字段（同 backend_mode_enabled
 * 等机制）。缺省 / 未加载时回落到 distribution，保持向后兼容且避免菜单闪烁消失。
 */

import { computed } from 'vue'
import { useAppStore } from '@/stores/app'

export const DEPLOYMENT_MODE_PERSONAL = 'personal'
export const DEPLOYMENT_MODE_DISTRIBUTION = 'distribution'

export type DeploymentMode = 'personal' | 'distribution'

/**
 * 个人模式下被结构性移除的分发功能路由名（对应 router 中的 `name`）。
 * 用户明确要求移除的 5 类：公告、风控、兑换码、优惠码、订阅。
 */
export const DISTRIBUTION_ROUTE_NAMES = [
  // 公告
  'AdminAnnouncements',
  // 风控
  'AdminRiskControl',
  // 兑换码（管理端 + 用户端）
  'AdminRedeem',
  'Redeem',
  // 优惠码
  'AdminPromoCodes',
  // 订阅（管理端 + 用户端）
  'AdminSubscriptions',
  'Subscriptions',
] as const

export type DistributionRouteName = (typeof DISTRIBUTION_ROUTE_NAMES)[number]

/**
 * 个人模式下被隐藏的菜单/路由路径前缀。用于导航守卫兜底拦截（即便路由已注销，
 * 直接输入 URL 也会被重定向而非停在 404）。
 */
export const DISTRIBUTION_PATH_PREFIXES = [
  '/admin/announcements',
  '/admin/risk-control',
  '/admin/redeem',
  '/admin/promo-codes',
  '/admin/subscriptions',
  '/redeem',
  '/subscriptions',
] as const

/**
 * 从 public settings 解析当前部署模式（纯函数，可在非组件上下文调用，如 router 守卫）。
 * 未加载 / 非法值回落到 distribution。
 */
export function resolveDeploymentMode(): DeploymentMode {
  const appStore = useAppStore()
  return appStore.cachedPublicSettings?.deployment_mode === DEPLOYMENT_MODE_PERSONAL
    ? DEPLOYMENT_MODE_PERSONAL
    : DEPLOYMENT_MODE_DISTRIBUTION
}

export function isPersonalModeNow(): boolean {
  return resolveDeploymentMode() === DEPLOYMENT_MODE_PERSONAL
}

export function isDistributionModeNow(): boolean {
  return resolveDeploymentMode() === DEPLOYMENT_MODE_DISTRIBUTION
}

/** 判断某路径是否属于分发功能（个人模式下应被移除）。 */
export function isDistributionPath(path: string): boolean {
  return DISTRIBUTION_PATH_PREFIXES.some(
    (prefix) => path === prefix || path.startsWith(prefix + '/'),
  )
}

/**
 * 组合式入口：返回响应式的模式状态，供组件 / 菜单使用。
 */
export function useDeploymentMode() {
  const appStore = useAppStore()

  const deploymentMode = computed<DeploymentMode>(() =>
    appStore.cachedPublicSettings?.deployment_mode === DEPLOYMENT_MODE_PERSONAL
      ? DEPLOYMENT_MODE_PERSONAL
      : DEPLOYMENT_MODE_DISTRIBUTION,
  )

  const isPersonalMode = computed(() => deploymentMode.value === DEPLOYMENT_MODE_PERSONAL)
  const isDistributionMode = computed(() => deploymentMode.value === DEPLOYMENT_MODE_DISTRIBUTION)

  return { deploymentMode, isPersonalMode, isDistributionMode }
}
