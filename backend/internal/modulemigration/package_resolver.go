package modulemigration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wei-Shaw/LightBridge/internal/config"
)

type OpenAIModulePackageResolution struct {
	Workspace     string
	PackagePath   string
	PublicKeyPath string
	RegistryURL   string
}

func ResolveOpenAIModulePackage(ctx context.Context, registryURL string, timeoutSeconds int, signaturePublicKeyPath string) (*OpenAIModulePackageResolution, error) {
	registryURL = strings.TrimSpace(registryURL)
	if registryURL == "" {
		registryURL = DefaultModuleMigrationRegistryURL
	}
	workspace, err := os.MkdirTemp("", "lightbridge-openai-module-*")
	if err != nil {
		return nil, fmt.Errorf("create openai module workspace: %w", err)
	}
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			_ = os.RemoveAll(workspace)
		}
	}()

	registry, err := fetchModuleRegistry(ctx, registryURL, timeoutSeconds)
	if err != nil {
		return nil, err
	}
	entry, ok := selectRegistryEntry(registry, DefaultOpenAIModuleID, DefaultOpenAIModuleVersion)
	if !ok {
		return nil, fmt.Errorf("openai module %s was not found in registry %s", DefaultOpenAIModuleVersion, registryURL)
	}

	packagePath := filepath.Join(workspace, filepath.Base(entry.DownloadURL))
	if packagePath == workspace || strings.TrimSpace(filepath.Base(entry.DownloadURL)) == "" || filepath.Base(entry.DownloadURL) == "." {
		packagePath = filepath.Join(workspace, "lightbridge-module-openai-0.1.0.tar.zst")
	}
	if err := downloadFile(ctx, entry.DownloadURL, packagePath, timeoutSeconds); err != nil {
		return nil, fmt.Errorf("download openai module package: %w", err)
	}
	if entry.SHA256 != "" {
		if err := verifyPackageSHA256(packagePath, entry.SHA256); err != nil {
			return nil, err
		}
	}

	publicKeyPath := strings.TrimSpace(signaturePublicKeyPath)
	if publicKeyPath == "" {
		publicKeyPath = filepath.Join(workspace, "ed25519.pub")
		publicKeyURL := registryAssetURL(registryURL, "ed25519.pub")
		if err := downloadFile(ctx, publicKeyURL, publicKeyPath, timeoutSeconds); err != nil {
			return nil, fmt.Errorf("download module signing public key: %w", err)
		}
	}

	cleanupOnError = false
	return &OpenAIModulePackageResolution{
		Workspace:     workspace,
		PackagePath:   packagePath,
		PublicKeyPath: publicKeyPath,
		RegistryURL:   registryURL,
	}, nil
}

func ResolveOpenAIModulePackageFromConfig(ctx context.Context, cfg *config.Config) (*OpenAIModulePackageResolution, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	return ResolveOpenAIModulePackage(ctx, cfg.Modules.MarketplaceRegistryURL, cfg.Modules.MarketplaceTimeoutSeconds, cfg.Modules.SignaturePublicKeyPath)
}
