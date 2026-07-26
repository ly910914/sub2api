import type { ApiKey } from '@/types'

export function isImageCapableAPIKey(key: ApiKey): boolean {
  return key.status === 'active' && Boolean(key.key)
}

export function listImageCapableAPIKeys(keys: ApiKey[]): ApiKey[] {
  return keys.filter(isImageCapableAPIKey)
}

export function pickDefaultImageAPIKey(keys: ApiKey[], currentID: number | null = null): ApiKey | null {
  const eligible = listImageCapableAPIKeys(keys)
  if (currentID !== null) {
    const current = eligible.find((key) => key.id === currentID)
    if (current) return current
  }
  return eligible[0] ?? null
}
