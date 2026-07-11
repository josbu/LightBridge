package service

import (
	"hash/fnv"
	"reflect"
	"sort"
	"strings"

	"github.com/WilliamWang1721/LightBridge/internal/domain"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/xai"
)

const (
	// OpenAICompactModeAuto follows compact-probe results when deciding compact eligibility.
	OpenAICompactModeAuto = "auto"
	// OpenAICompactModeForceOn always treats the account as compact-supported.
	OpenAICompactModeForceOn = "force_on"
	// OpenAICompactModeForceOff always treats the account as compact-unsupported.
	OpenAICompactModeForceOff = "force_off"
)

func normalizeOpenAICompactMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case OpenAICompactModeForceOn:
		return OpenAICompactModeForceOn
	case OpenAICompactModeForceOff:
		return OpenAICompactModeForceOff
	default:
		return OpenAICompactModeAuto
	}
}

func stringMappingFromRaw(raw any) map[string]string {
	switch mapping := raw.(type) {
	case map[string]any:
		if len(mapping) == 0 {
			return nil
		}
		result := make(map[string]string, len(mapping))
		for key, value := range mapping {
			if str, ok := value.(string); ok {
				result[key] = str
			}
		}
		if len(result) == 0 {
			return nil
		}
		return result
	case map[string]string:
		if len(mapping) == 0 {
			return nil
		}
		result := make(map[string]string, len(mapping))
		for key, value := range mapping {
			result[key] = value
		}
		return result
	default:
		return nil
	}
}

func (a *Account) GetModelMapping() map[string]string {
	credentialsPtr := mapPtr(a.Credentials)
	rawMapping, _ := a.Credentials["model_mapping"].(map[string]any)
	rawPtr := mapPtr(rawMapping)
	rawLen := len(rawMapping)
	rawSig := uint64(0)
	rawSigReady := false

	if a.modelMappingCacheReady &&
		a.modelMappingCacheCredentialsPtr == credentialsPtr &&
		a.modelMappingCacheRawPtr == rawPtr &&
		a.modelMappingCacheRawLen == rawLen {
		rawSig = modelMappingSignature(rawMapping)
		rawSigReady = true
		if a.modelMappingCacheRawSig == rawSig {
			return a.modelMappingCache
		}
	}

	mapping := a.resolveModelMapping(rawMapping)
	if !rawSigReady {
		rawSig = modelMappingSignature(rawMapping)
	}

	a.modelMappingCache = mapping
	a.modelMappingCacheReady = true
	a.modelMappingCacheCredentialsPtr = credentialsPtr
	a.modelMappingCacheRawPtr = rawPtr
	a.modelMappingCacheRawLen = rawLen
	a.modelMappingCacheRawSig = rawSig
	return mapping
}

func (a *Account) resolveModelMapping(rawMapping map[string]any) map[string]string {
	if a.Credentials == nil {
		// Antigravity 平台使用默认映射
		if a.IsAntigravity() {
			return domain.DefaultAntigravityModelMapping
		}
		if a.IsGrok() {
			return xai.DefaultModelMapping()
		}
		// Bedrock 默认映射由 forwardBedrock 统一处理（需配合 region prefix 调整）
		return nil
	}
	if len(rawMapping) == 0 {
		// Antigravity 平台使用默认映射
		if a.IsAntigravity() {
			return domain.DefaultAntigravityModelMapping
		}
		if a.IsGrok() {
			return xai.DefaultModelMapping()
		}
		return nil
	}

	result := make(map[string]string)
	for k, v := range rawMapping {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	if len(result) > 0 {
		if a.IsAntigravity() {
			ensureAntigravityDefaultPassthroughs(result, []string{
				"gemini-3-flash",
				"gemini-3.1-pro",
				"gemini-3.1-pro-high",
				"gemini-3.1-pro-low",
			})
		}
		if a.IsGrok() {
			defaults := xai.DefaultModelMapping()
			for k, v := range result {
				defaults[k] = v
			}
			return defaults
		}
		return result
	}

	// Antigravity 平台使用默认映射
	if a.IsAntigravity() {
		return domain.DefaultAntigravityModelMapping
	}
	if a.IsGrok() {
		return xai.DefaultModelMapping()
	}
	return nil
}

func mapPtr(m map[string]any) uintptr {
	if m == nil {
		return 0
	}
	return reflect.ValueOf(m).Pointer()
}

func modelMappingSignature(rawMapping map[string]any) uint64 {
	if len(rawMapping) == 0 {
		return 0
	}
	keys := make([]string, 0, len(rawMapping))
	for k := range rawMapping {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := fnv.New64a()
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte{0})
		if v, ok := rawMapping[k].(string); ok {
			_, _ = h.Write([]byte(v))
		} else {
			_, _ = h.Write([]byte{1})
		}
		_, _ = h.Write([]byte{0xff})
	}
	return h.Sum64()
}

func ensureAntigravityDefaultPassthrough(mapping map[string]string, model string) {
	if mapping == nil || model == "" {
		return
	}
	if _, exists := mapping[model]; exists {
		return
	}
	for pattern := range mapping {
		if matchWildcard(pattern, model) {
			return
		}
	}
	mapping[model] = model
}

