import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

describe('images api', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.stubEnv('VITE_API_BASE_URL', '')
    localStorage.setItem('sub2api_locale', 'en')
  })

  afterEach(() => {
    vi.unstubAllEnvs()
    vi.restoreAllMocks()
    localStorage.clear()
  })

  it('normalizes configured API URLs to gateway /v1 URLs', async () => {
    const { buildGatewayUrl } = await import('@/api/images')

    expect(buildGatewayUrl('/v1/models', 'https://api.example.com/api/v1')).toBe('https://api.example.com/v1/models')
    expect(buildGatewayUrl('/v1/images/generations', 'https://api.example.com/base')).toBe(
      'https://api.example.com/base/v1/images/generations'
    )
    expect(buildGatewayUrl('/v1/images/generations')).toBe('/v1/images/generations')
  })

  it('uses the Vite API base when the app is served from a prefixed API origin', async () => {
    vi.resetModules()
    vi.stubEnv('VITE_API_BASE_URL', 'https://sub2api.example.com/api/v1')
    const { buildGatewayUrl } = await import('@/api/images')

    expect(buildGatewayUrl('/v1/images/generations')).toBe('https://sub2api.example.com/v1/images/generations')
  })

  it('filters OpenAI image models from /v1/models', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({
      object: 'list',
      data: [
        { id: 'gpt-5.4' },
        { id: 'gpt-image-1' },
        { id: 'gpt-image-2' },
        { id: 'gpt-image-2' },
      ],
    })))
    vi.stubGlobal('fetch', fetchMock)
    const { listImageModels } = await import('@/api/images')

    await expect(listImageModels({ apiKey: 'sk-selected' })).resolves.toEqual(['gpt-image-1', 'gpt-image-2'])
    expect(fetchMock).toHaveBeenCalledWith(
      '/v1/models',
      expect.objectContaining({
        method: 'GET',
        headers: expect.objectContaining({
          Authorization: 'Bearer sk-selected',
        }),
      })
    )
  })

  it('sends generation requests with the selected API key and image payload', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ data: [{ b64_json: 'abc' }] })))
    vi.stubGlobal('fetch', fetchMock)
    const { createImageGeneration } = await import('@/api/images')

    await createImageGeneration({
      apiKey: 'sk-image',
      prompt: 'draw a cat',
      model: 'gpt-image-2',
      size: '1024x1024',
      quality: 'high',
      n: 2,
    })

    const [, init] = fetchMock.mock.calls[0]
    expect(init.headers.Authorization).not.toBe('Bearer jwt-token')
    expect(init.headers.Authorization).toBe('Bearer sk-image')
    expect(JSON.parse(init.body)).toEqual({
      prompt: 'draw a cat',
      model: 'gpt-image-2',
      size: '1024x1024',
      quality: 'high',
      n: 2,
      response_format: 'url',
    })
  })

  it('sends edit requests as multipart with images and selected API key', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ data: [{ b64_json: 'abc' }] })))
    vi.stubGlobal('fetch', fetchMock)
    const { createImageEdit } = await import('@/api/images')
    const file = new File(['x'], 'ref.png', { type: 'image/png' })

    await createImageEdit({
      apiKey: 'sk-edit',
      files: [file],
      prompt: 'change background',
      model: 'gpt-image-2',
      size: '1024x1024',
      quality: 'medium',
      n: 1,
    })

    const [, init] = fetchMock.mock.calls[0]
    expect(init.headers.Authorization).toBe('Bearer sk-edit')
    expect(init.headers['Content-Type']).toBeUndefined()
    expect(init.body).toBeInstanceOf(FormData)
    const form = init.body as FormData
    expect(form.get('prompt')).toBe('change background')
    expect(form.get('model')).toBe('gpt-image-2')
    expect(form.get('size')).toBe('1024x1024')
    expect(form.get('quality')).toBe('medium')
    expect(form.get('image')).toBe(file)
  })
})
