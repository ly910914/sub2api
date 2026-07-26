import type { ImageResponseItem } from '@/api/images'

export type ImageConversationMode = 'generate' | 'edit'
export type ImageTurnStatus = 'queued' | 'generating' | 'success' | 'error'
export type StoredGeneratedImageStatus = 'loading' | 'success' | 'error'

export interface StoredReferenceImage {
  name: string
  type: string
  dataUrl: string
}

export interface StoredGeneratedImage extends ImageResponseItem {
  id: string
  status: StoredGeneratedImageStatus
  error?: string
  /** Wall-clock generation time in ms (client-measured). */
  durationMs?: number
  /** Client timestamp when this image's request started (for live elapsed). */
  startedAt?: number
}

export interface ImageTurn {
  id: string
  prompt: string
  model: string
  mode: ImageConversationMode
  referenceImages: StoredReferenceImage[]
  count: number
  size: string
  ratio?: string
  tier?: string
  quality: string
  images: StoredGeneratedImage[]
  status: ImageTurnStatus
  error?: string
  createdAt: string
  promptDeleted?: boolean
  resultsDeleted?: boolean
}

export interface ImageConversation {
  id: string
  title: string
  createdAt: string
  updatedAt: string
  turns: ImageTurn[]
}

const STORAGE_KEY = 'sub2api:image_conversations:v1'
const MAX_CONVERSATIONS = 24
const MAX_TURNS_PER_CONVERSATION = 20
const MAX_REFERENCE_BYTES = 700_000
const MAX_JSON_BYTES = 4_000_000
const MAX_STORED_IMAGE_BASE64_BYTES = 80_000

