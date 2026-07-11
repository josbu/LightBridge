package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

func stringSliceFromRaw(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func (a *Account) IsRateLimited() bool {
	if a.RateLimitResetAt == nil {
		return false
	}
	return time.Now().Before(*a.RateLimitResetAt)
}

func (a *Account) IsOverloaded() bool {
	if a.OverloadUntil == nil {
		return false
	}
	return time.Now().Before(*a.OverloadUntil)
}

func (a *Account) IsOAuth() bool {
	return a.Type == AccountTypeOAuth || a.Type == AccountTypeSetupToken
}

// IsPrivacySet 检查账号的 privacy 是否已成功设置。
// OpenAI: privacy_mode == "training_off"
// Antigravity: privacy_mode == "privacy_set"
// 其他平台: 无 privacy 概念，始终返回 true
func (a *Account) IsPrivacySet() bool {
	if a.IsAntigravity() {
		return a.getExtraString("privacy_mode") == AntigravityPrivacySet
	}
	switch a.Platform {
	case PlatformOpenAI:
		return a.getExtraString("privacy_mode") == PrivacyModeTrainingOff
	default:
		return true
	}
}

func (a *Account) IsGemini() bool {
	return a.Platform == PlatformGemini || (a.IsCustom() && a.CustomProtocol() == CustomProtocolGemini)
}

// IsAntigravity 报告该账号是否为 Antigravity 账号。
// 合并后 Antigravity 账号的 Platform=="gemini"，通过 SubPlatform=="antigravity" 判别，
// 与 Type（oauth/apikey/upstream）正交。这是判断 Antigravity 身份的唯一权威方式。
//
// 为兼容尚未迁移的历史数据/外部构造对象，同时容忍旧的 Platform=="antigravity" 取值
// （归一化逻辑会在写入时将其转换为 gemini + sub_platform，但内存对象可能仍带旧值）。
func (a *Account) IsAntigravity() bool {
	return a.SubPlatform == SubPlatformAntigravity || a.Platform == PlatformAntigravity
}

// IsPureGemini 报告该账号是否为原生 Gemini 账号（platform==gemini 且非 Antigravity）。
// 用于那些只应作用于原生 Gemini、而不应作用于 Antigravity 的逻辑
// （如 Code Assist、AI Studio、Gemini 专属模型映射等）。
func (a *Account) IsPureGemini() bool {
	return a.Platform == PlatformGemini && !a.IsAntigravity()
}

// EffectivePlatform 返回账号对外暴露的平台标识（别名）。
// Antigravity 账号在数据库中 Platform=="gemini"，但对外（配额归因、展示、
// 向后兼容路由）仍呈现为 "antigravity"。其他账号返回其真实 Platform
// （Custom 账号返回 "custom"）。
func (a *Account) EffectivePlatform() string {
	if a.IsAntigravity() {
		return PlatformAntigravity
	}
	return a.Platform
}

// IsCustom 报告该账号是否为「自定义 Provider」账号（platform=="custom"）。
// Custom 账号通过自定义 base_url + api_key 连接任意上游，并由 CustomProtocol()
// 显式指定上游协议。
func (a *Account) IsCustom() bool {
	return a.Platform == PlatformCustom
}

// CustomProtocol 返回 Custom 账号选择的上游协议（见 CustomProtocol* 常量）。
// 非 Custom 账号返回空串。
func (a *Account) CustomProtocol() string {
	if !a.IsCustom() {
		return ""
	}
	if protocol := strings.TrimSpace(a.GetExtraString("protocol")); protocol != "" {
		return protocol
	}
	// Compatibility for accounts created by older Custom forms that stored the
	// protocol beside credentials. New writes and the migration keep extra.protocol
	// as the single authoritative field; this fallback prevents an upgrade window
	// from making an otherwise healthy upstream invisible to the router.
	return strings.TrimSpace(a.GetCredential("protocol"))
}

// RelayMode 返回账号的中转模式。缺省为 router；旧 passthrough 布尔字段按
// full_passthrough 兼容，以保留历史“原样转发，仅替换认证”的语义。
func (a *Account) RelayMode() string {
	if a == nil {
		return RelayModeRouter
	}
	if a.Extra != nil {
		if mode, ok := a.Extra["relay_mode"].(string); ok {
			switch strings.ToLower(strings.TrimSpace(mode)) {
			case RelayModeRouter:
				return RelayModeRouter
			case RelayModePassthrough:
				return RelayModePassthrough
			case RelayModeFullPassthrough:
				return RelayModeFullPassthrough
			}
		}
	}
	if a.legacyFullPassthroughEnabled() {
		return RelayModeFullPassthrough
	}
	return RelayModeRouter
}

func (a *Account) legacyFullPassthroughEnabled() bool {
	if a == nil || a.Extra == nil {
		return false
	}
	if a.IsOpenAI() {
		if enabled, ok := a.Extra["openai_passthrough"].(bool); ok && enabled {
			return true
		}
		if enabled, ok := a.Extra["openai_oauth_passthrough"].(bool); ok && enabled {
			return true
		}
	}
	if a.IsAnthropic() && a.Type == AccountTypeAPIKey {
		if enabled, ok := a.Extra["anthropic_passthrough"].(bool); ok && enabled {
			return true
		}
	}
	return false
}

func (a *Account) IsFullPassthroughEnabled() bool {
	return a != nil && a.RelayMode() == RelayModeFullPassthrough
}

func (a *Account) IsSameProtocolPassthroughEnabled() bool {
	return a != nil && a.RelayMode() == RelayModePassthrough
}

// TargetProtocol 返回账号默认出站协议。Custom 账号使用显式 protocol；原生平台
// 使用其主协议栈。Antigravity 在 SupportedTargetProtocols 中额外声明双协议能力。
func (a *Account) TargetProtocol() string {
	if a == nil {
		return ""
	}
	if a.IsCustom() {
		return a.CustomProtocol()
	}
	if a.IsGrok() {
		return CustomProtocolOpenAIResponses
	}
	if a.IsOpenAI() {
		return CustomProtocolOpenAIResponses
	}
	if a.IsAnthropic() {
		return CustomProtocolAnthropicMessages
	}
	if a.IsGemini() {
		return CustomProtocolGemini
	}
	return ""
}

func (a *Account) SupportedTargetProtocols() []string {
	if a == nil {
		return nil
	}
	if a.IsCustom() {
		if proto := a.CustomProtocol(); proto != "" {
			return []string{proto}
		}
		return nil
	}
	if a.IsAntigravity() {
		return []string{CustomProtocolAnthropicMessages, CustomProtocolGemini}
	}
	if a.IsGrok() {
		return []string{CustomProtocolOpenAIResponses}
	}
	if a.IsOpenAI() {
		return []string{
			CustomProtocolOpenAIResponses,
			CustomProtocolOpenAIChatCompletions,
			CustomProtocolOpenAIEmbeddings,
		}
	}
	if a.IsAnthropic() {
		return []string{CustomProtocolAnthropicMessages}
	}
	if a.IsGemini() {
		return []string{CustomProtocolGemini}
	}
	return nil
}

func (a *Account) SupportsTargetProtocol(protocol string) bool {
	protocol = strings.TrimSpace(protocol)
	if protocol == "" {
		return false
	}
	for _, supported := range a.SupportedTargetProtocols() {
		if supported == protocol {
			return true
		}
	}
	return false
}

// IsCustomOpenAIProtocol / IsCustomAnthropicProtocol / IsCustomGeminiProtocol
// 报告 Custom 账号的协议归属于哪个原生转发栈（供 Is*/转发逻辑复用）。
func (a *Account) IsCustomOpenAIProtocol() bool {
	switch a.CustomProtocol() {
	case CustomProtocolOpenAIResponses, CustomProtocolOpenAIChatCompletions, CustomProtocolOpenAIEmbeddings:
		return true
	}
	return false
}

func (a *Account) GeminiOAuthType() string {
	if !a.IsPureGemini() || a.Type != AccountTypeOAuth {
		return ""
	}
	oauthType := strings.TrimSpace(a.GetCredential("oauth_type"))
	if oauthType == "" && strings.TrimSpace(a.GetCredential("project_id")) != "" {
		return "code_assist"
	}
	return oauthType
}

func (a *Account) GeminiTierID() string {
	tierID := strings.TrimSpace(a.GetCredential("tier_id"))
	return tierID
}

func (a *Account) IsGeminiCodeAssist() bool {
	if !a.IsPureGemini() || a.Type != AccountTypeOAuth {
		return false
	}
	oauthType := a.GeminiOAuthType()
	if oauthType == "" {
		return strings.TrimSpace(a.GetCredential("project_id")) != ""
	}
	return oauthType == "code_assist"
}

func (a *Account) CanGetUsage() bool {
	return a.Type == AccountTypeOAuth
}

func (a *Account) GetCredential(key string) string {
	if a.Credentials == nil {
		return ""
	}
	v, ok := a.Credentials[key]
	if !ok || v == nil {
		return ""
	}

	// 支持多种类型（兼容历史数据中 expires_at 等字段可能是数字或字符串）
	switch val := v.(type) {
	case string:
		return val
	case json.Number:
		// GORM datatypes.JSONMap 使用 UseNumber() 解析，数字类型为 json.Number
		return val.String()
	case float64:
		// JSON 解析后数字默认为 float64
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)
	case int:
		return strconv.Itoa(val)
	default:
		return ""
	}
}

