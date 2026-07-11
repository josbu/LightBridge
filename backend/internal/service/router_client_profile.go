package service

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/ctxkey"
)

// RouterClientKind identifies clients whose protocol parsers require stricter
// response shapes than a generic OpenAI/Anthropic-compatible HTTP caller.
type RouterClientKind string

const (
	RouterClientUnknown    RouterClientKind = "unknown"
	RouterClientClaudeCode RouterClientKind = "claude_code"
	RouterClientCodexCLI   RouterClientKind = "codex_cli"
	RouterClientCodexApp   RouterClientKind = "codex_app"
	RouterClientOpenCode   RouterClientKind = "opencode"
)

// RouterClientProfile describes request/response compatibility requirements.
// It deliberately contains capability flags instead of scattering client-name
// checks across protocol converters.
type RouterClientProfile struct {
	Kind                    RouterClientKind
	Version                 string
	StrictAnthropicStream   bool
	StrictAnthropicUsage    bool
	StrictResponsesTerminal bool
}

var routerClientVersionPattern = regexp.MustCompile(`(?i)(?:claude-cli|claude-code|codex_cli_rs|codex|opencode)[/ _-]v?(\d+\.\d+(?:\.\d+)?)`)

// DetectRouterClientProfile uses only stable request metadata. Body-dependent
// Claude Code verification remains in ClaudeCodeValidator; this early profile
// exists so Router middleware can choose protocol-safe response normalization.
func DetectRouterClientProfile(r *http.Request) RouterClientProfile {
	profile := RouterClientProfile{Kind: RouterClientUnknown}
	if r == nil {
		return profile
	}

	ua := strings.TrimSpace(r.Header.Get("User-Agent"))
	uaLower := strings.ToLower(ua)
	xApp := strings.ToLower(strings.TrimSpace(r.Header.Get("X-App")))
	originator := strings.ToLower(strings.TrimSpace(r.Header.Get("Originator")))

	switch {
	case strings.HasPrefix(uaLower, "claude-cli/") ||
		strings.HasPrefix(uaLower, "claude-code/") ||
		strings.Contains(xApp, "claude-code"):
		profile.Kind = RouterClientClaudeCode
		profile.StrictAnthropicStream = true
		profile.StrictAnthropicUsage = true

	case strings.Contains(uaLower, "opencode") || strings.Contains(xApp, "opencode") || strings.Contains(originator, "opencode"):
		profile.Kind = RouterClientOpenCode
		profile.StrictResponsesTerminal = true

	case strings.Contains(uaLower, "codex_cli_rs") || strings.Contains(uaLower, "codex-cli"):
		profile.Kind = RouterClientCodexCLI
		profile.StrictResponsesTerminal = true

	case strings.Contains(uaLower, "codex") || strings.Contains(originator, "codex") ||
		r.Header.Get("X-Codex-Turn-State") != "" || r.Header.Get("X-Codex-Turn-Metadata") != "":
		profile.Kind = RouterClientCodexApp
		profile.StrictResponsesTerminal = true
	}

	if match := routerClientVersionPattern.FindStringSubmatch(ua); len(match) == 2 {
		profile.Version = match[1]
	}
	return profile
}

func WithRouterClientProfile(ctx context.Context, profile RouterClientProfile) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxkey.RouterClientProfile, profile)
}

func RouterClientProfileFromContext(ctx context.Context) RouterClientProfile {
	if ctx == nil {
		return RouterClientProfile{Kind: RouterClientUnknown}
	}
	if profile, ok := ctx.Value(ctxkey.RouterClientProfile).(RouterClientProfile); ok {
		return profile
	}
	return RouterClientProfile{Kind: RouterClientUnknown}
}

func IsStrictResponsesClient(ctx context.Context) bool {
	return RouterClientProfileFromContext(ctx).StrictResponsesTerminal
}
