// frontend/src/views/admin/ops/utils/errorExport.ts
import type { OpsErrorDetail } from '@/api/admin/ops'
import type { ErrorAnalysisResult, ErrorAnalysisAccountDiagnostic } from './errorAnalysis'
import { accountDisplayLabel, accountRoutingSummary, formatSchedulerGateCounts } from './errorAnalysis'
import { resolveUpstreamPayload } from './errorDetailResponse'

const SEPARATOR = '─'.repeat(48)
const DOUBLE_SEPARATOR = '='.repeat(60)

export interface ErrorExportData {
  detail: OpsErrorDetail
  analysis: ErrorAnalysisResult
  schedulerDiagnostics?: ErrorAnalysisAccountDiagnostic[]
  upstreamErrors?: OpsErrorDetail[]
  version?: string
}

function padRight(str: string, len: number): string {
  if (str.length >= len) return str
  return str + ' '.repeat(len - str.length)
}

function formatField(label: string, value: string | number | null | undefined): string {
  const display = value != null && value !== '' ? String(value) : '—'
  return `  ${padRight(label, 20)}${display}`
}

function formatSection(title: string, lines: string[]): string {
  return `\n${SEPARATOR}\n  ${title}\n${SEPARATOR}\n${lines.join('\n')}\n`
}

function formatAnalysisSteps(analysis: ErrorAnalysisResult): string[] {
  const lines: string[] = ['  诊断步骤:']
  for (let i = 0; i < analysis.steps.length; i++) {
    const step = analysis.steps[i]
    const stateLabel = step.state === 'passed' ? '通过'
      : step.state === 'failed' ? '失败'
      : step.state === 'warning' ? '异常'
      : step.state === 'skipped' ? '跳过'
      : '未确认'
    lines.push(`    #${i + 1}  ${padRight(step.key, 24)}[${stateLabel}]`)
  }
  return lines
}

function formatSchedulerDiagnostics(diagnostics: ErrorAnalysisAccountDiagnostic[], detail: OpsErrorDetail): string[] {
  if (diagnostics.length === 0) return []
  const available = diagnostics.filter(d => d.available).length
  const lines: string[] = [
    '  调度账户诊断:',
    `  分组账户: ${available}/${diagnostics.length} 可用`,
    ''
  ]
  for (const diag of diagnostics) {
    const status = diag.available ? '[可用]' : '[不可用]'
    const label = accountDisplayLabel(diag.account)
    lines.push(`  ${status} ${label} (#${diag.account.id})`)
    lines.push(`    平台: ${diag.account.platform} | 状态: ${diag.account.status}`)
    lines.push(`    路由: ${accountRoutingSummary(diag.account, detail)}`)
    if (diag.reasons.length > 0) {
      for (const reason of diag.reasons) {
        lines.push(`    原因: ${reason.key}${reason.detail ? ': ' + reason.detail : ''}`)
      }
    } else {
      lines.push('    无阻断原因')
    }
    lines.push('')
  }
  return lines
}

function formatSchedulerGateDiagnostics(analysis: ErrorAnalysisResult): string[] {
  const diagnostics = analysis.schedulerGateDiagnostics
  if (!diagnostics) return []
  const lines = [
    '  调度器 Gate 诊断:',
    `  主阻断门: ${diagnostics.primaryGate || '—'}`,
    `  Gate 计数: ${formatSchedulerGateCounts(diagnostics)}`
  ]
  if (diagnostics.freshDBRetry != null) {
    lines.push(`  Fresh DB 重试: ${diagnostics.freshDBRetry ? 'true' : 'false'}${diagnostics.freshDBRetryReason ? ` (${diagnostics.freshDBRetryReason})` : ''}`)
  }
  if (diagnostics.sampleRejectedAccounts.length > 0) {
    lines.push(`  样例拒绝账号: ${diagnostics.sampleRejectedAccounts.join(', ')}`)
  }
  if (diagnostics.raw) {
    lines.push(`  原始诊断: ${diagnostics.raw}`)
  }
  return lines
}

