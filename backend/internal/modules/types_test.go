package modules

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateManifest_AllowsOutboundProxyModule(t *testing.T) {
	manifest := Manifest{
		APIVersion: ManifestAPIVersionV1Alpha1,
		ID:         "lightbridge.proxy",
		Name:       "LightBridge Proxy",
		Type:       ModuleTypeOutbound,
		Version:    "0.1.0",
		Capabilities: []Capability{
			CapabilityOutboundAdapter,
			CapabilityEntityBinding,
		},
		Backend: &BackendSpec{Entrypoints: map[string]string{"outbound": "./proxy-module"}},
	}

	require.NoError(t, ValidateManifest(manifest))
}

func TestValidateManifest_RejectsOutboundAdapterWithoutBackend(t *testing.T) {
	manifest := Manifest{
		APIVersion:   ManifestAPIVersionV1Alpha1,
		ID:           "lightbridge.proxy",
		Name:         "LightBridge Proxy",
		Type:         ModuleTypeOutbound,
		Version:      "0.1.0",
		Capabilities: []Capability{CapabilityOutboundAdapter},
	}

	require.ErrorContains(t, ValidateManifest(manifest), "outbound.adapter requires backend spec")
}

func TestValidateManifest_RejectsUnsupportedModuleType(t *testing.T) {
	manifest := Manifest{
		APIVersion: ManifestAPIVersionV1Alpha1,
		ID:         "lightbridge.unknown",
		Name:       "Unknown",
		Type:       ModuleType("unknown"),
		Version:    "0.1.0",
	}

	require.ErrorContains(t, ValidateManifest(manifest), "unsupported module type")
}

func TestValidateManifest_RequiresEntityPanelContribution(t *testing.T) {
	manifest := Manifest{
		APIVersion:   ManifestAPIVersionV1Alpha1,
		ID:           "example.provider",
		Name:         "Example",
		Type:         ModuleTypeProvider,
		Version:      "0.1.0",
		Capabilities: []Capability{CapabilityUIEntityPanel},
		Frontend:     &FrontendSpec{Entry: "frontend/remoteEntry.js"},
	}

	require.ErrorContains(t, ValidateManifest(manifest), "requires at least one entity panel")
	manifest.Frontend.EntityPanels = []FrontendEntityPanelSpec{{
		Entity:        "account",
		Title:         "Provider status",
		ExposedModule: "./AccountStatus",
	}}
	require.NoError(t, ValidateManifest(manifest))
}
