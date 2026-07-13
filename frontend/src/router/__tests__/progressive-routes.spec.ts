import { beforeEach, describe, expect, it, vi } from 'vitest'

const appStore = vi.hoisted(() => ({
  siteName: 'LightBridge',
  backendModeEnabled: false,
  cachedPublicSettings: null as null | Record<string, unknown>,
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    checkAuth: vi.fn(),
    isAuthenticated: false,
    isAdmin: false,
    isSimpleMode: false,
    hasPendingAuthSession: false,
  }),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({
    customMenuItems: [],
  }),
}))

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    startNavigation: vi.fn(),
    endNavigation: vi.fn(),
    isLoading: { value: false },
  }),
}))

vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({
    triggerPrefetch: vi.fn(),
    cancelPendingPrefetch: vi.fn(),
    resetPrefetchState: vi.fn(),
  }),
}))

vi.mock('@/api/setup', () => ({
  getSetupStatus: vi.fn(),
}))

vi.mock('@/modules/runtime/registry', () => ({
  syncModuleRuntime: vi.fn().mockResolvedValue(undefined),
  resetModuleRuntime: vi.fn(),
}))

async function loadRouterWithSettings(settings: Record<string, unknown>) {
  appStore.cachedPublicSettings = settings
  vi.resetModules()
  const routerModule = await import('@/router')
  await routerModule.syncProgressiveRoutes()
  return routerModule.default
}

describe('progressive route registry', () => {
  beforeEach(() => {
    appStore.cachedPublicSettings = null
  })

  it('keeps redeem routes absent while the redeem feature is disabled', async () => {
    const router = await loadRouterWithSettings({
      deployment_mode: 'distribution',
      redeem_enabled: false,
    })

    expect(router.hasRoute('Redeem')).toBe(false)
    expect(router.hasRoute('AdminRedeem')).toBe(false)
  })

  it('registers redeem routes when the feature is enabled in distribution mode', async () => {
    const router = await loadRouterWithSettings({
      deployment_mode: 'distribution',
      redeem_enabled: true,
    })

    expect(router.hasRoute('Redeem')).toBe(true)
    expect(router.hasRoute('AdminRedeem')).toBe(true)
  })

  it('keeps opt-in available channels absent until explicitly enabled', async () => {
    const missingFlagRouter = await loadRouterWithSettings({
      deployment_mode: 'distribution',
    })
    expect(missingFlagRouter.hasRoute('UserAvailableChannels')).toBe(false)

    const disabledRouter = await loadRouterWithSettings({
      deployment_mode: 'distribution',
      available_channels_enabled: false,
    })
    expect(disabledRouter.hasRoute('UserAvailableChannels')).toBe(false)

    const enabledRouter = await loadRouterWithSettings({
      deployment_mode: 'distribution',
      available_channels_enabled: true,
    })
    expect(enabledRouter.hasRoute('UserAvailableChannels')).toBe(true)
  })

  it('removes distribution-only routes in personal mode even when their flags are enabled', async () => {
    const router = await loadRouterWithSettings({
      deployment_mode: 'personal',
      redeem_enabled: true,
      announcements_enabled: true,
    })

    expect(router.hasRoute('Redeem')).toBe(false)
    expect(router.hasRoute('AdminAnnouncements')).toBe(false)
    expect(router.hasRoute('AdminChannelMonitor')).toBe(true)
  })

  it('keeps the feature registry route available when module runtime is not registered', async () => {
    const router = await loadRouterWithSettings({
      deployment_mode: 'distribution',
    })

    expect(router.hasRoute('AdminFeatureRegistry')).toBe(true)
    expect(router.resolve('/admin/features').name).toBe('AdminFeatureRegistry')
    expect(router.hasRoute('AdminModules')).toBe(false)
  })
})