function formatUpstreamErrors(errors: OpsErrorDetail[]): string[] {
  if (errors.length === 0) return []
  const lines: string[] = ['  上游尝试记录:']
  for (let i = 0; i < errors.length; i++) {
    const ev = errors[i]
    const upstreamStatus = ev.upstream_status_code ?? ev.status_code ?? '—'
    lines.push(`  #${i + 1}  状态码: ${upstreamStatus}  |  账户: ${ev.account_name || ev.account_id || '—'}`)
    if (ev.upstream_endpoint) lines.push(`      上游端点: ${ev.upstream_endpoint}`)
    if (ev.upstream_error_message) lines.push(`      上游消息: ${ev.upstream_error_message}`)
    if (ev.upstream_error_detail) lines.push(`      上游详情: ${ev.upstream_error_detail}`)
    if (ev.message) lines.push(`      ${ev.message}`)
    const upstreamPayload = resolveUpstreamPayload(ev)
    if (upstreamPayload) {
      lines.push('      上游响应:')
      lines.push(prettyJSON(upstreamPayload))
    }
    lines.push('')
  }
  return lines
}

function formatPrimaryUpstreamFeedback(detail: OpsErrorDetail): string[] {
  const upstreamPayload = resolveUpstreamPayload(detail)
  const hasFeedback = detail.upstream_status_code != null ||
    !!String(detail.upstream_error_message || '').trim() ||
    !!String(detail.upstream_error_detail || '').trim() ||
    !!String(upstreamPayload || '').trim()
  if (!hasFeedback) return []
  const lines = [
    formatField('上游状态码:', detail.upstream_status_code),
    formatField('上游端点:', detail.upstream_endpoint),
    formatField('上游错误消息:', detail.upstream_error_message),
    formatField('上游错误详情:', detail.upstream_error_detail),
  ]
  if (upstreamPayload) {
    lines.push('')
    lines.push('  上游反馈体:')
    lines.push(prettyJSON(upstreamPayload))
  }
  return lines
}

function formatRequestType(type: number | null | undefined): string {
  switch (type) {
    case 1: return '同步请求'
    case 2: return '流式请求'
    case 3: return 'WebSocket'
    default: return '未知'
  }
}

function formatYesNo(value: boolean | null | undefined): string {
  return value ? '是' : '否'
}

