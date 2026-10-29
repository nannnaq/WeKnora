import { integrationContext } from './context'

export function createIntegrationStorage(ordinary: () => Storage, integrated: () => boolean) {
  const memory = new Map<string, string>()
  return {
    getItem(key: string) { return integrated() ? memory.get(key) ?? null : ordinary().getItem(key) },
    setItem(key: string, value: string) { if (integrated()) memory.set(key, String(value)); else ordinary().setItem(key, value) },
    removeItem(key: string) { if (integrated()) memory.delete(key); else ordinary().removeItem(key) },
    clearIntegration() { memory.clear() },
  }
}

// Never read the standalone site's credentials from an embedded application.
// Memory is private to this iframe and disappears on reload/account switching.
export const authStorage = createIntegrationStorage(() => localStorage, () => !!integrationContext())
