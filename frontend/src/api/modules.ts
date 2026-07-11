import { apiClient } from './client'

export type LocalizedText = Record<string, string>

export interface ModuleUIRouteSpec {
  path: string
  title: string
  title_i18n?: LocalizedText
  remoteEntry: string
  exposedModule: string
  requiresAdmin?: boolean
}

export interface ModuleUIMenuSpec {
  title: string
  title_i18n?: LocalizedText
  path: string
  group?: string
  order?: number
}

export interface ModuleUIAccountFormSpec {
  providerId: string
  providerName?: string
  providerNameI18n?: LocalizedText
  moduleId?: string
  moduleName?: string
  moduleNameI18n?: LocalizedText
  moduleVersion?: string
  remoteEntry: string
  exposedModule: string
}

export interface ModuleUIEntityPanelSpec {
  entity: string
  title: string
  title_i18n?: LocalizedText
  moduleId: string
  moduleVersion: string
  remoteEntry: string
  exposedModule: string
  requiresAdmin?: boolean
  order?: number
}

export interface ModuleUIManifestItem {
  moduleId: string
  moduleName: string
  moduleNameI18n?: LocalizedText
  version: string
  remoteEntry: string
  routes?: ModuleUIRouteSpec[]
  menu?: ModuleUIMenuSpec[]
  accountForms?: ModuleUIAccountFormSpec[]
  entityPanels?: ModuleUIEntityPanelSpec[]
}

export async function getModuleUIManifest(signal?: AbortSignal): Promise<ModuleUIManifestItem[]> {
  const { data } = await apiClient.get<{ modules?: ModuleUIManifestItem[] }>('/modules/ui', { signal })
  return Array.isArray(data?.modules) ? data.modules : []
}
