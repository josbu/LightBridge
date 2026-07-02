package proxy

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/WilliamWang1721/LightBridge/internal/modules"
	proxybinding "github.com/WilliamWang1721/LightBridge/internal/modules/proxy/internal/binding"
	"github.com/WilliamWang1721/LightBridge/internal/outbound"
	"github.com/stretchr/testify/require"
)

func TestOutboundRuntimeRegistersResolver(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	var _ = db

	registry := outbound.NewRegistry()
	runtime := NewOutboundRuntime(registry, db)
	err = runtime.StartOutbound(context.Background(), modules.InstalledModule{ID: proxybinding.AdapterID, Type: modules.ModuleTypeOutbound})
	require.NoError(t, err)

	resolver, err := registry.ResolveAdapter(proxybinding.AdapterID)
	require.NoError(t, err)
	require.NotNil(t, resolver)

	mock.ExpectExec("UPDATE proxy_runtime_instances").
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, runtime.StopOutbound(context.Background(), proxybinding.AdapterID))
	_, err = registry.ResolveAdapter(proxybinding.AdapterID)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOutboundRuntimeIgnoresOtherOutboundModules(t *testing.T) {
	registry := outbound.NewRegistry()
	runtime := NewOutboundRuntime(registry, nil)
	err := runtime.StartOutbound(context.Background(), modules.InstalledModule{ID: "other.outbound", Type: modules.ModuleTypeOutbound})
	require.NoError(t, err)
	require.Empty(t, registry.IDs())
}
