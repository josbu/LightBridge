import { apiClient } from '../client'

export interface InstalledModule {
  id: string
  name: string
  type: string
  version: string
  status: string
  installPath?: string
  lastError?: string
  installedAt?: string
  enabledAt?: string
  manifest?: Record<string, unknown>
}

export interface MarketplaceModule {
  id: string
  name: string
  type: string
  version: string
  summary?: string
  downloadUrl?: string
  sha256?: string
}

export interface MarketplaceResult {
  modules: MarketplaceModule[]
}

export interface ModulePermissions {
  module?: InstalledModule
  permissions: Array<{
    moduleId: string
    permissionType: string
    permissionValue: string
    approved: boolean
    approvedAt?: string
    createdAt?: string
  }>
  approved: boolean
}

export async function listInstalledModules(): Promise<InstalledModule[]> {
  const { data } = await apiClient.get<{ modules: InstalledModule[] }>('/admin/modules')
  return data.modules || []
}

export async function listMarketplaceModules(): Promise<MarketplaceModule[]> {
  const { data } = await apiClient.get<MarketplaceResult>('/admin/modules/marketplace')
  return data.modules || []
}

export async function installMarketplaceModule(id: string, version: string): Promise<InstalledModule> {
  const { data } = await apiClient.post<InstalledModule>('/admin/modules/marketplace/install', { id, version })
  return data
}

export async function enableModule(id: string): Promise<InstalledModule> {
  const { data } = await apiClient.post<InstalledModule>(`/admin/modules/${encodeURIComponent(id)}/enable`)
  return data
}

export async function disableModule(id: string): Promise<InstalledModule> {
  const { data } = await apiClient.post<InstalledModule>(`/admin/modules/${encodeURIComponent(id)}/disable`)
  return data
}

export async function uninstallModule(id: string): Promise<InstalledModule> {
  const { data } = await apiClient.post<InstalledModule>(`/admin/modules/${encodeURIComponent(id)}/uninstall`)
  return data
}

export async function purgeModule(id: string): Promise<InstalledModule> {
  const { data } = await apiClient.delete<InstalledModule>(`/admin/modules/${encodeURIComponent(id)}`)
  return data
}

export async function getModulePermissions(id: string): Promise<ModulePermissions> {
  const { data } = await apiClient.get<ModulePermissions>(`/admin/modules/${encodeURIComponent(id)}/permissions`)
  return data
}

export async function approveModulePermissions(id: string): Promise<ModulePermissions> {
  const { data } = await apiClient.post<ModulePermissions>(`/admin/modules/${encodeURIComponent(id)}/permissions/approve`)
  return data
}

export default {
  listInstalledModules,
  listMarketplaceModules,
  installMarketplaceModule,
  enableModule,
  disableModule,
  uninstallModule,
  purgeModule,
  getModulePermissions,
  approveModulePermissions
}