function prettyJSON(raw?: string): string {
  if (!raw) return 'N/A'
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

export function buildSingleErrorTXT(data: ErrorExportData): string {
  const { detail, analysis, schedulerDiagnostics, upstreamErrors, version } = data
  const now = new Date().toLocaleString('zh-CN', { hour12: false })
  const requestId = detail.request_id || detail.client_request_id || '—'

  const sections: string[] = []

  // Header
  sections.push(`${DOUBLE_SEPARATOR}`)
  sections.push(`  LightBridge 错误报告`)
  sections.push(`  导出时间: ${now}`)
  if (version) sections.push(`  系统版本: ${version}`)
  sections.push(DOUBLE_SEPARATOR)

  // Basic info
  sections.push(formatSection('基本信息', [
    formatField('错误ID:', detail.id),
    formatField('请求ID:', requestId),
    formatField('客户端请求ID:', detail.client_request_id),
    formatField('发生时间:', detail.created_at),
    formatField('错误阶段:', detail.phase),
    formatField('错误类型:', detail.type),
    formatField('错误归属:', detail.error_owner),
    formatField('错误来源:', detail.error_source),
    formatField('严重程度:', detail.severity),
    formatField('状态码:', detail.status_code),
    formatField('已解决:', detail.resolved ? '是' : '否'),
  ]))

  // Device/instance info
  sections.push(formatSection('设备/实例信息', [
    formatField('入站端点:', detail.inbound_endpoint),
    formatField('出站端点:', detail.upstream_endpoint),
    formatField('平台:', detail.platform),
    formatField('模型:', detail.model),
    formatField('请求模型:', detail.requested_model),
    formatField('上游模型:', detail.upstream_model),
    formatField('用户代理:', detail.user_agent),
    formatField('请求类型:', formatRequestType(detail.request_type)),
  ]))

  // User/account info
  sections.push(formatSection('用户/账户信息', [
    formatField('用户ID:', detail.user_id),
    formatField('用户邮箱:', detail.user_email),
    formatField('账户ID:', detail.account_id),
    formatField('账户名称:', detail.account_name),
    formatField('分组ID:', detail.group_id),
    formatField('分组名称:', detail.group_name),
    formatField('客户端IP:', detail.client_ip),
    formatField('请求路径:', detail.request_path),
    formatField('流式传输:', formatYesNo(detail.stream)),
  ]))

  // Error details
  sections.push(formatSection('错误详情', [
    formatField('错误消息:', detail.message),
    formatField('上游状态码:', detail.upstream_status_code),
    formatField('上游错误消息:', detail.upstream_error_message),
    formatField('上游错误详情:', detail.upstream_error_detail),
    '',
    '  响应体:',
    prettyJSON(detail.error_body),
  ]))

  const primaryUpstreamFeedback = formatPrimaryUpstreamFeedback(detail)
  if (primaryUpstreamFeedback.length > 0) {
    sections.push(formatSection('上游错误反馈', primaryUpstreamFeedback))
  }

  // Latency info
  sections.push(formatSection('延迟信息', [
    formatField('认证延迟:', detail.auth_latency_ms != null ? `${detail.auth_latency_ms}ms` : null),
    formatField('路由延迟:', detail.routing_latency_ms != null ? `${detail.routing_latency_ms}ms` : null),
    formatField('上游延迟:', detail.upstream_latency_ms != null ? `${detail.upstream_latency_ms}ms` : null),
    formatField('响应延迟:', detail.response_latency_ms != null ? `${detail.response_latency_ms}ms` : null),
    formatField('首Token延迟:', detail.time_to_first_token_ms != null ? `${detail.time_to_first_token_ms}ms` : null),
    formatField('业务限流:', formatYesNo(detail.is_business_limited)),
  ]))

  // Analysis result
  const analysisLines: string[] = [
    formatField('根因判断:', analysis.rootCause),
    formatField('根因模块:', analysis.rootModule),
    formatField('置信度:', analysis.confidence),
    '',
    ...formatAnalysisSteps(analysis),
    '',
    ...formatSchedulerGateDiagnostics(analysis),
    ...(analysis.schedulerGateDiagnostics ? [''] : []),
    '  证据:',
    ...analysis.evidence.map(ev => `    - ${ev.key}: ${ev.value}`),
    '',
    '  建议:',
    ...analysis.suggestionKeys.map(key => `    - ${key}`),
  ]
  sections.push(formatSection('分析结果', analysisLines))

  // Scheduler diagnostics
  if (schedulerDiagnostics && schedulerDiagnostics.length > 0) {
    sections.push(formatSection('调度账户诊断', formatSchedulerDiagnostics(schedulerDiagnostics, detail)))
  }

  // Upstream attempts
  if (upstreamErrors && upstreamErrors.length > 0) {
    sections.push(formatSection('上游尝试记录', formatUpstreamErrors(upstreamErrors)))
  }

  return sections.join('\n')
}

function downloadTXT(content: string, filename: string) {
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

export function exportSingleErrorTXT(data: ErrorExportData) {
  const content = buildSingleErrorTXT(data)
  const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19)
  downloadTXT(content, `error-report-${data.detail.id}-${timestamp}.txt`)
}

export function exportBatchErrorsTXT(dataList: ErrorExportData[], version?: string) {
  const parts = dataList.map((data, idx) => {
    if (idx > 0) {
      return '\n' + DOUBLE_SEPARATOR + '\n' + DOUBLE_SEPARATOR + '\n' + buildSingleErrorTXT(data)
    }
    return buildSingleErrorTXT({ ...data, version })
  })
  const content = parts.join('\n')
  const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19)
  downloadTXT(content, `error-report-${timestamp}.txt`)
}
