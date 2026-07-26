import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

import enLanding from '../../i18n/locales/en/landing'
import zhLanding from '../../i18n/locales/zh/landing'

type HomeLanding = {
  home: {
    heroDescription: string
    providers?: unknown
  }
}

const homeViewPath = resolve(dirname(fileURLToPath(import.meta.url)), '../HomeView.vue')
const providerNames = /Claude|OpenAI|GPT|Gemini|Antigravity/i

describe('HomeView landing customization', () => {
  it('does not render the provider stats section on the public home page', () => {
    const source = readFileSync(homeViewPath, 'utf8')

    expect(source).not.toContain('home.providers')
    expect(source).not.toContain('Supported Providers')
  })

  it('does not keep provider stats copy in landing locales', () => {
    const zhHome = (zhLanding as HomeLanding).home
    const enHome = (enLanding as HomeLanding).home

    expect(zhHome.providers).toBeUndefined()
    expect(enHome.providers).toBeUndefined()
    expect(zhHome.heroDescription).not.toMatch(providerNames)
    expect(enHome.heroDescription).not.toMatch(providerNames)
  })
})
