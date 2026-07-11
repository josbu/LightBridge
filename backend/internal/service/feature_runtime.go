package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/config"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
)

const featureRuntimeOperationTimeout = 30 * time.Second

// FeatureRuntimeComponent binds one independently owned background component to
// a catalog feature. Pause must leave restartable dynamic components reusable;
// Shutdown performs final process-exit cleanup and is called even after Pause.
type FeatureRuntimeComponent struct {
	Name     string
	Feature  ProgressiveFeature
	Start    func(context.Context) error
	Pause    func(context.Context) error
	Shutdown func(context.Context) error
}

type FeatureRuntimeComponentStatus struct {
	Name            string                       `json:"name"`
	Feature         ProgressiveFeature           `json:"feature"`
	Activation      ProgressiveFeatureActivation `json:"activation"`
	Running         bool                         `json:"running"`
	Started         bool                         `json:"started"`
	CleanupRequired bool                         `json:"cleanupRequired,omitempty"`
	LastError       string                       `json:"lastError,omitempty"`
	UpdatedAt       time.Time                    `json:"updatedAt"`
}

type featureRuntimeComponentState struct {
	component       FeatureRuntimeComponent
	running         bool
	started         bool
	cleanupRequired bool
	lastError       string
	updatedAt       time.Time
}

// FeatureRuntimeManager owns optional background component lifecycle. Core
// request-path services are intentionally not registered here.
type FeatureRuntimeManager struct {
	settingService *SettingService
	cfg            *config.Config

	reconcileMu      sync.Mutex
	mu               sync.RWMutex
	components       []*featureRuntimeComponentState
	bootAvailability map[ProgressiveFeature]bool
	started          bool
	stopped          bool
	reconcileSignal  chan struct{}
	workerCancel     context.CancelFunc
	workerWG         sync.WaitGroup
}

func NewFeatureRuntimeManager(settingService *SettingService, cfg *config.Config) (*FeatureRuntimeManager, error) {
	if err := ValidateProgressiveFeatureConfig(cfg); err != nil {
		return nil, err
	}
	return &FeatureRuntimeManager{
		settingService:   settingService,
		cfg:              cfg,
		bootAvailability: make(map[ProgressiveFeature]bool),
	}, nil
}

func (m *FeatureRuntimeManager) Register(component FeatureRuntimeComponent) error {
	if m == nil {
		return errors.New("feature runtime manager is nil")
	}
	if component.Name == "" {
		return errors.New("feature runtime component name is required")
	}
	def, ok := ProgressiveFeatureDefinitionFor(component.Feature)
	if !ok {
		return fmt.Errorf("feature runtime component %q references unknown feature %q", component.Name, component.Feature)
	}
	if def.Tier == ProgressiveFeatureTierCore {
		return fmt.Errorf("core feature %q must not be managed by progressive runtime", component.Feature)
	}
	if component.Start == nil {
		return fmt.Errorf("feature runtime component %q has no start function", component.Name)
	}
	if component.Pause == nil {
		component.Pause = func(context.Context) error { return nil }
	}
	if component.Shutdown == nil {
		component.Shutdown = component.Pause
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return fmt.Errorf("feature runtime component %q registered after manager start", component.Name)
	}
	for _, existing := range m.components {
		if existing.component.Name == component.Name {
			return fmt.Errorf("duplicate feature runtime component %q", component.Name)
		}
	}
	m.components = append(m.components, &featureRuntimeComponentState{component: component})
	return nil
}

// Start performs the only boot reconciliation and registers a lightweight
// settings callback for dynamic components. Boot components never start later
// in the process, so their configuration has clear restart semantics.
func (m *FeatureRuntimeManager) Start(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	if m.stopped {
		m.mu.Unlock()
		return errors.New("feature runtime manager already stopped")
	}
	m.started = true
	m.mu.Unlock()

	initialErr := m.reconcile(ctx, true)
	m.captureBootAvailability(ctx)
	if m.settingService != nil {
		m.settingService.SetProgressiveFeatureAvailabilityResolver(m.bootFeatureAvailability)
		workerCtx, workerCancel := context.WithCancel(context.Background())
		m.mu.Lock()
		m.reconcileSignal = make(chan struct{}, 1)
		m.workerCancel = workerCancel
		m.workerWG.Add(1)
		m.mu.Unlock()
		go m.runReconcileWorker(workerCtx)
		m.settingService.AddOnUpdateCallback(m.requestReconcile)
	}
	return initialErr
}