func ensureAntigravityDefaultPassthroughs(mapping map[string]string, models []string) {
	for _, model := range models {
		ensureAntigravityDefaultPassthrough(mapping, model)
	}
}

func normalizeRequestedModelForLookup(platform, requestedModel string) string {
	trimmed := strings.TrimSpace(requestedModel)
	if trimmed == "" {
		return ""
	}
	if platform != PlatformGemini && platform != PlatformAntigravity {
		return trimmed
	}
	if trimmed == "gemini-3.1-pro-preview-customtools" {
		return "gemini-3.1-pro-preview"
	}
	return trimmed
}

func mappingSupportsRequestedModel(mapping map[string]string, requestedModel string) bool {
	if requestedModel == "" {
		return false
	}
	if _, exists := mapping[requestedModel]; exists {
		return true
	}
	for pattern := range mapping {
		if matchWildcard(pattern, requestedModel) {
			return true
		}
	}
	return false
}

func resolveRequestedModelInMapping(mapping map[string]string, requestedModel string) (mappedModel string, matched bool) {
	if requestedModel == "" {
		return "", false
	}
	if mappedModel, exists := mapping[requestedModel]; exists {
		return mappedModel, true
	}
	return matchWildcardMappingResult(mapping, requestedModel)
}

// IsModelSupported preserves the historical hot-path API, but model_mapping is
// no longer a whitelist by default. It only restricts when the explicit account
// switch restrict_to_model_list is enabled.
func (a *Account) IsModelSupported(requestedModel string) bool {
	if !a.RestrictToModelList() {
		return true
	}
	if a.SupportsListedModel(requestedModel) {
		return true
	}
	mapping := a.GetModelMapping()
	if mappingSupportsRequestedModel(mapping, requestedModel) {
		return true
	}
	normalized := normalizeRequestedModelForLookup(a.Platform, requestedModel)
	return normalized != requestedModel && mappingSupportsRequestedModel(mapping, normalized)
}

// GetMappedModel 获取映射后的模型名（支持通配符，最长优先匹配）
// 如果未配置 mapping，返回原始模型名
func (a *Account) GetMappedModel(requestedModel string) string {
	mappedModel, _ := a.ResolveMappedModel(requestedModel)
	return mappedModel
}

// ResolveMappedModel 获取映射后的模型名，并返回是否命中了账号级映射。
// matched=true 表示命中了精确映射或通配符映射，即使映射结果与原模型名相同。
func (a *Account) ResolveMappedModel(requestedModel string) (mappedModel string, matched bool) {
	mapping := a.GetModelMapping()
	if len(mapping) == 0 {
		return requestedModel, false
	}
	if mappedModel, matched := resolveRequestedModelInMapping(mapping, requestedModel); matched {
		return mappedModel, true
	}
	normalized := normalizeRequestedModelForLookup(a.Platform, requestedModel)
	if normalized != requestedModel {
		if mappedModel, matched := resolveRequestedModelInMapping(mapping, normalized); matched {
			return mappedModel, true
		}
	}
	return requestedModel, false
}

// GetOpenAICompactMode returns the compact routing mode for an OpenAI account.
// Missing or invalid values fall back to "auto".
func (a *Account) GetOpenAICompactMode() string {
	if a == nil || !a.IsOpenAI() || a.Extra == nil {
		return OpenAICompactModeAuto
	}
	mode, _ := a.Extra["openai_compact_mode"].(string)
	return normalizeOpenAICompactMode(mode)
}

// OpenAICompactSupportKnown reports whether compact capability is known for this
// account and, when known, whether it is supported.
func (a *Account) OpenAICompactSupportKnown() (supported bool, known bool) {
	if a == nil || !a.IsOpenAI() {
		return false, false
	}

	switch a.GetOpenAICompactMode() {
	case OpenAICompactModeForceOn:
		return true, true
	case OpenAICompactModeForceOff:
		return false, true
	}

	if a.Extra == nil {
		return false, false
	}
	supported, ok := a.Extra["openai_compact_supported"].(bool)
	if !ok {
		return false, false
	}
	return supported, true
}

// AllowsOpenAICompact reports whether the account may be considered for compact
// requests. Unknown capability remains allowed to avoid breaking older accounts
// before an explicit probe has been run.
func (a *Account) AllowsOpenAICompact() bool {
	if a == nil || !a.IsOpenAI() {
		return false
	}
	supported, known := a.OpenAICompactSupportKnown()
	if !known {
		return true
	}
	return supported
}

// GetCompactModelMapping returns compact-only model remapping configuration.
// This mapping is intended for /responses/compact only and does not affect
// normal /responses traffic.
func (a *Account) GetCompactModelMapping() map[string]string {
	if a == nil || a.Credentials == nil {
		return nil
	}
	return stringMappingFromRaw(a.Credentials["compact_model_mapping"])
}