// GetCredentialAsTime 解析凭证中的时间戳字段，支持多种格式
// 兼容以下格式：
//   - RFC3339 字符串: "2025-01-01T00:00:00Z"
//   - Unix 时间戳字符串: "1735689600"
//   - Unix 时间戳数字: 1735689600 (float64/int64/json.Number)
func (a *Account) GetCredentialAsTime(key string) *time.Time {
	s := a.GetCredential(key)
	if s == "" {
		return nil
	}
	// 尝试 RFC3339 格式
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t
	}
	// 尝试 Unix 时间戳（纯数字字符串）
	if ts, err := strconv.ParseInt(s, 10, 64); err == nil {
		t := time.Unix(ts, 0)
		return &t
	}
	return nil
}

// GetCredentialAsInt64 解析凭证中的 int64 字段
// 用于读取 _token_version 等内部字段
func (a *Account) GetCredentialAsInt64(key string) int64 {
	if a == nil || a.Credentials == nil {
		return 0
	}
	val, ok := a.Credentials[key]
	if !ok || val == nil {
		return 0
	}
	switch v := val.(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
	case string:
		if i, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return i
		}
	}
	return 0
}

func (a *Account) IsTempUnschedulableEnabled() bool {
	if a.Credentials == nil {
		return false
	}
	raw, ok := a.Credentials["temp_unschedulable_enabled"]
	if !ok || raw == nil {
		return false
	}
	enabled, ok := raw.(bool)
	return ok && enabled
}

