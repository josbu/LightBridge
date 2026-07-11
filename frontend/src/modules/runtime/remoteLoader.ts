import { markRaw, type Component } from 'vue'

interface RemoteNamespace {
  default?: unknown
  get?: (name: string) => unknown | Promise<unknown>
  components?: Record<string, unknown>
  [key: string]: unknown
}

const namespaceCache = new Map<string, Promise<RemoteNamespace>>()
const componentCache = new Map<string, Promise<Component>>()

export function validateModuleRemoteEntry(remoteEntry: string): URL {
  const url = new URL(remoteEntry, window.location.origin)
  if (url.origin !== window.location.origin) throw new Error('module remote entry must be same-origin')
  if (url.username || url.password || url.hash) throw new Error('module remote entry contains forbidden URL fields')
  if (!url.pathname.startsWith('/modules/')) throw new Error('module remote entry must be served from /modules/')
  return url
}

async function loadNamespace(remoteEntry: string): Promise<RemoteNamespace> {
  const url = validateModuleRemoteEntry(remoteEntry)
  const key = url.href
  let pending = namespaceCache.get(key)
  if (!pending) {
    pending = import(/* @vite-ignore */ key) as Promise<RemoteNamespace>
    namespaceCache.set(key, pending)
    pending.catch(() => namespaceCache.delete(key))
  }
  return pending
}

function exposedCandidates(exposedModule: string): string[] {
  const trimmed = exposedModule.trim()
  const withoutPrefix = trimmed.replace(/^\.\//, '')
  return [trimmed, withoutPrefix, withoutPrefix.replace(/[\\/.-]+(.)/g, (_match, char: string) => char.toUpperCase())]
}

function unwrapComponent(value: unknown): Component | undefined {
  if (!value) return undefined
  if (typeof value === 'object' && value !== null && 'default' in value) {
    return unwrapComponent((value as { default?: unknown }).default)
  }
  if (typeof value === 'object' || typeof value === 'function') return markRaw(value as Component)
  return undefined
}

export async function loadModuleComponent(remoteEntry: string, exposedModule: string): Promise<Component> {
  const url = validateModuleRemoteEntry(remoteEntry)
  const cacheKey = `${url.href}#${exposedModule}`
  let pending = componentCache.get(cacheKey)
  if (pending) return pending

  pending = (async () => {
    const namespace = await loadNamespace(url.href)
    if (typeof namespace.get === 'function') {
      const component = unwrapComponent(await namespace.get(exposedModule))
      if (component) return component
    }
    for (const key of exposedCandidates(exposedModule)) {
      const component = unwrapComponent(namespace[key] ?? namespace.components?.[key])
      if (component) return component
    }
    const fallback = unwrapComponent(namespace.default)
    if (fallback) return fallback
    throw new Error(`module export ${exposedModule} was not found`)
  })()

  componentCache.set(cacheKey, pending)
  pending.catch(() => componentCache.delete(cacheKey))
  return pending
}

export function clearModuleRemoteCache(remoteEntry?: string): void {
  if (!remoteEntry) {
    namespaceCache.clear()
    componentCache.clear()
    return
  }
  const href = validateModuleRemoteEntry(remoteEntry).href
  namespaceCache.delete(href)
  for (const key of componentCache.keys()) {
    if (key.startsWith(`${href}#`)) componentCache.delete(key)
  }
}
