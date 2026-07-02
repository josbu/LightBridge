package aistudio_proxy

import (
	"testing"
)

func TestGenerateToken(t *testing.T) {
	tok := generateToken()
	if len(tok) < 16 {
		t.Fatalf("generateToken() too short: %q", tok)
	}
	// Two calls should produce distinct tokens (randomness sanity check).
	tok2 := generateToken()
	if tok == tok2 {
		t.Fatalf("generateToken() produced identical tokens: %q", tok)
	}
	if got, want := tok[:6], "lbaip_"; got != want {
		t.Fatalf("generateToken() prefix = %q, want %q", got, want)
	}
}

func TestManagerNilSafe(t *testing.T) {
	var m *Manager
	if got := m.StatusOf(1); got != StatusStopped {
		t.Fatalf("nil Manager.StatusOf() = %q, want %q", got, StatusStopped)
	}
	m.StopAll() // must not panic
	if _, err := m.EnsureRunning(context.TODO(), 1); err == nil {
		t.Fatal("nil Manager.EnsureRunning() should error")
	}
}

func TestSnapshotEmpty(t *testing.T) {
	m := NewManager(Config{DataDir: t.TempDir()}, nil, nil)
	if snap := m.Snapshot(); len(snap) != 0 {
		t.Fatalf("Snapshot() on empty manager = %v, want empty", snap)
	}
}

func TestNewManagerDefaults(t *testing.T) {
	m := NewManager(Config{DataDir: t.TempDir()}, nil, nil)
	if m.cfg.PortStart != defaultPortStart || m.cfg.PortEnd != defaultPortEnd {
		t.Fatalf("default port range = %d-%d, want %d-%d", m.cfg.PortStart, m.cfg.PortEnd, defaultPortStart, defaultPortEnd)
	}
	if m.cfg.PythonBin != "python3" {
		t.Fatalf("default python = %q, want python3", m.cfg.PythonBin)
	}
	if m.cfg.HealthTimeout != defaultHealthTimeout {
		t.Fatalf("default health timeout = %v, want %v", m.cfg.HealthTimeout, defaultHealthTimeout)
	}
}