export function createImageConversationId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`
}

export function buildImageConversationTitle(prompt: string): string {
  const normalized = prompt.trim().replace(/\s+/g, ' ')
  if (!normalized) return 'Untitled'
  return normalized.length > 18 ? `${normalized.slice(0, 18)}...` : normalized
}

export function normalizeStoredConversations(value: unknown): ImageConversation[] {
  if (!Array.isArray(value)) return []

  return value
    .map((conversation): ImageConversation | null => {
      if (!conversation || typeof conversation !== 'object') return null
      const source = conversation as Record<string, any>
      const turns = Array.isArray(source.turns)
        ? source.turns
            .map(normalizeTurn)
            .filter((turn): turn is ImageTurn => turn !== null)
            .slice(-MAX_TURNS_PER_CONVERSATION)
        : []

      if (turns.length === 0) return null
      const createdAt = String(source.createdAt || turns[0]?.createdAt || new Date().toISOString())
      const updatedAt = String(source.updatedAt || turns[turns.length - 1]?.createdAt || createdAt)
      return {
        id: String(source.id || createImageConversationId()),
        title: String(source.title || buildImageConversationTitle(turns[0]?.prompt || '')),
        createdAt,
        updatedAt,
        turns,
      }
    })
    .filter((conversation): conversation is ImageConversation => conversation !== null)
    .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))
    .slice(0, MAX_CONVERSATIONS)
}

function normalizeTurn(value: unknown): ImageTurn | null {
  if (!value || typeof value !== 'object') return null
  const source = value as Record<string, any>
  const images = Array.isArray(source.images)
    ? source.images
        .map(normalizeImage)
        .filter((image): image is StoredGeneratedImage => image !== null)
    : []
  const createdAt = String(source.createdAt || new Date().toISOString())
  return {
    id: String(source.id || createImageConversationId()),
    prompt: String(source.prompt || ''),
    model: String(source.model || 'gpt-image-2'),
    mode: source.mode === 'edit' ? 'edit' : 'generate',
    referenceImages: normalizeReferenceImages(source.referenceImages),
    count: Math.max(1, Math.min(100, Math.floor(Number(source.count) || images.length || 1))),
    size: String(source.size || '1024x1024'),
    ratio: typeof source.ratio === 'string' ? source.ratio : ratioFromSize(String(source.size || '1024x1024')),
    tier: typeof source.tier === 'string' ? source.tier : tierFromSize(String(source.size || '1024x1024')),
    quality: String(source.quality || 'auto'),
    images,
    status: normalizeTurnStatus(source.status, images.length),
    error: typeof source.error === 'string' ? source.error : undefined,
    createdAt,
    promptDeleted: source.promptDeleted === true,
    resultsDeleted: source.resultsDeleted === true,
  }
}

function normalizeTurnStatus(value: unknown, imageCount: number): ImageTurnStatus {
  if (value === 'queued') return 'queued'
  if (value === 'generating') return 'generating'
  if (value === 'success') return 'success'
  if (value === 'error') return 'error'
  return imageCount > 0 ? 'success' : 'error'
}

function normalizeImage(value: unknown): StoredGeneratedImage | null {
  if (!value || typeof value !== 'object') return null
  const source = value as Record<string, any>
  const status: StoredGeneratedImageStatus =
    source.status === 'loading' ? 'loading' : source.status === 'error' ? 'error' : 'success'
  return {
    id: String(source.id || createImageConversationId()),
    status,
    b64_json: typeof source.b64_json === 'string' ? source.b64_json : undefined,
    url: typeof source.url === 'string' ? source.url : undefined,
    revised_prompt: typeof source.revised_prompt === 'string' ? source.revised_prompt : undefined,
    error: typeof source.error === 'string' ? source.error : undefined,
    durationMs: typeof source.durationMs === 'number' ? source.durationMs : undefined,
    startedAt: typeof source.startedAt === 'number' ? source.startedAt : undefined,
  }
}

function normalizeReferenceImages(value: unknown): StoredReferenceImage[] {
  if (!Array.isArray(value)) return []
  return value
    .filter((item): item is StoredReferenceImage => {
      return Boolean(item && typeof item === 'object' && typeof item.dataUrl === 'string' && item.dataUrl)
    })
    .map((item) => ({
      name: item.name || 'reference.png',
      type: item.type || 'image/png',
      dataUrl: item.dataUrl.length > MAX_REFERENCE_BYTES ? '' : item.dataUrl,
    }))
    .filter((item) => item.dataUrl)
    .slice(0, 4)
}

export function listImageConversations(): ImageConversation[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    return normalizeStoredConversations(JSON.parse(raw))
  } catch {
    return []
  }
}

export function saveImageConversations(conversations: ImageConversation[]): void {
  const normalized = normalizeStoredConversations(conversations)
  trySave(normalized)
}

export function renameImageConversation(id: string, title: string): ImageConversation[] {
  const nextTitle = title.trim()
  if (!nextTitle) return listImageConversations()
  const next = listImageConversations().map((conversation) =>
    conversation.id === id
      ? {
          ...conversation,
          title: nextTitle,
          updatedAt: new Date().toISOString(),
        }
      : conversation
  )
  trySave(next)
  return normalizeStoredConversations(next)
}

export function upsertImageConversation(conversation: ImageConversation, current: ImageConversation[] = listImageConversations()): ImageConversation[] {
  const next = normalizeStoredConversations([
    conversation,
    ...current.filter((item) => item.id !== conversation.id),
  ])
  trySave(next)
  return next
}

export function deleteImageConversation(id: string): ImageConversation[] {
  const next = listImageConversations().filter((conversation) => conversation.id !== id)
  trySave(next)
  return next
}

export function clearImageConversations(): void {
  localStorage.removeItem(STORAGE_KEY)
}

function trySave(conversations: ImageConversation[]): void {
  let next = compactImageConversations(normalizeStoredConversations(conversations))
  while (next.length > 0) {
    const serialized = JSON.stringify(next)
    if (serialized.length <= MAX_JSON_BYTES) {
      try {
        localStorage.setItem(STORAGE_KEY, serialized)
        return
      } catch {
        next = next.slice(0, -1)
        continue
      }
    }
    next = next.slice(0, -1)
  }
  try {
    localStorage.setItem(STORAGE_KEY, '[]')
  } catch {
    // Keep the previous history if the browser refuses even the empty payload.
  }
}

function compactImageConversations(conversations: ImageConversation[]): ImageConversation[] {
  return conversations
    .map((conversation) => ({
      ...conversation,
      turns: conversation.turns
        .map((turn) => ({
          ...turn,
          referenceImages: turn.referenceImages.map((image) => ({
            ...image,
            dataUrl: image.dataUrl.length > MAX_REFERENCE_BYTES ? '' : image.dataUrl,
          })).filter((image) => image.dataUrl),
          images: turn.images.map(compactGeneratedImage).filter(hasPersistableGeneratedImage),
        }))
        .filter(hasPersistableImageTurn),
    }))
    .filter((conversation) => conversation.turns.length > 0)
}

function compactGeneratedImage(image: StoredGeneratedImage): StoredGeneratedImage {
  const url = typeof image.url === 'string' && isPersistableImageURL(image.url) ? image.url : undefined
  const keepInlineImage = !url && typeof image.b64_json === 'string' && image.b64_json.length <= MAX_STORED_IMAGE_BASE64_BYTES
  return {
    id: image.id,
    status: image.status,
    url,
    revised_prompt: image.revised_prompt,
    error: image.error,
    b64_json: keepInlineImage ? image.b64_json : undefined,
    durationMs: image.durationMs,
    startedAt: image.startedAt,
  }
}

function hasPersistableGeneratedImage(image: StoredGeneratedImage): boolean {
  return image.status === 'loading' || Boolean(image.url || image.b64_json || image.error)
}

function hasPersistableImageTurn(turn: ImageTurn): boolean {
  return !turn.promptDeleted || !turn.resultsDeleted || turn.images.length > 0 || turn.status === 'error' || Boolean(turn.error)
}

function isPersistableImageURL(value: string): boolean {
  const trimmed = value.trim()
  return /^https?:\/\//i.test(trimmed) || trimmed.startsWith('/')
}

function ratioFromSize(size: string): string {
  const [rawWidth, rawHeight] = size.split('x').map((part) => Number(part))
  if (!Number.isFinite(rawWidth) || !Number.isFinite(rawHeight) || rawWidth <= 0 || rawHeight <= 0) {
    return '1:1'
  }
  const divisor = gcd(Math.round(rawWidth), Math.round(rawHeight))
  return `${Math.round(rawWidth / divisor)}:${Math.round(rawHeight / divisor)}`
}

function tierFromSize(size: string): string {
  const [rawWidth, rawHeight] = size.split('x').map((part) => Number(part))
  const longest = Math.max(rawWidth || 0, rawHeight || 0)
  if (longest >= 3840) return '4k'
  if (longest >= 2048) return '2k'
  return '1k'
}

function gcd(a: number, b: number): number {
  let x = Math.abs(a)
  let y = Math.abs(b)
  while (y) {
    const next = x % y
    x = y
    y = next
  }
  return x || 1
}