func (m *FeatureRuntimeManager) requestReconcile() {
	if m == nil {
		return
	}
	m.mu.RLock()
	stopped := m.stopped
	signal := m.reconcileSignal
	m.mu.RUnlock()
	if stopped || signal == nil {
		return
	}
	select {
	case signal <- struct{}{}:
	default:
		// A pending signal already covers all setting changes because reconcile
		// evaluates one immutable feature snapshot.
	}
}

func (m *FeatureRuntimeManager) runReconcileWorker(ctx context.Context) {
	defer m.workerWG.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.reconcileSignal:
			reconcileCtx, cancel := context.WithTimeout(ctx, featureRuntimeOperationTimeout)
			err := m.reconcile(reconcileCtx, false)
			cancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("feature runtime reconcile failed", "error", err)
			}
		}
	}
}

func (m *FeatureRuntimeManager) reconcile(ctx context.Context, initial bool) error {
	if m == nil {
		return nil
	}
	m.reconcileMu.Lock()
	defer m.reconcileMu.Unlock()

	m.mu.RLock()
	if m.stopped {
		m.mu.RUnlock()
		return nil
	}
	components := append([]*featureRuntimeComponentState(nil), m.components...)
	m.mu.RUnlock()

	// Components that implement one feature form one lifecycle unit. Starting
	// them independently can leave a feature half-alive (for example metrics
	// collection running without aggregation or cleanup). Preserve registration
	// order between features and component order within each feature.
	featureOrder := make([]ProgressiveFeature, 0)
	componentsByFeature := make(map[ProgressiveFeature][]*featureRuntimeComponentState)
	for _, state := range components {
		feature := state.component.Feature
		if _, exists := componentsByFeature[feature]; !exists {
			featureOrder = append(featureOrder, feature)
		}
		componentsByFeature[feature] = append(componentsByFeature[feature], state)
	}

	var failures []error
	for _, feature := range featureOrder {
		states := componentsByFeature[feature]
		def, ok := ProgressiveFeatureDefinitionFor(feature)
		if !ok {
			continue
		}
		if def.Activation == ProgressiveFeatureActivationBoot && !initial {
			continue
		}
		if def.Activation == ProgressiveFeatureActivationOnDemand || def.Activation == ProgressiveFeatureActivationEager {
			continue
		}
		enabled := true
		if m.settingService != nil {
			enabled = m.settingService.IsProgressiveFeatureEnabled(ctx, feature)
		}
		if enabled {
			if err := m.startFeatureComponents(ctx, feature, states); err != nil {
				failures = append(failures, err)
			}
			continue
		}
		if def.Activation == ProgressiveFeatureActivationDynamic {
			if err := m.pauseFeatureComponents(ctx, feature, states); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

func (m *FeatureRuntimeManager) startFeatureComponents(
	ctx context.Context,
	feature ProgressiveFeature,
	states []*featureRuntimeComponentState,
) error {
	for _, state := range states {
		m.mu.RLock()
		cleanupRequired := state.cleanupRequired
		m.mu.RUnlock()
		if cleanupRequired {
			return fmt.Errorf("start feature %s: component %s still requires cleanup", feature, state.component.Name)
		}
	}

	for _, state := range states {
		if err := m.startComponent(ctx, state); err != nil {
			rollbackCtx, cancel := context.WithTimeout(context.Background(), featureRuntimeOperationTimeout)
			rollbackErr := m.rollbackFeatureComponents(rollbackCtx, states)
			cancel()
			if rollbackErr != nil {
				return errors.Join(err, fmt.Errorf("rollback feature %s: %w", feature, rollbackErr))
			}
			return err
		}
	}
	return nil
}

func (m *FeatureRuntimeManager) pauseFeatureComponents(
	ctx context.Context,
	feature ProgressiveFeature,
	states []*featureRuntimeComponentState,
) error {
	var failures []error
	for i := len(states) - 1; i >= 0; i-- {
		if err := m.pauseComponent(ctx, states[i]); err != nil {
			failures = append(failures, err)
		}
	}
	if err := errors.Join(failures...); err != nil {
		return fmt.Errorf("pause feature %s: %w", feature, err)
	}
	return nil
}

// rollbackFeatureComponents returns the complete feature to a stopped state
// after any member fails to start. Rollback uses reverse registration order so
// dependants are stopped before the services they may call.
func (m *FeatureRuntimeManager) rollbackFeatureComponents(
	ctx context.Context,
	states []*featureRuntimeComponentState,
) error {
	var failures []error
	for i := len(states) - 1; i >= 0; i-- {
		state := states[i]
		m.mu.RLock()
		running := state.running
		m.mu.RUnlock()
		if !running {
			continue
		}
		if err := state.component.Pause(ctx); err != nil {
			wrapped := fmt.Errorf("rollback component %s: %w", state.component.Name, err)
			m.mu.RLock()
			started := state.started
			m.mu.RUnlock()
			m.setComponentState(state, true, started, true, wrapped)
			failures = append(failures, wrapped)
			continue
		}
		m.updateComponentState(state, false, true, nil)
	}
	return errors.Join(failures...)
}

func (m *FeatureRuntimeManager) startComponent(ctx context.Context, state *featureRuntimeComponentState) error {
	m.mu.RLock()
	running := state.running
	cleanupRequired := state.cleanupRequired
	previouslyStarted := state.started
	m.mu.RUnlock()
	if running {
		return nil
	}
	if cleanupRequired {
		return fmt.Errorf("start %s: previous failed start still requires cleanup", state.component.Name)
	}
	if err := state.component.Start(ctx); err != nil {
		startErr := fmt.Errorf("start %s: %w", state.component.Name, err)
		rollbackCtx, cancel := context.WithTimeout(context.Background(), featureRuntimeOperationTimeout)
		rollbackErr := state.component.Pause(rollbackCtx)
		cancel()
		if rollbackErr != nil {
			combined := errors.Join(startErr, fmt.Errorf("rollback %s after failed start: %w", state.component.Name, rollbackErr))
			// Treat the component as unavailable and block retries. Some resources may
			// still be alive, so final process shutdown must try cleanup again.
			m.setComponentState(state, false, true, true, combined)
			return combined
		}
		// A successful rollback leaves a dynamic component reusable. Preserve an
		// earlier successful-start marker so final Shutdown can still destroy any
		// long-lived object (for example a reusable worker pool).
		m.setComponentState(state, false, previouslyStarted, false, startErr)
		return startErr
	}
	m.setComponentState(state, true, true, false, nil)
	slog.Info("progressive component started", "component", state.component.Name, "feature", state.component.Feature)
	return nil
}

func (m *FeatureRuntimeManager) pauseComponent(ctx context.Context, state *featureRuntimeComponentState) error {
	m.mu.RLock()
	running := state.running
	m.mu.RUnlock()
	if !running {
		return nil
	}
	if err := state.component.Pause(ctx); err != nil {
		wrapped := fmt.Errorf("pause %s: %w", state.component.Name, err)
		m.updateComponentState(state, true, true, wrapped)
		return wrapped
	}
	m.updateComponentState(state, false, true, nil)
	slog.Info("progressive component paused", "component", state.component.Name, "feature", state.component.Feature)
	return nil
}

func (m *FeatureRuntimeManager) updateComponentState(state *featureRuntimeComponentState, running, started bool, err error) {
	m.mu.RLock()
	cleanupRequired := state.cleanupRequired
	previouslyStarted := state.started
	m.mu.RUnlock()
	m.setComponentState(state, running, previouslyStarted || started, cleanupRequired, err)
}

func (m *FeatureRuntimeManager) setComponentState(
	state *featureRuntimeComponentState,
	running bool,
	started bool,
	cleanupRequired bool,
	err error,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state.running = running
	state.started = started
	state.cleanupRequired = cleanupRequired
	state.updatedAt = time.Now().UTC()
	if err != nil {
		state.lastError = err.Error()
	} else {
		state.lastError = ""
	}
}

// captureBootAvailability freezes the actual process-lifetime availability of
// boot features after initial reconciliation. Subsequent setting changes can
// report requiresRestart, but cannot expose routes without their worker or hide
// routes while the worker is still alive.
func (m *FeatureRuntimeManager) captureBootAvailability(ctx context.Context) {
	if m == nil {
		return
	}
	configured := buildProgressiveFeatureSnapshot(m.cfg, nil, progressiveFeatureDefinitions)
	if m.settingService != nil {
		configured = m.settingService.progressiveFeatureSnapshot(ctx)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, def := range progressiveFeatureDefinitions {
		if def.Activation != ProgressiveFeatureActivationBoot {
			continue
		}
		desired := false
		if configured != nil {
			if state, ok := configured.states[def.ID]; ok {
				desired = state.Enabled
			}
		}
		active := desired
		registered := false
		for _, component := range m.components {
			if component.component.Feature != def.ID {
				continue
			}
			registered = true
			if !component.running {
				active = false
			}
		}
		if desired && !registered {
			// Route-only boot features are valid; no background component is required.
			active = true
		}
		m.bootAvailability[def.ID] = active
	}
}

func (m *FeatureRuntimeManager) bootFeatureAvailability(feature ProgressiveFeature) (bool, bool) {
	if m == nil {
		return false, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	available, ok := m.bootAvailability[feature]
	return available, ok
}

// Shutdown stops all components in reverse registration order. It retains every
// component record, including previously paused dynamic components, so final
// resource destruction is never skipped.
func (m *FeatureRuntimeManager) Shutdown(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	workerCancel := m.workerCancel
	m.mu.RUnlock()
	if workerCancel != nil {
		workerCancel()
		m.workerWG.Wait()
	}
	m.reconcileMu.Lock()
	defer m.reconcileMu.Unlock()

	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return nil
	}
	m.stopped = true
	components := append([]*featureRuntimeComponentState(nil), m.components...)
	m.mu.Unlock()

	var failures []error
	for i := len(components) - 1; i >= 0; i-- {
		state := components[i]
		m.mu.RLock()
		started := state.started || state.cleanupRequired
		m.mu.RUnlock()
		if !started {
			continue
		}
		if err := state.component.Shutdown(ctx); err != nil {
			wrapped := fmt.Errorf("shutdown %s: %w", state.component.Name, err)
			m.setComponentState(state, false, true, true, wrapped)
			failures = append(failures, wrapped)
			continue
		}
		m.setComponentState(state, false, true, false, nil)
	}
	if m.settingService != nil {
		m.settingService.SetProgressiveFeatureAvailabilityResolver(nil)
	}
	return errors.Join(failures...)
}

func (m *FeatureRuntimeManager) Status() []FeatureRuntimeComponentStatus {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]FeatureRuntimeComponentStatus, 0, len(m.components))
	for _, state := range m.components {
		def, _ := ProgressiveFeatureDefinitionFor(state.component.Feature)
		result = append(result, FeatureRuntimeComponentStatus{
			Name:            state.component.Name,
			Feature:         state.component.Feature,
			Activation:      def.Activation,
			Running:         state.running,
			Started:         state.started,
			CleanupRequired: state.cleanupRequired,
			LastError:       state.lastError,
			UpdatedAt:       state.updatedAt,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func noErrorStart(fn func()) func(context.Context) error {
	return func(context.Context) error {
		if fn != nil {
			fn()
		}
		return nil
	}
}

func noErrorStop(fn func()) func(context.Context) error {
	return func(context.Context) error {
		if fn != nil {
			fn()
		}
		return nil
	}
}

// ProvideFeatureRuntimeManager registers all non-core background ownership in
// one place. Constructors remain cheap and side-effect free; this provider is
// the sole startup point for optional workers.
func ProvideFeatureRuntimeManager(
	settingService *SettingService,
	cfg *config.Config,
	moduleService *ModuleService,
	opsMetricsCollector *OpsMetricsCollector,
	opsAggregation *OpsAggregationService,
	opsAlertEvaluator *OpsAlertEvaluatorService,
	opsCleanup *OpsCleanupService,
	opsScheduledReport *OpsScheduledReportService,
	opsSystemLogSink *OpsSystemLogSink,
	dashboardAggregation *DashboardAggregationService,
	usageCleanup *UsageCleanupService,
	scheduledTests *ScheduledTestRunnerService,
	backup *BackupService,
	lightBridgeConnect *LightBridgeConnectSyncService,
	paymentExpiry *PaymentOrderExpiryService,
	channelMonitor *ChannelMonitorRunner,
	contentModeration *ContentModerationService,
) (*FeatureRuntimeManager, error) {
	manager, err := NewFeatureRuntimeManager(settingService, cfg)
	if err != nil {
		return nil, err
	}

	register := func(component FeatureRuntimeComponent) error {
		if err := manager.Register(component); err != nil {
			return err
		}
		return nil
	}

	if err := register(FeatureRuntimeComponent{
		Name:    "payment_order_expiry",
		Feature: ProgressiveFeaturePayment,
		Start:   noErrorStart(paymentExpiry.Start),
		Pause:   noErrorStop(paymentExpiry.Stop),
	}); err != nil {
		return nil, err
	}
	if err := register(FeatureRuntimeComponent{
		Name:     "channel_monitor_runner",
		Feature:  ProgressiveFeatureChannelMonitor,
		Start:    noErrorStart(channelMonitor.Start),
		Pause:    noErrorStop(channelMonitor.Pause),
		Shutdown: noErrorStop(channelMonitor.Stop),
	}); err != nil {
		return nil, err
	}
	if err := register(FeatureRuntimeComponent{
		Name:    "content_moderation_workers",
		Feature: ProgressiveFeatureRiskControl,
		Start:   func(context.Context) error { contentModeration.SetRuntimeEnabled(true); return nil },
		Pause:   func(context.Context) error { contentModeration.SetRuntimeEnabled(false); return nil },
	}); err != nil {
		return nil, err
	}

	opsComponents := []FeatureRuntimeComponent{
		{Name: "ops_system_log_sink", Feature: ProgressiveFeatureOpsMonitoring, Start: func(context.Context) error { opsSystemLogSink.Start(); logger.SetSink(opsSystemLogSink); return nil }, Pause: func(context.Context) error { logger.SetSink(nil); opsSystemLogSink.Stop(); return nil }},
		{Name: "ops_metrics_collector", Feature: ProgressiveFeatureOpsMonitoring, Start: noErrorStart(opsMetricsCollector.Start), Pause: noErrorStop(opsMetricsCollector.Stop)},
		{Name: "ops_aggregation", Feature: ProgressiveFeatureOpsMonitoring, Start: noErrorStart(opsAggregation.Start), Pause: noErrorStop(opsAggregation.Stop)},
		{Name: "ops_alert_evaluator", Feature: ProgressiveFeatureOpsMonitoring, Start: noErrorStart(opsAlertEvaluator.Start), Pause: noErrorStop(opsAlertEvaluator.Stop)},
		{Name: "ops_cleanup", Feature: ProgressiveFeatureOpsMonitoring, Start: noErrorStart(opsCleanup.Start), Pause: noErrorStop(opsCleanup.Stop)},
		{Name: "ops_scheduled_reports", Feature: ProgressiveFeatureOpsMonitoring, Start: noErrorStart(opsScheduledReport.Start), Pause: noErrorStop(opsScheduledReport.Stop)},
	}
	for _, component := range opsComponents {
		if err := register(component); err != nil {
			return nil, err
		}
	}

	if err := register(FeatureRuntimeComponent{Name: "dashboard_aggregation", Feature: ProgressiveFeatureDashboardAggregation, Start: noErrorStart(dashboardAggregation.Start), Pause: noErrorStop(dashboardAggregation.Stop)}); err != nil {
		return nil, err
	}
	if err := register(FeatureRuntimeComponent{Name: "usage_cleanup", Feature: ProgressiveFeatureUsageCleanup, Start: noErrorStart(usageCleanup.Start), Pause: noErrorStop(usageCleanup.Stop)}); err != nil {
		return nil, err
	}
	if err := register(FeatureRuntimeComponent{Name: "scheduled_tests", Feature: ProgressiveFeatureScheduledTests, Start: noErrorStart(scheduledTests.Start), Pause: noErrorStop(scheduledTests.Stop)}); err != nil {
		return nil, err
	}
	if err := register(FeatureRuntimeComponent{Name: "backup_scheduler", Feature: ProgressiveFeatureBackup, Start: noErrorStart(backup.Start), Pause: noErrorStop(backup.Stop)}); err != nil {
		return nil, err
	}
	if err := register(FeatureRuntimeComponent{
		Name:    "module_runtime",
		Feature: ProgressiveFeatureModuleRuntime,
		Start: func(ctx context.Context) error {
			if err := moduleService.AutoInstallManagedProviderModules(ctx); err != nil {
				return err
			}
			return moduleService.StartEnabledModules(ctx)
		},
		Pause: moduleService.StopEnabledModuleRuntimes,
	}); err != nil {
		return nil, err
	}
	if err := register(FeatureRuntimeComponent{Name: "lightbridge_connect_sync", Feature: ProgressiveFeatureLightBridgeConnect, Start: noErrorStart(lightBridgeConnect.Start), Pause: noErrorStop(lightBridgeConnect.Stop)}); err != nil {
		return nil, err
	}

	startCtx, cancel := context.WithTimeout(context.Background(), featureRuntimeOperationTimeout)
	defer cancel()
	if err := manager.Start(startCtx); err != nil {
		// Optional components must not make the core gateway unavailable. The
		// component states retain their errors for the administrator diagnostic
		// endpoint and can be retried where their activation permits it.
		slog.Error("one or more progressive components failed to start", "error", err)
	}
	return manager, nil
}
