package modules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const ManifestAPIVersionV1Alpha1 = "lightbridge/v1alpha1"

type ModuleType string

const (
	ModuleTypeProvider ModuleType = "provider"
	ModuleTypeOutbound ModuleType = "outbound"
)

type ModuleStatus string

const (
	ModuleStatusInstalled   ModuleStatus = "installed"
	ModuleStatusEnabled     ModuleStatus = "enabled"
	ModuleStatusDisabled    ModuleStatus = "disabled"
	ModuleStatusFailed      ModuleStatus = "failed"
	ModuleStatusUninstalled ModuleStatus = "uninstalled"
	ModuleStatusPurged      ModuleStatus = "purged"
)

type Capability string

const (
	CapabilityProviderAdapter Capability = "provider.adapter"
	CapabilityOutboundAdapter Capability = "outbound.adapter"
	CapabilityUIAdminRoute    Capability = "ui.admin.route"
	CapabilityUIAccountForm   Capability = "ui.account.form"
	CapabilityUIEntityPanel   Capability = "ui.entity.panel"
	CapabilityEntityBinding   Capability = "entity.binding"
)

type PermissionSet map[string][]string
type LocalizedText map[string]string

type CoreSpec struct {
	Compatible string `json:"compatible" yaml:"compatible"`
}
type BackendSpec struct {
	Entrypoints map[string]string `json:"entrypoints,omitempty" yaml:"entrypoints,omitempty"`
}
type FrontendSpec struct {
	Entry        string                    `json:"entry" yaml:"entry"`
	Routes       []FrontendRouteSpec       `json:"routes,omitempty" yaml:"routes,omitempty"`
	Menu         []FrontendMenuSpec        `json:"menu,omitempty" yaml:"menu,omitempty"`
	AccountForms []FrontendAccountFormSpec `json:"accountForms,omitempty" yaml:"accountForms,omitempty"`
	EntityPanels []FrontendEntityPanelSpec `json:"entityPanels,omitempty" yaml:"entityPanels,omitempty"`
}
type FrontendRouteSpec struct {
	Path          string        `json:"path" yaml:"path"`
	Title         string        `json:"title" yaml:"title"`
	TitleI18n     LocalizedText `json:"title_i18n,omitempty" yaml:"title_i18n,omitempty"`
	ExposedModule string        `json:"exposedModule" yaml:"exposedModule"`
	RequiresAdmin bool          `json:"requiresAdmin,omitempty" yaml:"requiresAdmin,omitempty"`
}
type FrontendMenuSpec struct {
	Title     string        `json:"title" yaml:"title"`
	TitleI18n LocalizedText `json:"title_i18n,omitempty" yaml:"title_i18n,omitempty"`
	Path      string        `json:"path" yaml:"path"`
	Group     string        `json:"group,omitempty" yaml:"group,omitempty"`
	Order     int           `json:"order,omitempty" yaml:"order,omitempty"`
}
type FrontendAccountFormSpec struct {
	ProviderID    string `json:"providerId" yaml:"providerId"`
	ExposedModule string `json:"exposedModule" yaml:"exposedModule"`
}
type FrontendEntityPanelSpec struct {
	Entity        string        `json:"entity" yaml:"entity"`
	Title         string        `json:"title" yaml:"title"`
	TitleI18n     LocalizedText `json:"title_i18n,omitempty" yaml:"title_i18n,omitempty"`
	ExposedModule string        `json:"exposedModule" yaml:"exposedModule"`
	RequiresAdmin bool          `json:"requiresAdmin,omitempty" yaml:"requiresAdmin,omitempty"`
	Order         int           `json:"order,omitempty" yaml:"order,omitempty"`
}

