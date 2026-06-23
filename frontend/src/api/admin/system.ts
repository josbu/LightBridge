/**
 * System API endpoints for admin operations
 */

import { apiClient } from '../client'

export interface ReleaseInfo {
  name: string
  body: string
  published_at: string
  html_url: string
  prerelease?: boolean
}

export interface VersionRelease {
  version: string
  name: string
  body: string
  published_at: string
  html_url: string
  prerelease?: boolean
  draft?: boolean
  current?: boolean
  latest?: boolean
}

export interface VersionReleasesResult {
  current_version: string
  latest_version: string
  build_type: string
  releases: VersionRelease[]
}

export interface VersionInfo {
  current_version: string
  latest_version: string
  has_update: boolean
  release_info?: ReleaseInfo
  cached: boolean
  warning?: string
  build_type: string // "source" for manual builds, "release" for CI builds
}

/**
 * Get current version
 */
export async function getVersion(): Promise<{ version: string }> {
  const { data } = await apiClient.get<{ version: string }>('/admin/system/version')
  return data
}

/**
 * Check for updates
 * @param force - Force refresh from GitHub API
 */
export async function checkUpdates(force = false): Promise<VersionInfo> {
  const { data } = await apiClient.get<VersionInfo>('/admin/system/check-updates', {
    params: force ? { force: 'true' } : undefined
  })
  return data
}

/**
 * List published versions that can be installed.
 */
export async function listVersionReleases(force = false): Promise<VersionReleasesResult> {
  const { data } = await apiClient.get<VersionReleasesResult>('/admin/system/versions', {
    params: force ? { force: 'true' } : undefined
  })
  return data
}

export interface UpdateResult {
  message: string
  need_restart: boolean
}

export interface UpdateOptions {
  version?: string
}

/**
 * Perform system update
 * Downloads and applies the latest version
 */
export async function performUpdate(options: UpdateOptions = {}): Promise<UpdateResult> {
  const payload = options.version ? { version: options.version } : undefined
  const { data } = await apiClient.post<UpdateResult>('/admin/system/update', payload)
  return data
}

/**
 * Rollback to previous version
 */
export async function rollback(): Promise<UpdateResult> {
  const { data } = await apiClient.post<UpdateResult>('/admin/system/rollback')
  return data
}

/**
 * Restart the service
 */
export async function restartService(): Promise<{ message: string }> {
  const { data } = await apiClient.post<{ message: string }>('/admin/system/restart')
  return data
}

export const systemAPI = {
  getVersion,
  checkUpdates,
  listVersionReleases,
  performUpdate,
  rollback,
  restartService
}

export default systemAPI