func (a *Account) GetTempUnschedulableRules() []TempUnschedulableRule {
	if a.Credentials == nil {
		return nil
	}
	raw, ok := a.Credentials["temp_unschedulable_rules"]
	if !ok || raw == nil {
		return nil
	}

	arr, ok := raw.([]any)
	if !ok {
		return nil
	}

	rules := make([]TempUnschedulableRule, 0, len(arr))
	for _, item := range arr {
		entry, ok := item.(map[string]any)
		if !ok || entry == nil {
			continue
		}

		rule := TempUnschedulableRule{
			ErrorCode:       parseTempUnschedInt(entry["error_code"]),
			Keywords:        parseTempUnschedStrings(entry["keywords"]),
			DurationMinutes: parseTempUnschedInt(entry["duration_minutes"]),
			Description:     parseTempUnschedString(entry["description"]),
		}

		if rule.ErrorCode <= 0 || rule.DurationMinutes <= 0 || len(rule.Keywords) == 0 {
			continue
		}

		rules = append(rules, rule)
	}

	return rules
}

func parseTempUnschedString(value any) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func parseTempUnschedStrings(value any) []string {
	if value == nil {
		return nil
	}

	var raw []string
	switch v := value.(type) {
	case []string:
		raw = v
	case []any:
		raw = make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				raw = append(raw, s)
			}
		}
	default:
		return nil
	}

	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s := strings.TrimSpace(item)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func normalizeAccountNotes(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func parseTempUnschedInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return i
		}
	}
	return 0
}
