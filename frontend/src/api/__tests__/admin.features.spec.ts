import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put, remove } = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  remove: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    put,
    delete: remove,
  },
}))

import {
  clearFeatureControlOverride,
  listFeatureControls,
  setFeatureControl,
  unwrapFeatureControlOverview,
} from '@/api/admin/features'

const overview = {
  profile: 'full',
  features: [{
    id: 'module_runtime',
    label: 'Module Runtime',
    tier: 'extension',
    activation: 'boot',
    enabled: true,
    configuredEnabled: true,
    available: true,
    controllable: true,
    override: null,
    requiresRestart: false,
    reason: 'enabled',
    minimumProfile: 'full',
    dependencies: ['core_gateway'],
    surfaces: ['menu'],
    runtimeComponents: [{ name: 'module_runtime', status: 'running', lastError: '' }],
  }],
}

describe('admin feature control API', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
    remove.mockReset()
  })

  it('reads the overview with the camelCase control-plane contract', async () => {
    get.mockResolvedValue({ data: overview })

    const result = await listFeatureControls()

    expect(get).toHaveBeenCalledWith('/admin/features')
    expect(result).toMatchObject(overview)
  })

  it('accepts a nested data overview and the snake_case preview fields during rollout', () => {
    const result = unwrapFeatureControlOverview({
      data: {
        profile: 'standard',
        features: [{
          id: 'backup',
          label: 'Backup',
          tier: 'optional',
          activation: 'boot',
          enabled: false,
          configured_enabled: true,
          requires_restart: true,
          minimum_profile: 'standard',
          runtime_components: [{ name: 'backup_scheduler', last_error: 'waiting' }],
        }],
      },
    })

    expect(result).toEqual(expect.objectContaining({ profile: 'standard' }))
    expect(result.features[0]).toEqual(expect.objectContaining({
      configuredEnabled: true,
      requiresRestart: true,
      minimumProfile: 'standard',
      runtimeComponents: [expect.objectContaining({ lastError: 'waiting' })],
    }))
  })

  it('uses PUT and DELETE controls and returns their complete refreshed overview', async () => {
    const disabledOverview = {
      ...overview,
      features: [{ ...overview.features[0], enabled: false, configuredEnabled: false, override: false }],
    }
    put.mockResolvedValue({ data: disabledOverview })
    remove.mockResolvedValue({ data: overview })

    await expect(setFeatureControl('module runtime', false)).resolves.toMatchObject(disabledOverview)
    expect(put).toHaveBeenCalledWith('/admin/features/module%20runtime', { enabled: false })

    await expect(clearFeatureControlOverride('module_runtime')).resolves.toMatchObject(overview)
    expect(remove).toHaveBeenCalledWith('/admin/features/module_runtime/override')
  })
})