// ResolveCompactMappedModel resolves compact-only model remapping and reports
// whether a compact-specific mapping rule matched.
func (a *Account) ResolveCompactMappedModel(requestedModel string) (mappedModel string, matched bool) {
	mapping := a.GetCompactModelMapping()
	if len(mapping) == 0 {
		return requestedModel, false
	}
	if mappedModel, matched := resolveRequestedModelInMapping(mapping, requestedModel); matched {
		return mappedModel, true
	}
	return requestedModel, false
}

func (a *Account) GetBaseURL() string {
	if a.Type != AccountTypeAPIKey {
		return ""
	}
	baseURL := strings.TrimSpace(a.GetCredential("base_url"))
	if a.IsAntigravity() {
		if baseURL == "" {
			baseURL = "https://api.anthropic.com"
		}
		return strings.TrimRight(baseURL, "/") + "/antigravity"
	}
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return baseURL
}

// GetGeminiBaseURL 返回 Gemini 兼容端点的 base URL。
// Custom（gemini 协议）账号使用自定义 base_url；Antigravity 平台的 APIKey 账号
// 自动拼接 /antigravity；原生 Gemini 仍可使用自定义 base_url（不锁定）。
func (a *Account) GetGeminiBaseURL(defaultBaseURL string) string {
	baseURL := strings.TrimSpace(a.GetCredential("base_url"))
	if a.IsCustom() {
		if baseURL == "" {
			return defaultBaseURL
		}
		return baseURL
	}
	if baseURL == "" {
		return defaultBaseURL
	}
	if a.IsAntigravity() && a.Type == AccountTypeAPIKey {
		return strings.TrimRight(baseURL, "/") + "/antigravity"
	}
	return baseURL
}

// UsesBearerAuth 报告该 Gemini APIKey 账号是否以 Bearer token 鉴权上游。
// 用于 AIStudio 反代（如 aistudio-api）账号：上游使用 `Authorization: Bearer <token>`
// 而非官方的 `x-goog-api-key` 头。仅对原生 Gemini APIKey 账号有意义（凭据字段
// auth_header=="bearer"）；其他类型恒为 false。
func (a *Account) UsesBearerAuth() bool {
	if a == nil || a.Type != AccountTypeAPIKey || !a.IsPureGemini() {
		return false
	}
	return strings.TrimSpace(a.GetCredential("auth_header")) == "bearer"
}

func (a *Account) GetExtraString(key string) string {
	if a.Extra == nil {
		return ""
	}
	if v, ok := a.Extra[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// GetAuthenticityVerdict 返回账号的 Claude 模型真伪检测结论。
// 值为 "genuine"(真)、"counterfeit"(假冒/疑似)、"unknown"(未知/不适用) 或空。
func (a *Account) GetAuthenticityVerdict() string {
	return a.GetExtraString(AccountExtraKeyAuthenticityVerdict)
}

// GetAuthenticityCheckedAt 返回上次真伪检测的时间（RFC3339 字符串）。
func (a *Account) GetAuthenticityCheckedAt() string {
	return a.GetExtraString(AccountExtraKeyAuthenticityCheckedAt)
}

// GetAuthenticityMethod 返回真伪检测方法："probe"(主动探针) 或 "passive"(被动检测)。
func (a *Account) GetAuthenticityMethod() string {
	return a.GetExtraString(AccountExtraKeyAuthenticityMethod)
}

func (a *Account) GetClaudeUserID() string {
	if v := strings.TrimSpace(a.GetExtraString("claude_user_id")); v != "" {
		return v
	}
	if v := strings.TrimSpace(a.GetExtraString("anthropic_user_id")); v != "" {
		return v
	}
	if v := strings.TrimSpace(a.GetCredential("claude_user_id")); v != "" {
		return v
	}
	if v := strings.TrimSpace(a.GetCredential("anthropic_user_id")); v != "" {
		return v
	}
	return ""
}

// matchAntigravityWildcard 通配符匹配（仅支持末尾 *）
// 用于 model_mapping 的通配符匹配
func matchAntigravityWildcard(pattern, str string) bool {
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(str, prefix)
	}
	return pattern == str
}

// matchWildcard 通用通配符匹配（仅支持末尾 *）
// 复用 Antigravity 的通配符逻辑，供其他平台使用
func matchWildcard(pattern, str string) bool {
	return matchAntigravityWildcard(pattern, str)
}

func matchWildcardMappingResult(mapping map[string]string, requestedModel string) (string, bool) {
	// 收集所有匹配的 pattern，按长度降序排序（最长优先）
	type patternMatch struct {
		pattern string
		target  string
	}
	var matches []patternMatch

	for pattern, target := range mapping {
		if matchWildcard(pattern, requestedModel) {
			matches = append(matches, patternMatch{pattern, target})
		}
	}

	if len(matches) == 0 {
		return requestedModel, false // 无匹配，返回原始模型名
	}

	// 按 pattern 长度降序排序
	sort.Slice(matches, func(i, j int) bool {
		if len(matches[i].pattern) != len(matches[j].pattern) {
			return len(matches[i].pattern) > len(matches[j].pattern)
		}
		return matches[i].pattern < matches[j].pattern
	})

	return matches[0].target, true
}
