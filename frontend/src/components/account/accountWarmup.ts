export interface AccountWarmupConfig {
  enabled: boolean
  model: string
  models: string[]
  concurrency: number
  prompt: string
  success_threshold: number
}

export interface AccountWarmupModelOption {
  id: string
  display_name?: string
}

export interface AccountWarmupState {
  status: 'disabled' | 'pending' | 'running' | 'cooling' | 'failed' | 'completed'
  success_count: number
  success_threshold: number
  failure_count: number
  last_error: string
  completed_at?: string | null
}

export function readAccountWarmup(extra?: Record<string, unknown>): AccountWarmupConfig {
  const cfg = extra?.openai_warmup as Partial<AccountWarmupConfig> | undefined
  const configuredModels = Array.isArray(cfg?.models)
    ? cfg.models.filter((model): model is string => typeof model === 'string' && model.trim().length > 0).map((model) => model.trim())
    : []
  const models = configuredModels.length > 0
    ? configuredModels
    : (typeof cfg?.model === 'string' && cfg.model.trim() ? [cfg.model.trim()] : ['gpt-5.6-sol'])
  return {
    enabled: cfg?.enabled === true,
    model: models[0],
    models,
    concurrency: cfg?.concurrency ?? 1,
    prompt: cfg?.prompt ?? '',
    success_threshold: cfg?.success_threshold ?? 0
  }
}

export function normalizeAccountWarmup(config: AccountWarmupConfig): AccountWarmupConfig {
  const models = config.models.filter((model) => model.trim().length > 0).map((model) => model.trim())
  const normalizedModels = models.length > 0 ? models : [config.model.trim() || 'gpt-5.6-sol']
  return { ...config, model: normalizedModels[0], models: normalizedModels }
}

export function readAccountWarmupState(extra?: Record<string, unknown>): AccountWarmupState | undefined {
  return extra?.openai_warmup_state as AccountWarmupState | undefined
}
