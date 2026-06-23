import { afterEach, describe, expect, it } from 'vitest'
import {
  listImageConversations,
  renameImageConversation,
  upsertImageConversation,
  type ImageConversation,
} from '@/stores/imageConversations'

function conversationWithImage(image: Record<string, unknown>): ImageConversation {
  return {
    id: 'conversation-1',
    title: 'draw a cat',
    createdAt: '2026-06-02T06:00:00.000Z',
    updatedAt: '2026-06-02T06:00:00.000Z',
    turns: [
      {
        id: 'turn-1',
        prompt: 'draw a cat',
        model: 'gpt-image-2',
        mode: 'generate',
        referenceImages: [],
        count: 1,
        size: '1024x1024',
        ratio: '1:1',
        tier: '1k',
        quality: 'auto',
        status: 'success',
        createdAt: '2026-06-02T06:00:00.000Z',
        images: [
          {
            id: 'image-1',
            status: 'success',
            ...image,
          },
        ],
      },
    ],
  }
}

describe('image conversations storage', () => {
  afterEach(() => {
    localStorage.clear()
  })

  it('keeps remote URLs and drops large inline image data before saving history', () => {
    const largeInlineImage = 'a'.repeat(1_200_000)
    upsertImageConversation(conversationWithImage({
      b64_json: largeInlineImage,
      url: 'https://cdn.example/2026-06-02/test.png',
    }))

    const stored = listImageConversations()
    expect(stored).toHaveLength(1)
    expect(stored[0].turns[0].images[0].url).toBe('https://cdn.example/2026-06-02/test.png')
    expect(stored[0].turns[0].images[0].b64_json).toBeUndefined()
    expect(localStorage.getItem('sub2api:image_conversations:v1')).not.toContain(largeInlineImage)
  })

  it('does not persist data image URLs in history', () => {
    const inlineURL = 'data:image/png;base64,' + 'a'.repeat(120_000)
    upsertImageConversation(conversationWithImage({
      url: inlineURL,
    }))

    const stored = listImageConversations()
    expect(stored).toHaveLength(1)
    expect(stored[0].turns[0].images).toHaveLength(0)
    expect(localStorage.getItem('sub2api:image_conversations:v1')).not.toContain(inlineURL)
  })

  it('keeps loading image placeholders and deletion flags for active UI state', () => {
    upsertImageConversation({
      ...conversationWithImage({}),
      turns: [
        {
          ...conversationWithImage({}).turns[0],
          status: 'queued',
          promptDeleted: true,
          resultsDeleted: false,
          ratio: '16:9',
          tier: '2k',
          size: '2560x1440',
          images: [
            {
              id: 'loading-1',
              status: 'loading',
            },
          ],
        },
      ],
    })

    const stored = listImageConversations()
    expect(stored).toHaveLength(1)
    expect(stored[0].turns[0].status).toBe('queued')
    expect(stored[0].turns[0].promptDeleted).toBe(true)
    expect(stored[0].turns[0].resultsDeleted).toBe(false)
    expect(stored[0].turns[0].ratio).toBe('16:9')
    expect(stored[0].turns[0].tier).toBe('2k')
    expect(stored[0].turns[0].images[0].status).toBe('loading')
  })

  it('renames a conversation without dropping its turns', () => {
    upsertImageConversation(conversationWithImage({ url: 'https://cdn.example/image.png' }))

    const renamed = renameImageConversation('conversation-1', 'new title')

    expect(renamed[0].title).toBe('new title')
    expect(listImageConversations()[0].turns).toHaveLength(1)
  })
})
