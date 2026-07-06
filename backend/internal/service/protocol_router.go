package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/ctxkey"
)

type ProtocolRouteDecision struct {
	InboundProtocol  string
	TargetProtocol   string
	RelayMode        string
	ConversionChain  []string
	FinalRelayFormat string
	FailureReason    string
}

func InboundProtocolFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(ctxkey.InboundProtocol).(string); ok {
		return strings.TrimSpace(v)
	}
	if v, ok := ctx.Value(ctxkey.RequiredProtocol).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func TargetProtocolFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(ctxkey.TargetProtocol).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func RelayModeFromContext(ctx context.Context) string {
	if ctx == nil {
		return RelayModeRouter
	}
	if v, ok := ctx.Value(ctxkey.RelayMode).(string); ok {
		switch strings.TrimSpace(v) {
		case RelayModePassthrough, RelayModeFullPassthrough:
			return strings.TrimSpace(v)
		}
	}
	return RelayModeRouter
}

func WithInboundProtocol(ctx context.Context, protocol string) context.Context {
	protocol = strings.TrimSpace(protocol)
	if ctx == nil || protocol == "" {
		return ctx
	}
	ctx = context.WithValue(ctx, ctxkey.InboundProtocol, protocol)
	// RequiredProtocol is kept for older call sites. It now carries inbound protocol.
	return context.WithValue(ctx, ctxkey.RequiredProtocol, protocol)
}

func WithProtocolRouteDecision(ctx context.Context, decision ProtocolRouteDecision) context.Context {
	if ctx == nil {
		return ctx
	}
	if decision.InboundProtocol != "" {
		ctx = context.WithValue(ctx, ctxkey.InboundProtocol, decision.InboundProtocol)
		ctx = context.WithValue(ctx, ctxkey.RequiredProtocol, decision.InboundProtocol)
	}
	if decision.TargetProtocol != "" {
		ctx = context.WithValue(ctx, ctxkey.TargetProtocol, decision.TargetProtocol)
	}
	if decision.RelayMode != "" {
		ctx = context.WithValue(ctx, ctxkey.RelayMode, decision.RelayMode)
	}
	if len(decision.ConversionChain) > 0 {
		ctx = context.WithValue(ctx, ctxkey.ConversionChain, append([]string(nil), decision.ConversionChain...))
	}
	if decision.FinalRelayFormat != "" {
		ctx = context.WithValue(ctx, ctxkey.FinalRelayFormat, decision.FinalRelayFormat)
	}
	return ctx
}

func IsMessageProtocol(protocol string) bool {
	switch strings.TrimSpace(protocol) {
	case CustomProtocolOpenAIResponses,
		CustomProtocolOpenAIChatCompletions,
		CustomProtocolAnthropicMessages,
		CustomProtocolGemini:
		return true
	default:
		return false
	}
}

func ProtocolRouteDecisionForAccount(ctx context.Context, account *Account) (ProtocolRouteDecision, bool) {
	inbound := InboundProtocolFromContext(ctx)
	if inbound == "" {
		inbound = CustomProtocolAnthropicMessages
	}
	return ProtocolRouteDecisionForAccountProtocols(inbound, account)
}

