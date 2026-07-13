import { apiClient } from '../client'

export type FeatureControlTier = 'core' | 'optional' | 'extension' | string
export type FeatureControlActivation = 'eager' | 'dynamic' | 'boot' | 'on_demand' | string

export interface FeatureRuntimeComponent {
  name?: string
  feature?: string
  activation?: string
  status?: string
  running?: boolean
  started?: boolean
  cleanupRequired?: boolean
  lastError?: string
  updatedAt?: string
  [key: string]: unknown
}

/**
 * Administrative control-plane view of a progressively registered feature.
 *
 * Field names follow the administrative API's camelCase contract. The API
 * adapter also accepts the earlier snake_case preview shape while instances
 * are rolling forward.
 */
export interface FeatureControlState {
  id: string
  label: string
  tier: FeatureControlTier
  activation: FeatureControlActivation
  enabled: boolean
  configuredEnabled: boolean
  available?: boolean
  controllable?: boolean
  override?: boolean | null
  requiresRestart?: boolean
  reason?: string
  minimumProfile?: string
  dependencies?: string[]
  surfaces?: string[]
  runtimeComponents?: FeatureRuntimeComponent[]
}

export interface FeatureControlResponse {
  features: FeatureControlState[]
  profile?: string
}

type UnknownRecord = Record<string, unknown>

function isRecord(value: unknown): value is UnknownRecord {
  return typeof value === 'object' && value !== null
}

function readString(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined
}

function readBoolean(value: unknown): boolean | undefined {
  return typeof value === 'boolean' ? value : undefined
}

function readStrings(value: unknown): string[] | undefined {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : undefined
}

function normalizeRuntimeComponents(value: unknown): FeatureRuntimeComponent[] | undefined {
  if (!Array.isArray(value)) return undefined
  return value.filter(isRecord).map((component) => ({
    ...component,
    name: readString(component.name),
    feature: readString(component.feature),
    activation: readString(component.activation),
    status: readString(component.status),
    running: readBoolean(component.running),
    started: readBoolean(component.started),
    cleanupRequired: readBoolean(component.cleanupRequired ?? component.cleanup_required),
    lastError: readString(component.lastError ?? component.last_error),
    updatedAt: readString(component.updatedAt ?? component.updated_at),
  }))
}

function normalizeFeature(value: unknown): FeatureControlState | null {
  if (!isRecord(value)) return null
  const id = readString(value.id)
  if (!id) return null
  return {
    id,
    label: readString(value.label) || id,
    tier: readString(value.tier) || 'optional',
    activation: readString(value.activation) || 'dynamic',
    enabled: readBoolean(value.enabled) ?? false,
    configuredEnabled: readBoolean(value.configuredEnabled ?? value.configured_enabled) ?? false,
    available: readBoolean(value.available),
    controllable: readBoolean(value.controllable),
    override: readBoolean(value.override) ?? (value.override === null ? null : undefined),
    requiresRestart: readBoolean(value.requiresRestart ?? value.requires_restart),
    reason: readString(value.reason),
    minimumProfile: readString(value.minimumProfile ?? value.minimum_profile),
    dependencies: readStrings(value.dependencies),
    surfaces: readStrings(value.surfaces),
    runtimeComponents: normalizeRuntimeComponents(value.runtimeComponents ?? value.runtime_components),
  }
}

/** Accept the normal overview and the common { data: overview } fallback. */
export function unwrapFeatureControlOverview(value: unknown): FeatureControlResponse {
  const outer = isRecord(value) ? value : {}
  const overview = isRecord(outer.features) || !isRecord(outer.data)
    ? outer
    : outer.data as UnknownRecord
  const rawFeatures = Array.isArray(overview.features) ? overview.features : []
  return {
    features: rawFeatures.map(normalizeFeature).filter((feature): feature is FeatureControlState => feature !== null),
    profile: readString(overview.profile),
  }
}

export async function listFeatureControls(): Promise<FeatureControlResponse> {
  const { data } = await apiClient.get<unknown>('/admin/features')
  return unwrapFeatureControlOverview(data)
}

export async function setFeatureControl(id: string, enabled: boolean): Promise<FeatureControlResponse> {
  const { data } = await apiClient.put<unknown>(
    `/admin/features/${encodeURIComponent(id)}`,
    { enabled },
  )
  return unwrapFeatureControlOverview(data)
}

export async function clearFeatureControlOverride(id: string): Promise<FeatureControlResponse> {
  const { data } = await apiClient.delete<unknown>(
    `/admin/features/${encodeURIComponent(id)}/override`,
  )
  return unwrapFeatureControlOverview(data)
}