type Manifest struct {
	APIVersion      string        `json:"apiVersion" yaml:"apiVersion"`
	ID              string        `json:"id" yaml:"id"`
	Name            string        `json:"name" yaml:"name"`
	NameI18n        LocalizedText `json:"name_i18n,omitempty" yaml:"name_i18n,omitempty"`
	Description     string        `json:"description,omitempty" yaml:"description,omitempty"`
	DescriptionI18n LocalizedText `json:"description_i18n,omitempty" yaml:"description_i18n,omitempty"`
	Type            ModuleType    `json:"type" yaml:"type"`
	Version         string        `json:"version" yaml:"version"`
	Core            CoreSpec      `json:"core" yaml:"core"`
	Capabilities    []Capability  `json:"capabilities" yaml:"capabilities"`
	Permissions     PermissionSet `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	Backend         *BackendSpec  `json:"backend,omitempty" yaml:"backend,omitempty"`
	Frontend        *FrontendSpec `json:"frontend,omitempty" yaml:"frontend,omitempty"`
}

type InstalledModule struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Type        ModuleType   `json:"type"`
	Version     string       `json:"version"`
	Status      ModuleStatus `json:"status"`
	InstallPath string       `json:"installPath"`
	Manifest    Manifest     `json:"manifest"`
	InstalledAt time.Time    `json:"installedAt"`
	EnabledAt   *time.Time   `json:"enabledAt,omitempty"`
	LastError   string       `json:"lastError,omitempty"`
}
type PermissionRecord struct {
	ModuleID        string     `json:"moduleId"`
	PermissionType  string     `json:"permissionType"`
	PermissionValue string     `json:"permissionValue"`
	Approved        bool       `json:"approved"`
	ApprovedAt      *time.Time `json:"approvedAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type Store interface {
	ListInstalled(context.Context) ([]InstalledModule, error)
	GetInstalled(context.Context, string) (*InstalledModule, error)
	SaveInstalled(context.Context, InstalledModule) error
	SavePermissions(context.Context, string, []PermissionRecord) error
	ListPermissions(context.Context, string) ([]PermissionRecord, error)
	ApprovePermissions(context.Context, string) error
	SetStatus(context.Context, string, ModuleStatus, string) error
}
type DataPurger interface {
	PurgeModuleData(context.Context, InstalledModule) error
}
type Installer interface {
	InstallArchive(context.Context, string) (*InstalledModule, error)
}
type InstalledVerifier interface {
	VerifyInstalled(context.Context, InstalledModule) (*InstalledModule, error)
}
type SignatureVerifier interface{ Verify([]byte, string) error }

