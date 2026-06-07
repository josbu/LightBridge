package service

import (
	"strings"

	"github.com/Wei-Shaw/LightBridge/internal/config"
	"github.com/Wei-Shaw/LightBridge/internal/modules"
)

type CoreBridge struct{}

func ProvideCoreBridge(_ modules.Store, _ modules.Store, _ UserRepository, _ AccountRepository) *CoreBridge {
	return &CoreBridge{}
}
func ProvideModuleInstaller(cfg *config.Config, store modules.Store, _ BuildInfo) modules.Installer {
	dataDir := "data"
	if cfg != nil && cfg.Modules.DataDir != "" {
		dataDir = cfg.Modules.DataDir
	}
	var verifier modules.SignatureVerifier
	if cfg != nil && strings.TrimSpace(cfg.Modules.SignaturePublicKeyPath) != "" {
		if v, err := modules.NewEd25519SignatureVerifierFromFile(strings.TrimSpace(cfg.Modules.SignaturePublicKeyPath)); err == nil {
			verifier = v
		}
	}
	return modules.NewPackageInstallerWithVerifier(dataDir, store, verifier)
}
func ProvideProviderRuntime(_ *config.Config, registry *modules.ProviderRegistry, _ modules.Store, _ *CoreBridge) modules.ProviderRuntime {
	return modules.NewProcessProviderRuntime(registry)
}
