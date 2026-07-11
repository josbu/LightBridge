package handler

import (
	"context"
	"strings"

	"github.com/WilliamWang1721/LightBridge/internal/service"
)

// shouldSkipOpsErrorLog determines if an error should be skipped from logging based on settings.
// Returns true for errors that should be filtered according to OpsAdvancedSettings.
func shouldSkipOpsErrorLog(ctx context.Context, ops *service.OpsService, message, body, requestPath string) bool {
	if ops == nil {
		return false
	}

	// Get advanced settings to check filter configuration
	settings, err := ops.GetOpsAdvancedSettings(ctx)
	if err != nil || settings == nil {
		// If we can't get settings, don't skip (fail open)
		return false
	}

	msgLower := strings.ToLower(message)
	bodyLower := strings.ToLower(body)

	// Check if count_tokens errors should be ignored
	if settings.IgnoreCountTokensErrors && strings.Contains(requestPath, "/count_tokens") {
		return true
	}

	// Check if context canceled errors should be ignored (client disconnects)
	if settings.IgnoreContextCanceled {
		if strings.Contains(msgLower, opsErrContextCanceled) || strings.Contains(bodyLower, opsErrContextCanceled) {
			return true
		}
	}

	// Check if "no available accounts" errors should be ignored
	if settings.IgnoreNoAvailableAccounts {
		if strings.Contains(msgLower, opsErrNoAvailableAccounts) || strings.Contains(bodyLower, opsErrNoAvailableAccounts) {
			return true
		}
	}

	// Check if invalid/missing API key errors should be ignored (user misconfiguration)
	if settings.IgnoreInvalidApiKeyErrors {
		if strings.Contains(bodyLower, opsErrInvalidAPIKey) || strings.Contains(bodyLower, opsErrAPIKeyRequired) {
			return true
		}
	}

	// Check if insufficient balance errors should be ignored
	if settings.IgnoreInsufficientBalanceErrors {
		if strings.Contains(bodyLower, opsErrInsufficientBalance) || strings.Contains(bodyLower, opsErrInsufficientAccountBalance) ||
			strings.Contains(bodyLower, opsErrInsufficientQuota) ||
			strings.Contains(msgLower, opsErrInsufficientBalance) || strings.Contains(msgLower, opsErrInsufficientAccountBalance) {
			return true
		}
	}

	return false
}
