package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProtocolRouteDecision_MessageProtocolMatrix(t *testing.T) {
	inbounds := []string{
		CustomProtocolOpenAIResponses,
		CustomProtocolOpenAIChatCompletions,
		CustomProtocolAnthropicMessages,
		CustomProtocolGemini,
	}
	targets := []string{
		CustomProtocolOpenAIResponses,
		CustomProtocolOpenAIChatCompletions,
		CustomProtocolAnthropicMessages,
		CustomProtocolGemini,
	}

	for _, inbound := range inbounds {
		for _, target := range targets {
			t.Run(inbound+"->"+target, func(t *testing.T) {
				account := &Account{
					ID:       1,
					Platform: PlatformCustom,
					Type:     AccountTypeAPIKey,
					Extra: map[string]any{
						"protocol": target,
					},
				}

				decision, ok := ProtocolRouteDecisionForAccountProtocols(inbound, account)
				require.True(t, ok)
				require.Equal(t, inbound, decision.InboundProtocol)
				require.Equal(t, target, decision.TargetProtocol)
				require.Equal(t, RelayModeRouter, decision.RelayMode)
				require.Equal(t, target, decision.FinalRelayFormat)
				if inbound == target {
					require.Equal(t, []string{inbound}, decision.ConversionChain)
				} else if inbound == CustomProtocolOpenAIResponses || target == CustomProtocolOpenAIResponses {
					require.Equal(t, []string{inbound, target}, decision.ConversionChain)
				} else {
					require.Equal(t, []string{inbound, CustomProtocolOpenAIResponses, target}, decision.ConversionChain)
				}
			})
		}
	}
}

func TestProtocolRouteDecision_RelayModes(t *testing.T) {
	sameProtocol := &Account{
		Platform: PlatformCustom,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			"protocol":   CustomProtocolOpenAIResponses,
			"relay_mode": RelayModePassthrough,
		},
	}
	decision, ok := ProtocolRouteDecisionForAccountProtocols(CustomProtocolOpenAIResponses, sameProtocol)
	require.True(t, ok)
	require.Equal(t, RelayModePassthrough, decision.RelayMode)
	require.Equal(t, CustomProtocolOpenAIResponses, decision.TargetProtocol)
	require.Equal(t, []string{CustomProtocolOpenAIResponses}, decision.ConversionChain)

	_, ok = ProtocolRouteDecisionForAccountProtocols(CustomProtocolAnthropicMessages, sameProtocol)
	require.False(t, ok)

	fullPassthrough := &Account{
		Platform: PlatformCustom,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			"protocol":   CustomProtocolGemini,
			"relay_mode": RelayModeFullPassthrough,
		},
	}
	decision, ok = ProtocolRouteDecisionForAccountProtocols(CustomProtocolAnthropicMessages, fullPassthrough)
	require.True(t, ok)
	require.Equal(t, RelayModeFullPassthrough, decision.RelayMode)
	require.Equal(t, CustomProtocolGemini, decision.TargetProtocol)
	require.Equal(t, CustomProtocolAnthropicMessages, decision.FinalRelayFormat)
	require.Equal(t, []string{CustomProtocolAnthropicMessages}, decision.ConversionChain)
}

func TestAccountRelayMode_LegacyFieldsMapToFullPassthrough(t *testing.T) {
	require.Equal(t, RelayModeFullPassthrough, (&Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Extra:    map[string]any{"openai_passthrough": true},
	}).RelayMode())

	require.Equal(t, RelayModeFullPassthrough, (&Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"openai_oauth_passthrough": true},
	}).RelayMode())

	require.Equal(t, RelayModeFullPassthrough, (&Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Extra:    map[string]any{"anthropic_passthrough": true},
	}).RelayMode())

	require.Equal(t, RelayModePassthrough, (&Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			"relay_mode":         RelayModePassthrough,
			"openai_passthrough": true,
		},
	}).RelayMode(), "explicit relay_mode should take precedence over legacy fields")
}

func TestFilterAccountsByRequestProtocol_MixedGroupAllowsRouterButRestrictsPassthrough(t *testing.T) {
	ctx := WithInboundProtocol(context.Background(), CustomProtocolOpenAIResponses)
	accounts := []Account{
		{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey},
		{ID: 2, Platform: PlatformGemini, Type: AccountTypeAPIKey},
		{ID: 3, Platform: PlatformCustom, Type: AccountTypeAPIKey, Extra: map[string]any{
			"protocol":   CustomProtocolAnthropicMessages,
			"relay_mode": RelayModePassthrough,
		}},
		{ID: 4, Platform: PlatformCustom, Type: AccountTypeAPIKey, Extra: map[string]any{
			"protocol":   CustomProtocolGemini,
			"relay_mode": RelayModeFullPassthrough,
		}},
	}

	filtered := filterAccountsByRequestProtocol(ctx, accounts)
	var ids []int64
	for _, account := range filtered {
		ids = append(ids, account.ID)
	}
	require.ElementsMatch(t, []int64{1, 2, 4}, ids)
}

func TestSchedulingQueryPlatformsForMessageRequestIgnoresGroupPlatform(t *testing.T) {
	ctx := WithInboundProtocol(context.Background(), CustomProtocolGemini)

	require.ElementsMatch(t,
		[]string{PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformCustom},
		schedulingQueryPlatformsForRequest(ctx, PlatformAnthropic, false),
	)
	require.ElementsMatch(t,
		[]string{PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformCustom},
		schedulingQueryPlatformsForRequest(ctx, PlatformOpenAI, false),
	)
}
