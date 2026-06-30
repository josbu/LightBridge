package i18n

// catalog maps a target language to a lookup table keyed by the canonical
// English message. English is the source language and therefore has no table —
// Translate returns the key verbatim for LangEN.
//
// Keys must match the strings emitted by the gateway handlers exactly. Format
// keys (used via Translatef) keep their verbs, e.g. "No available accounts: %s".
var catalog = map[Lang]map[string]string{
	LangZH: zhMessages,
}

// zhMessages holds Simplified Chinese translations for the error messages the
// gateway returns to API clients.
var zhMessages = map[string]string{
	// Authentication / request validation
	"Invalid API key":                          "API Key 无效",
	"User context not found":                   "未找到用户上下文",
	"Failed to read request body":              "读取请求体失败",
	"Failed to parse request body":             "解析请求体失败",
	"Failed to normalize compact request body": "规范化 compact 请求体失败",
	"Request body is empty":                    "请求体为空",
	"model is required":                        "缺少必填参数 model",
	"invalid stream field type":                "stream 字段类型无效",
	"Request body too large, limit is %s":      "请求体过大，上限为 %s",

	// Account availability / scheduling
	"No available accounts":                                   "无可用账号",
	"No available accounts: %s":                               "无可用账号：%s",
	"No available compatible accounts":                        "无可用的兼容账号",
	"No available Gemini accounts":                            "无可用的 Gemini 账号",
	"No available Gemini accounts: %s":                        "无可用的 Gemini 账号：%s",
	"No available OpenAI accounts support /responses/compact": "没有支持 /responses/compact 的可用 OpenAI 账号",
	"All available accounts exhausted":                        "所有可用账号均已尝试失败",

	// Concurrency / rate limiting
	"Too many pending requests, please retry later":                   "等待处理的请求过多，请稍后重试",
	"Concurrency limit exceeded for %s, please retry later":           "%s 并发数已超限，请稍后重试",
	"Image generation concurrency limit exceeded, please retry later": "图像生成并发数已超限，请稍后重试",

	// Service availability
	"Service temporarily unavailable":                              "服务暂时不可用",
	"Service temporarily unavailable, please retry later":          "服务暂时不可用，请稍后重试",
	"Billing service temporarily unavailable. Please retry later.": "计费服务暂时不可用，请稍后重试。",
	"context canceled": "请求已被取消",

	// Upstream errors (the 401/403/429/5xx mapping — the headline 502/503/429 cases)
	"Upstream request failed":                                                                "上游请求失败",
	"Upstream authentication failed, please contact administrator":                           "上游鉴权失败，请联系管理员",
	"Upstream access forbidden, please contact administrator":                                "上游拒绝访问，请联系管理员",
	"Upstream rate limit exceeded, please retry later":                                       "上游触发限流，请稍后重试",
	"Upstream service overloaded, please retry later":                                        "上游服务过载，请稍后重试",
	"Upstream service temporarily unavailable":                                               "上游服务暂时不可用",
	"Empty upstream response":                                                                "上游返回空响应",
	"Upstream returned an empty completion without usage; no fallback account was available": "上游返回了空响应且无用量信息，且没有可用的回退账号",

	// Gemini / OpenAI specific
	"Gemini compatibility service is not configured":                        "Gemini 兼容服务未配置",
	"Failed to get user info":                                               "获取用户信息失败",
	"Missing model in URL":                                                  "URL 中缺少 model",
	"previous_response_id is only supported on Responses WebSocket v2":      "previous_response_id 仅在 Responses WebSocket v2 上受支持",
	"previous_response_id must be a response.id (resp_*), not a message id": "previous_response_id 必须是 response.id（resp_*），而不是 message id",
	"WebSocket upgrade required (Upgrade: websocket)":                       "需要升级为 WebSocket 连接（Upgrade: websocket）",
}
