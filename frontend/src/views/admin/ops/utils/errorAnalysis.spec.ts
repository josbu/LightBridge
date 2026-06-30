import { describe, expect, it } from 'vitest'
import type { OpsErrorDetail } from '@/api/admin/ops'
import type { Account } from '@/types'
import { buildErrorAnalysis, diagnoseSchedulerAccount } from './errorAnalysis'

function makeDetail(overrides: Partial<OpsErrorDetail> = {}): OpsErrorDetail {
  return {
    id: 1,
    created_at: '2026-06-25T10:00:00Z',
    phase: 'routing',
    type: 'api_error',
    error_owner: 'platform',
    error_source: 'gateway',
    severity: 'P1',
    status_code: 503,
    platform: 'custom',
    model: 'gpt-4o-mini',
    resolved: false,
    client_request_id: 'client-req-1',
    request_id: 'req-1',
    message: 'No available accounts',
    user_email: 'user@example.com',
    account_name: '',
    group_name: 'default',
    stream: false,
    inbound_endpoint: '/v1/chat/completions',
    upstream_endpoint: '',
    requested_model: 'gpt-4o-mini',
    upstream_model: '',
    request_type: 1,
    error_body: '{"error":{"message":"No available accounts"}}',
    user_agent: 'vitest',
    is_business_limited: false,
    ...overrides
  }
}

function makeAccount(overrides: Partial<Account> = {}): Account {
  return {
    id: 42,
    name: 'primary',
    platform: 'custom' as any,
    type: 'api_key' as any,
    credentials: {},
    proxy_id: null,
    concurrency: 2,
    current_concurrency: 0,
    priority: 1,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: false,
    created_at: '2026-06-25T10:00:00Z',
    updated_at: '2026-06-25T10:00:00Z',
    group_ids: [7],
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides
  }
}

describe('buildErrorAnalysis', () => {
  it('classifies 503 No Available Account as account scheduler failure before upstream', () => {
    const analysis = buildErrorAnalysis(makeDetail(), [])

    expect(analysis.rootCause).toBe('no_available_account')
    expect(analysis.rootModule).toBe('openai_account_scheduler')
    expect(analysis.confidence).toBe('high')
    expect(analysis.failedStep).toBe('account_scheduler')
    expect(analysis.steps.find((step) => step.key === 'account_scheduler')?.state).toBe('failed')
    expect(analysis.steps.find((step) => step.key === 'provider_adapter')?.state).toBe('skipped')
    expect(analysis.steps.find((step) => step.key === 'upstream')?.state).toBe('skipped')
    expect(analysis.suggestionKeys).toContain('customNoUpstreamAttempt')
  })

  it('classifies 403 auth phase as auth failure', () => {
    const analysis = buildErrorAnalysis(makeDetail({
      phase: 'auth',
      error_owner: 'client',
      error_source: 'client_request',
      status_code: 403,
      platform: 'openai',
      message: 'forbidden',
      error_body: '{"error":{"message":"forbidden"}}'
    }), [])

    expect(analysis.rootCause).toBe('auth_forbidden')
    expect(analysis.rootModule).toBe('middleware.api_key_auth')
    expect(analysis.failedStep).toBe('auth')
    expect(analysis.steps.find((step) => step.key === 'auth')?.state).toBe('failed')
  })

  it('classifies correlated upstream errors as provider upstream failure', () => {
    const detail = makeDetail({
      phase: 'upstream',
      error_owner: 'provider',
      error_source: 'upstream_http',
      status_code: 502,
      platform: 'openai',
      account_id: 42,
      account_name: 'primary',
      message: 'upstream bad gateway',
      error_body: '{"error":{"message":"upstream bad gateway"}}',
      upstream_status_code: 502,
      upstream_error_message: 'bad gateway'
    })
    const upstream = makeDetail({
      id: 2,
      phase: 'upstream',
      error_owner: 'provider',
      error_source: 'upstream_http',
      status_code: 502,
      account_id: 42,
      account_name: 'primary',
      message: 'provider returned 502',
      error_body: '{"error":{"message":"provider returned 502"}}'
    })

    const analysis = buildErrorAnalysis(detail, [upstream])

    expect(analysis.rootCause).toBe('provider_upstream')
    expect(analysis.failedStep).toBe('upstream')
    expect(analysis.confidence).toBe('high')
    expect(analysis.steps.find((step) => step.key === 'account_scheduler')?.state).toBe('passed')
    expect(analysis.steps.find((step) => step.key === 'upstream')?.state).toBe('failed')
  })

  it('explains why a scheduler candidate account is unavailable', () => {
    const detail = makeDetail({ group_id: 7, requested_model: 'gpt-4o-mini' })
    const diagnostic = diagnoseSchedulerAccount(makeAccount({
      status: 'error',
      error_message: 'invalid token',
      schedulable: false,
      rate_limit_reset_at: '2026-06-25T10:30:00Z',
      current_concurrency: 2,
      extra: { model_whitelist: ['gpt-4o'] }
    }), detail, new Date('2026-06-25T10:00:00Z').getTime())

    expect(diagnostic.available).toBe(false)
    expect(diagnostic.reasons.map((reason) => reason.key)).toEqual(expect.arrayContaining([
      'status_error',
      'unschedulable',
      'rate_limited',
      'concurrency_full',
      'model_not_allowed'
    ]))
  })

  it('does not treat group account platform differences as scheduler blockers', () => {
    const detail = makeDetail({ group_id: 7, platform: 'openai' })
    const diagnostic = diagnoseSchedulerAccount(makeAccount({ platform: 'module' as any }), detail)

    expect(diagnostic.available).toBe(true)
    expect(diagnostic.reasons.map((reason) => reason.key)).not.toContain('platform_mismatch')
  })
})
