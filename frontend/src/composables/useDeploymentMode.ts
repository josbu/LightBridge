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
 * 本模块只负责解析部署模式；哪些页面 / 后台能力属于渐进式模块由
 * `utils/progressiveFeatures.ts` 统一声明。
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
