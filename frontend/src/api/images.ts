import { getLocale } from '@/i18n'

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || '/api/v1'

export interface GatewayModel {
  id: string
  object?: string
  type?: string
  display_name?: string
  owned_by?: string
}

export interface GatewayModelsResponse {
  object?: string
  data: GatewayModel[]
}

export interface ImageResponseItem {
  b64_json?: string
  url?: string
  revised_prompt?: string
}

export interface ImageResponse {
  created?: number
  data: ImageResponseItem[]
}

export interface ImageRequestOptions {
  apiKey: string
  baseUrl?: string
  signal?: AbortSignal
}

export interface ImageGenerationRequest extends ImageRequestOptions {
  prompt: string
  model: string
  size?: string
  quality?: string
  n?: number
}

export interface ImageEditRequest extends ImageGenerationRequest {
  files: File[]
}

export class ImageGatewayError extends Error {
  status: number
  payload: unknown

  constructor(message: string, status: number, payload: unknown) {
    super(message)
    this.name = 'ImageGatewayError'
    this.status = status
    this.payload = payload
  }
}

export function normalizeGatewayBaseUrl(rawBaseUrl?: string): string {
  const raw = String(rawBaseUrl || '').trim()
  if (!raw) return normalizeGatewayBaseUrl(API_BASE_URL)

  const trimmed = raw.replace(/\/+$/, '')
  if (/\/api\/v1$/i.test(trimmed)) {
    return trimmed.replace(/\/api\/v1$/i, '/v1')
  }
  if (/\/v1$/i.test(trimmed)) {
    return trimmed
  }
  return `${trimmed}/v1`
}

export function buildGatewayUrl(path: string, baseUrl?: string): string {
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  const base = normalizeGatewayBaseUrl(baseUrl)
  const suffix = normalizedPath.replace(/^\/v1/i, '')
  return `${base}${suffix.startsWith('/') ? suffix : `/${suffix}`}`
}

export function isOpenAIImageModel(modelID: string): boolean {
  return /^gpt-image-/i.test(String(modelID || '').trim())
}

export function filterImageModelIDs(models: GatewayModel[]): string[] {
  const seen = new Set<string>()
  const ids: string[] = []
  for (const model of models || []) {
    const id = String(model?.id || '').trim()
    if (!id || seen.has(id) || !isOpenAIImageModel(id)) continue
    seen.add(id)
    ids.push(id)
  }
  return ids
}

export function extractImageGatewayErrorMessage(payload: unknown, fallback = 'Image request failed'): string {
  if (!payload || typeof payload !== 'object') {
    return fallback
  }
  const data = payload as Record<string, any>
  const candidates = [
    data?.error?.message,
    data?.message,
    data?.detail,
    data?.error,
  ]
  for (const item of candidates) {
    if (typeof item === 'string' && item.trim()) {
      return item.trim()
    }
  }
  return fallback
}

async function parseGatewayResponse<T>(response: Response): Promise<T> {
  const text = await response.text()
  let payload: unknown = null
  if (text.trim()) {
    try {
      payload = JSON.parse(text)
    } catch {
      payload = { message: text }
    }
  }

  if (!response.ok) {
    throw new ImageGatewayError(
      extractImageGatewayErrorMessage(payload, response.statusText || 'Image request failed'),
      response.status,
      payload
    )
  }

  return payload as T
}

function authHeaders(apiKey: string, contentType?: string): HeadersInit {
  const headers: Record<string, string> = {
    Authorization: `Bearer ${apiKey}`,
    Accept: 'application/json',
    'Accept-Language': getLocale(),
  }
  if (contentType) {
    headers['Content-Type'] = contentType
  }
  return headers
}

export async function listImageModels(options: ImageRequestOptions): Promise<string[]> {
  const response = await fetch(buildGatewayUrl('/v1/models', options.baseUrl), {
    method: 'GET',
    headers: authHeaders(options.apiKey),
    credentials: 'same-origin',
    signal: options.signal,
  })
  const payload = await parseGatewayResponse<GatewayModelsResponse>(response)
  return filterImageModelIDs(Array.isArray(payload.data) ? payload.data : [])
}

export async function createImageGeneration(options: ImageGenerationRequest): Promise<ImageResponse> {
  const body = {
    prompt: options.prompt,
    model: options.model,
    ...(options.size ? { size: options.size } : {}),
    ...(options.quality ? { quality: options.quality } : {}),
    n: Math.max(1, Math.floor(options.n || 1)),
    response_format: 'url',
  }

  const response = await fetch(buildGatewayUrl('/v1/images/generations', options.baseUrl), {
    method: 'POST',
    headers: authHeaders(options.apiKey, 'application/json'),
    credentials: 'same-origin',
    body: JSON.stringify(body),
    signal: options.signal,
  })
  return parseGatewayResponse<ImageResponse>(response)
}

export async function createImageEdit(options: ImageEditRequest): Promise<ImageResponse> {
  const form = new FormData()
  for (const file of options.files) {
    form.append('image', file)
  }
  form.append('prompt', options.prompt)
  form.append('model', options.model)
  if (options.size) form.append('size', options.size)
  if (options.quality) form.append('quality', options.quality)
  form.append('n', String(Math.max(1, Math.floor(options.n || 1))))
  form.append('response_format', 'url')

  const response = await fetch(buildGatewayUrl('/v1/images/edits', options.baseUrl), {
    method: 'POST',
    headers: authHeaders(options.apiKey),
    credentials: 'same-origin',
    body: form,
    signal: options.signal,
  })
  return parseGatewayResponse<ImageResponse>(response)
}

export const imagesAPI = {
  buildGatewayUrl,
  normalizeGatewayBaseUrl,
  listImageModels,
  createImageGeneration,
  createImageEdit,
}

export default imagesAPI
