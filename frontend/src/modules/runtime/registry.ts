import { shallowRef } from 'vue'
import type { Router, RouteRecordName, RouteRecordRaw } from 'vue-router'
import {
  getModuleUIManifest,
  type LocalizedText,
  type ModuleUIAccountFormSpec,
  type ModuleUIEntityPanelSpec,
  type ModuleUIMenuSpec,
  type ModuleUIRouteSpec,
} from '@/api/modules'
import { clearModuleRemoteCache, validateModuleRemoteEntry } from './remoteLoader'

export interface RegisteredModuleMenu extends ModuleUIMenuSpec {
  key: string
  moduleId: string
  moduleVersion: string
}
export interface RegisteredModuleAccountForm extends ModuleUIAccountFormSpec { key: string }
export interface RegisteredModuleEntityPanel extends ModuleUIEntityPanelSpec { key: string }
export interface RegisteredModuleRoute extends ModuleUIRouteSpec {
  key: string
  moduleId: string
  moduleVersion: string
}

export const moduleMenuContributions = shallowRef<readonly RegisteredModuleMenu[]>([])
export const moduleAccountFormContributions = shallowRef<readonly RegisteredModuleAccountForm[]>([])
export const moduleEntityPanelContributions = shallowRef<readonly RegisteredModuleEntityPanel[]>([])

const routeRemovers = new Map<RouteRecordName, () => void>()
let desiredEnabled = false
let requestedGeneration = 0
let appliedGeneration = 0
let syncRequest: Promise<void> | null = null

export function isSafeModuleRoutePath(path: string): boolean {
  return path.startsWith('/admin/') && !path.includes(':') && !path.includes('*') && !path.includes('..')
}

function clearRoutes(): void {
  for (const remove of routeRemovers.values()) remove()
  routeRemovers.clear()
}

function resetContributions(): void {
  clearRoutes()
  moduleMenuContributions.value = []
  moduleAccountFormContributions.value = []
  moduleEntityPanelContributions.value = []
  clearModuleRemoteCache()
}

function namespacedRouteName(moduleId: string, version: string, index: number): string {
  return `Module:${moduleId}:${version}:${index}`
}

async function fetchAndApplyModuleManifest(router: Router, generation: number): Promise<void> {
  const controller = new AbortController()
  const timer = window.setTimeout(() => controller.abort(), 7000)
  try {
    const manifests = await getModuleUIManifest(controller.signal)
    if (generation !== requestedGeneration || !desiredEnabled) return

    const existingPaths = new Set(
      router.getRoutes()
        .filter((route) => !route.name || !routeRemovers.has(route.name))
        .map((route) => route.path),
    )
    const acceptedPaths = new Set<string>()
    const routes: RegisteredModuleRoute[] = []
    const menus: RegisteredModuleMenu[] = []
    const forms: RegisteredModuleAccountForm[] = []
    const panels: RegisteredModuleEntityPanel[] = []

    for (const manifest of manifests) {
      try { validateModuleRemoteEntry(manifest.remoteEntry) } catch { continue }
      for (const [index, route] of (manifest.routes ?? []).entries()) {
        if (!isSafeModuleRoutePath(route.path) || existingPaths.has(route.path) || acceptedPaths.has(route.path)) continue
        try { validateModuleRemoteEntry(route.remoteEntry) } catch { continue }
        routes.push({
          ...route,
          key: namespacedRouteName(manifest.moduleId, manifest.version, index),
          moduleId: manifest.moduleId,
          moduleVersion: manifest.version,
        })
        acceptedPaths.add(route.path)
      }
      for (const [index, menu] of (manifest.menu ?? []).entries()) {
        if (!acceptedPaths.has(menu.path)) continue
        menus.push({
          ...menu,
          key: `${manifest.moduleId}:${manifest.version}:menu:${index}`,
          moduleId: manifest.moduleId,
          moduleVersion: manifest.version,
        })
      }
      for (const [index, form] of (manifest.accountForms ?? []).entries()) {
        try { validateModuleRemoteEntry(form.remoteEntry) } catch { continue }
        forms.push({ ...form, key: `${manifest.moduleId}:${manifest.version}:form:${index}` })
      }
      for (const [index, panel] of (manifest.entityPanels ?? []).entries()) {
        try { validateModuleRemoteEntry(panel.remoteEntry) } catch { continue }
        panels.push({ ...panel, key: `${manifest.moduleId}:${manifest.version}:panel:${index}` })
      }
    }

    if (generation !== requestedGeneration || !desiredEnabled) return
    clearRoutes()
    clearModuleRemoteCache()
    for (const contribution of routes) {
      const route: RouteRecordRaw = {
        path: contribution.path,
        name: contribution.key,
        component: () => import('./ModuleRouteHost.vue'),
        props: { contribution },
        meta: {
          requiresAuth: true,
          requiresAdmin: contribution.requiresAdmin !== false,
          title: contribution.title,
        },
      }
      routeRemovers.set(contribution.key, router.addRoute(route))
    }
    moduleMenuContributions.value = Object.freeze(menus.sort((a, b) => (a.order ?? 0) - (b.order ?? 0)))
    moduleAccountFormContributions.value = Object.freeze(forms)
    moduleEntityPanelContributions.value = Object.freeze(panels.sort((a, b) => (a.order ?? 0) - (b.order ?? 0)))
  } catch (error) {
    // Optional module UI must never prevent core application boot. Preserve the
    // last known-good registrations and retry on the next reconciliation.
    console.warn('Failed to refresh module UI manifest:', error)
  } finally {
    window.clearTimeout(timer)
  }
}

async function reconcileModuleRuntime(router: Router): Promise<void> {
  while (appliedGeneration !== requestedGeneration) {
    const generation = requestedGeneration
    if (!desiredEnabled) {
      resetContributions()
      appliedGeneration = generation
      continue
    }
    await fetchAndApplyModuleManifest(router, generation)
    if (generation === requestedGeneration) appliedGeneration = generation
  }
}

export async function syncModuleRuntime(router: Router, enabled: boolean): Promise<void> {
  desiredEnabled = enabled
  ++requestedGeneration
  if (!enabled) resetContributions()
  if (!syncRequest) {
    syncRequest = reconcileModuleRuntime(router).finally(() => { syncRequest = null })
  }
  await syncRequest
}

export function resolveModuleText(fallback: string, localized: LocalizedText | undefined, locale: string): string {
  if (!localized) return fallback
  return localized[locale] ?? localized[locale.split('-')[0]] ?? localized.en ?? fallback
}