func ProtocolRouteDecisionForAccountProtocols(inbound string, account *Account) (ProtocolRouteDecision, bool) {
	inbound = strings.TrimSpace(inbound)
	if account == nil {
		return ProtocolRouteDecision{
			InboundProtocol: inbound,
			RelayMode:       RelayModeRouter,
			FailureReason:   "account is nil",
		}, false
	}
	mode := account.RelayMode()
	supported := account.SupportedTargetProtocols()
	if len(supported) == 0 {
		return ProtocolRouteDecision{
			InboundProtocol: inbound,
			RelayMode:       mode,
			FailureReason:   "account has no supported target protocols",
		}, false
	}

	if inbound == "" {
		target := preferredTargetProtocol(account, supported, "")
		return ProtocolRouteDecision{
			InboundProtocol:  inbound,
			TargetProtocol:   target,
			RelayMode:        mode,
			ConversionChain:  []string{target},
			FinalRelayFormat: target,
		}, true
	}

	switch mode {
	case RelayModeFullPassthrough:
		if !containsProtocol(supported, inbound) {
			return ProtocolRouteDecision{
				InboundProtocol: inbound,
				RelayMode:       mode,
				FailureReason:   fmt.Sprintf("full passthrough mode does not support inbound protocol %q (supported: %s)", inbound, strings.Join(supported, ", ")),
			}, false
		}
		target := preferredTargetProtocol(account, supported, inbound)
		return ProtocolRouteDecision{
			InboundProtocol:  inbound,
			TargetProtocol:   target,
			RelayMode:        mode,
			ConversionChain:  []string{inbound},
			FinalRelayFormat: inbound,
		}, true

	case RelayModePassthrough:
		if !containsProtocol(supported, inbound) {
			return ProtocolRouteDecision{
				InboundProtocol: inbound,
				RelayMode:       mode,
				FailureReason:   fmt.Sprintf("passthrough mode does not support inbound protocol %q (supported: %s)", inbound, strings.Join(supported, ", ")),
			}, false
		}
		return ProtocolRouteDecision{
			InboundProtocol:  inbound,
			TargetProtocol:   inbound,
			RelayMode:        mode,
			ConversionChain:  []string{inbound},
			FinalRelayFormat: inbound,
		}, true
	}

	if containsProtocol(supported, inbound) {
		return ProtocolRouteDecision{
			InboundProtocol:  inbound,
			TargetProtocol:   inbound,
			RelayMode:        RelayModeRouter,
			ConversionChain:  []string{inbound},
			FinalRelayFormat: inbound,
		}, true
	}

	if !IsMessageProtocol(inbound) {
		return ProtocolRouteDecision{
			InboundProtocol: inbound,
			RelayMode:       RelayModeRouter,
			FailureReason:   fmt.Sprintf("inbound protocol %q is not a recognized message protocol (supported: anthropic-messages, openai-responses, openai-chat-completions, gemini)", inbound),
		}, false
	}
	target := preferredTargetProtocol(account, supported, inbound)
	if !IsMessageProtocol(target) {
		return ProtocolRouteDecision{
			InboundProtocol: inbound,
			RelayMode:       RelayModeRouter,
			FailureReason:   fmt.Sprintf("account has no supported target protocol for conversion (account supports: %s)", strings.Join(supported, ", ")),
		}, false
	}
	if !routePairImplemented(inbound, target) {
		return ProtocolRouteDecision{
			InboundProtocol: inbound,
			TargetProtocol:  target,
			RelayMode:       RelayModeRouter,
			FailureReason:   fmt.Sprintf("conversion from %q to %q is not implemented", inbound, target),
		}, false
	}
	chain := buildConversionChain(inbound, target)
	return ProtocolRouteDecision{
		InboundProtocol:  inbound,
		TargetProtocol:   target,
		RelayMode:        RelayModeRouter,
		ConversionChain:  chain,
		FinalRelayFormat: target,
	}, true
}

func routePairImplemented(inbound, target string) bool {
	if inbound == target {
		return true
	}
	switch inbound {
	case CustomProtocolAnthropicMessages, CustomProtocolOpenAIChatCompletions:
		return target == CustomProtocolAnthropicMessages ||
			target == CustomProtocolOpenAIResponses ||
			target == CustomProtocolOpenAIChatCompletions ||
			target == CustomProtocolGemini
	case CustomProtocolOpenAIResponses:
		return target == CustomProtocolAnthropicMessages ||
			target == CustomProtocolOpenAIResponses ||
			target == CustomProtocolOpenAIChatCompletions ||
			target == CustomProtocolGemini
	case CustomProtocolGemini:
		return target == CustomProtocolAnthropicMessages ||
			target == CustomProtocolOpenAIResponses ||
			target == CustomProtocolOpenAIChatCompletions ||
			target == CustomProtocolGemini
	default:
		return false
	}
}

func preferredTargetProtocol(account *Account, supported []string, inbound string) string {
	if inbound != "" && containsProtocol(supported, inbound) {
		return inbound
	}
	if account != nil {
		if target := account.TargetProtocol(); target != "" && containsProtocol(supported, target) {
			return target
		}
	}
	for _, proto := range supported {
		if strings.TrimSpace(proto) != "" {
			return strings.TrimSpace(proto)
		}
	}
	return ""
}

func containsProtocol(protocols []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, proto := range protocols {
		if strings.TrimSpace(proto) == target {
			return true
		}
	}
	return false
}

func buildConversionChain(inbound, target string) []string {
	if inbound == "" {
		return nil
	}
	if target == "" || target == inbound {
		return []string{inbound}
	}
	if inbound == CustomProtocolOpenAIResponses || target == CustomProtocolOpenAIResponses {
		return []string{inbound, target}
	}
	return []string{inbound, CustomProtocolOpenAIResponses, target}
}