type ProviderAccount struct {
	ID            string         `json:"id,omitempty"`
	ProviderID    string         `json:"provider_id,omitempty"`
	DisplayName   string         `json:"display_name,omitempty"`
	CredentialRef string         `json:"credential_ref,omitempty"`
	Config        map[string]any `json:"config,omitempty"`
	Secrets       map[string]any `json:"secrets,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}
type GatewayRequest struct {
	DownstreamProtocol string              `json:"downstream_protocol,omitempty"`
	Endpoint           string              `json:"endpoint,omitempty"`
	Method             string              `json:"method,omitempty"`
	Headers            map[string][]string `json:"headers,omitempty"`
	Body               json.RawMessage     `json:"body,omitempty"`
	Stream             bool                `json:"stream,omitempty"`
	Account            ProviderAccount     `json:"account"`
	Metadata           map[string]any      `json:"metadata,omitempty"`
}
type Usage struct {
	InputTokens  int64 `json:"input_tokens,omitempty"`
	OutputTokens int64 `json:"output_tokens,omitempty"`
}
type GatewayError struct {
	StatusCode int    `json:"status_code,omitempty"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
	Retryable  bool   `json:"retryable,omitempty"`
}
type GatewayEvent struct {
	Type       string              `json:"type"`
	StatusCode int                 `json:"status_code,omitempty"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Data       json.RawMessage     `json:"data,omitempty"`
	Usage      *Usage              `json:"usage,omitempty"`
	Error      *GatewayError       `json:"error,omitempty"`
	Metadata   map[string]any      `json:"metadata,omitempty"`
}
type TestAccountRequest struct {
	Account ProviderAccount `json:"account"`
	Mode    string          `json:"mode,omitempty"`
}
type TestAccountResult struct {
	OK       bool           `json:"ok"`
	Message  string         `json:"message,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}
type AccountValidationResult struct {
	Valid    bool           `json:"valid"`
	Warnings []string       `json:"warnings,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type ProviderAdapter interface {
	Forward(context.Context, GatewayRequest) (<-chan GatewayEvent, error)
	ValidateAccount(context.Context, ProviderAccount) (*AccountValidationResult, error)
	RefreshAccount(context.Context, ProviderAccount) (*ProviderAccount, error)
	TestAccount(context.Context, TestAccountRequest) (*TestAccountResult, error)
	Close() error
}
type ProviderRuntime interface {
	StartProvider(context.Context, InstalledModule) error
	StopProvider(context.Context, string) error
}

type OutboundRuntime interface {
	StartOutbound(context.Context, InstalledModule) error
	StopOutbound(context.Context, string) error
}

type ProviderRegistry struct {
	mu       sync.RWMutex
	adapters map[string]ProviderAdapter
}

func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{adapters: map[string]ProviderAdapter{}}
}
func (r *ProviderRegistry) Register(id string, a ProviderAdapter) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[strings.TrimSpace(id)] = a
}
func (r *ProviderRegistry) Unregister(id string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.adapters, strings.TrimSpace(id))
}
func (r *ProviderRegistry) Resolve(id string) (ProviderAdapter, error) {
	if r == nil {
		return nil, errors.New("provider registry is nil")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	a := r.adapters[strings.TrimSpace(id)]
	if a == nil {
		return nil, fmt.Errorf("provider %q is not registered", id)
	}
	return a, nil
}
func (r *ProviderRegistry) IDs() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.adapters))
	for id := range r.adapters {
		ids = append(ids, id)
	}
	return ids
}
func IsAllowedCapability(c Capability) bool {
	switch c {
	case CapabilityProviderAdapter,
		CapabilityOutboundAdapter,
		CapabilityUIAdminRoute,
		CapabilityUIAccountForm,
		CapabilityUIEntityPanel,
		CapabilityEntityBinding:
		return true
	default:
		return false
	}
}
func ValidateManifest(m Manifest) error {
	if strings.TrimSpace(m.APIVersion) == "" {
		return errors.New("apiVersion is required")
	}
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("id is required")
	}
	if strings.TrimSpace(m.Version) == "" {
		return errors.New("version is required")
	}
	if m.Type == "" {
		return errors.New("type is required")
	}
	switch m.Type {
	case ModuleTypeProvider, ModuleTypeOutbound:
	default:
		return fmt.Errorf("unsupported module type %q", m.Type)
	}
	for _, c := range m.Capabilities {
		if !IsAllowedCapability(c) {
			return fmt.Errorf("unsupported capability %q", c)
		}
	}
	if hasCapability(m, CapabilityProviderAdapter) && m.Backend == nil {
		return errors.New("provider.adapter requires backend spec")
	}
	if hasCapability(m, CapabilityOutboundAdapter) && m.Backend == nil {
		return errors.New("outbound.adapter requires backend spec")
	}
	if err := validateFrontendCapabilities(m); err != nil {
		return err
	}
	return nil
}

func validateFrontendCapabilities(m Manifest) error {
	if m.Frontend == nil {
		for _, capability := range []Capability{CapabilityUIAdminRoute, CapabilityUIAccountForm, CapabilityUIEntityPanel} {
			if hasCapability(m, capability) {
				return fmt.Errorf("%s requires frontend spec", capability)
			}
		}
		return nil
	}
	if strings.TrimSpace(m.Frontend.Entry) == "" {
		return errors.New("frontend entry is required")
	}
	if len(m.Frontend.Routes) > 0 && !hasCapability(m, CapabilityUIAdminRoute) {
		return errors.New("frontend routes require ui.admin.route capability")
	}
	if len(m.Frontend.AccountForms) > 0 && !hasCapability(m, CapabilityUIAccountForm) {
		return errors.New("frontend account forms require ui.account.form capability")
	}
	if len(m.Frontend.EntityPanels) > 0 && !hasCapability(m, CapabilityUIEntityPanel) {
		return errors.New("frontend entity panels require ui.entity.panel capability")
	}
	if hasCapability(m, CapabilityUIAdminRoute) && len(m.Frontend.Routes) == 0 {
		return errors.New("ui.admin.route requires at least one frontend route")
	}
	if hasCapability(m, CapabilityUIAccountForm) && len(m.Frontend.AccountForms) == 0 {
		return errors.New("ui.account.form requires at least one account form")
	}
	if hasCapability(m, CapabilityUIEntityPanel) && len(m.Frontend.EntityPanels) == 0 {
		return errors.New("ui.entity.panel requires at least one entity panel")
	}
	for _, route := range m.Frontend.Routes {
		if !strings.HasPrefix(strings.TrimSpace(route.Path), "/") || strings.TrimSpace(route.ExposedModule) == "" {
			return errors.New("frontend routes require an absolute path and exposedModule")
		}
	}
	for _, form := range m.Frontend.AccountForms {
		if strings.TrimSpace(form.ProviderID) == "" || strings.TrimSpace(form.ExposedModule) == "" {
			return errors.New("frontend account forms require providerId and exposedModule")
		}
	}
	for _, panel := range m.Frontend.EntityPanels {
		if strings.TrimSpace(panel.Entity) == "" || strings.TrimSpace(panel.ExposedModule) == "" {
			return errors.New("frontend entity panels require entity and exposedModule")
		}
	}
	return nil
}
func hasCapability(m Manifest, c Capability) bool {
	for _, x := range m.Capabilities {
		if x == c {
			return true
		}
	}
	return false
}
