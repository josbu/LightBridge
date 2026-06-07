package modules

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type ProcessProviderRuntime struct {
	registry  *ProviderRegistry
	processes map[string]*exec.Cmd
	sockets   map[string]string
	mu        sync.Mutex
}

func NewProcessProviderRuntime(registry *ProviderRegistry) *ProcessProviderRuntime {
	return &ProcessProviderRuntime{registry: registry, processes: map[string]*exec.Cmd{}, sockets: map[string]string{}}
}
func (r *ProcessProviderRuntime) StartProvider(ctx context.Context, m InstalledModule) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.processes[m.ID] != nil {
		return nil
	}
	binary := providerBinaryPath(m)
	if binary == "" {
		return fmt.Errorf("module %s has no provider backend entrypoint", m.ID)
	}
	socket := filepath.Join(os.TempDir(), "lightbridge-modules", m.ID+".sock")
	_ = os.MkdirAll(filepath.Dir(socket), 0o755)
	_ = os.Remove(socket)
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = append(os.Environ(), "LIGHTBRIDGE_MODULE_SOCKET="+socket)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socket); err == nil {
			adapter, err := NewGRPCProviderAdapter(socket)
			if err != nil {
				_ = cmd.Process.Kill()
				return err
			}
			r.registry.Register(m.ID, adapter)
			r.processes[m.ID] = cmd
			r.sockets[m.ID] = socket
			go func() { _ = cmd.Wait() }()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return fmt.Errorf("provider module %s did not create socket", m.ID)
}
func (r *ProcessProviderRuntime) StopProvider(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.registry != nil {
		if a, err := r.registry.Resolve(id); err == nil {
			_ = a.Close()
		}
		r.registry.Unregister(id)
	}
	if cmd := r.processes[id]; cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if sock := r.sockets[id]; sock != "" {
		_ = os.Remove(sock)
	}
	delete(r.processes, id)
	delete(r.sockets, id)
	return nil
}
func providerBinaryPath(m InstalledModule) string {
	if m.Manifest.Backend == nil {
		return ""
	}
	platform := runtime.GOOS + "-" + runtime.GOARCH
	if p := strings.TrimSpace(m.Manifest.Backend.Entrypoints[platform]); p != "" {
		return filepath.Join(m.InstallPath, filepath.FromSlash(p))
	}
	if p := strings.TrimSpace(m.Manifest.Backend.Entrypoints["default"]); p != "" {
		return filepath.Join(m.InstallPath, filepath.FromSlash(p))
	}
	return ""
}
