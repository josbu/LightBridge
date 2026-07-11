import { apiClient } from './client'

export type BackendFeatureTier = 'core' | 'optional' | 'extension'
export type BackendFeatureActivation = 'eager' | 'dynamic' | 'boot' | 'on_demand'
export type BackendFeatureProfile = 'minimal' | 'standard' | 'full'
export type BackendFeatureSurface =
  | 'backend_route'
  | 'frontend_route'
  | 'menu'
  | 'worker'
  | 'module_runtime'

export interface BackendFeatureState {
  id: string
  enabled: boolean
  configuredEnabled: boolean
  requiresRestart?: boolean
  reason: string
  tier: BackendFeatureTier
  activation: BackendFeatureActivation
  minimumProfile: BackendFeatureProfile
  surfaces?: BackendFeatureSurface[]
}

export async function getFeatureManifest(signal?: AbortSignal): Promise<BackendFeatureState[]> {
  const { data } = await apiClient.get<BackendFeatureState[]>('/settings/features', { signal })
  return Array.isArray(data) ? data : []
}
