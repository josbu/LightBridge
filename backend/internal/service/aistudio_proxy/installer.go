package aistudio_proxy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// RuntimeStatus describes the aistudio-api runtime readiness on this host.
type RuntimeStatus struct {
	RuntimeDir    string `json:"runtime_dir"`
	PythonBin     string `json:"python_bin"`
	PythonOK      bool   `json:"python_ok"`
	PythonVersion string `json:"python_version,omitempty"`
	// AistudioInstalled reports whether the aistudio-api main.py is present in RuntimeDir.
	AistudioInstalled bool `json:"aistudio_installed"`
	// PackagesInstalled reports whether the python deps (fastapi, playwright,
	// cloakbrowser, ...) appear importable. Best-effort.
	PackagesInstalled bool `json:"packages_installed"`
	// BrowserInstalled reports whether a chromium browser is available to cloakbrowser.
	BrowserInstalled bool `json:"browser_installed"`
	// MissingSystemLibs lists Linux system packages that appear absent (Debian names).
	MissingSystemLibs []string `json:"missing_system_libs,omitempty"`
	Ready             bool     `json:"ready"`
}

// Installer manages detection and installation of the aistudio-api runtime.
// It shells out to python3/pip; no privileged operations.
type Installer struct {
	cfg Config
}

// NewInstaller builds an installer sharing the manager's config.
func NewInstaller(cfg Config) *Installer {
	if strings.TrimSpace(cfg.PythonBin) == "" {
		cfg.PythonBin = "python3"
	}
	cfg.RuntimeDir = strings.TrimSpace(cfg.RuntimeDir)
	if cfg.RuntimeDir == "" {
		cfg.RuntimeDir = filepath.Join(strings.TrimSpace(cfg.DataDir), runtimeSubdir)
	}
	return &Installer{cfg: cfg}
}

// RuntimeDir returns the configured aistudio-api checkout directory.
func (i *Installer) RuntimeDir() string { return i.cfg.RuntimeDir }

// Detect inspects the host and reports readiness. Never returns an error for
// "not installed" states — those are expressed via the status booleans. Only
// returns an error for unexpected failures (e.g. command exec panics).
func (i *Installer) Detect(ctx context.Context) (*RuntimeStatus, error) {
	st := &RuntimeStatus{RuntimeDir: i.cfg.RuntimeDir, PythonBin: i.cfg.PythonBin}

	// Python presence + version.
	if out, err := runCmd(ctx, i.cfg.PythonBin, "--version"); err == nil {
		st.PythonOK = true
		st.PythonVersion = strings.TrimSpace(string(out))
	} else {
		// python3 --version writes to stderr on some builds; accept either.
		st.PythonVersion = strings.TrimSpace(string(out))
		if st.PythonVersion != "" {
			st.PythonOK = true
		}
	}

	// aistudio-api main.py present?
	mainPy := filepath.Join(i.cfg.RuntimeDir, "main.py")
	if info, err := os.Stat(mainPy); err == nil && !info.IsDir() {
		st.AistudioInstalled = true
	}

	// Python packages importable? Probe a representative set.
	if st.PythonOK {
		code := "import fastapi, httpx, playwright; print('ok')"
		if out, err := runCmd(ctx, i.cfg.PythonBin, "-c", code); err == nil && strings.Contains(string(out), "ok") {
			st.PackagesInstalled = true
		}
		// cloakbrowser is the default browser backend — probe separately.
		if out, err := runCmd(ctx, i.cfg.PythonBin, "-c", "import cloakbrowser; print('ok')"); err == nil && strings.Contains(string(out), "ok") {
			st.BrowserInstalled = true
		}
	}

	// Linux system libs (Debian package names). Best-effort file-presence probe.
	if runtime.GOOS == "linux" {
		st.MissingSystemLibs = missingLinuxLibs()
	}

	st.Ready = st.PythonOK && st.AistudioInstalled && st.PackagesInstalled && st.BrowserInstalled && len(st.MissingSystemLibs) == 0
	return st, nil
}

// InstallResult is returned by Install to summarize what was done.
type InstallResult struct {
	Steps  []string       `json:"steps"`
	Status *RuntimeStatus `json:"status"`
}

// Install performs best-effort installation: pip install requirements into the
// target venv, and run playwright/cloakbrowser browser fetch. The aistudio-api
// source checkout itself is NOT fetched here (M2 expects the operator or a
// future step to place main.py into RuntimeDir); if it is missing, Install
// returns an error instructing where to put it.
//
// logFn (optional) receives incremental progress lines for SSE streaming.
func (i *Installer) Install(ctx context.Context, logFn func(string)) (*InstallResult, error) {
	log := func(line string) {
		if logFn != nil {
			logFn(line)
		}
	}
	res := &InstallResult{Steps: []string{}}

	if _, err := os.Stat(i.cfg.RuntimeDir); err != nil {
		_ = os.MkdirAll(i.cfg.RuntimeDir, 0o755)
	}
	mainPy := filepath.Join(i.cfg.RuntimeDir, "main.py")
	if _, err := os.Stat(mainPy); err != nil {
		return nil, fmt.Errorf("aistudio-api main.py not found at %s — please place the aistudio-api checkout (containing main.py) there first", i.cfg.RuntimeDir)
	}

	reqFile := filepath.Join(i.cfg.RuntimeDir, "requirements.txt")
	if _, err := os.Stat(reqFile); err != nil {
		return nil, fmt.Errorf("requirements.txt not found at %s — ensure the full aistudio-api checkout is present", reqFile)
	}

	log("Installing python dependencies via pip (this may take a few minutes)...")
	if out, err := runCmdWithLog(ctx, log, i.cfg.PythonBin, "-m", "pip", "install", "-r", reqFile); err != nil {
		return nil, fmt.Errorf("pip install failed: %w\n%s", err, string(out))
	}
	res.Steps = append(res.Steps, "pip install -r requirements.txt")

	log("Installing browser binary via cloakbrowser/playwright (downloads ~150MB)...")
	// cloakbrowser bundles its own browser fetch; fall back to playwright install chromium.
	if _, err := runCmdWithLog(ctx, log, i.cfg.PythonBin, "-m", "cloakbrowser", "fetch"); err != nil {
		log("cloakbrowser fetch unavailable, trying playwright install chromium...")
		if out, err := runCmdWithLog(ctx, log, i.cfg.PythonBin, "-m", "playwright", "install", "chromium"); err != nil {
			return nil, fmt.Errorf("browser install failed: %w\n%s", err, string(out))
		}
	}
	res.Steps = append(res.Steps, "browser install")

	// Re-detect to reflect new state.
	status, _ := i.Detect(ctx)
	res.Status = status
	if status == nil || !status.Ready {
		log("Install completed but runtime is not fully ready — see status for details.")
	}
	return res, nil
}

// runCmd runs a command and returns combined stdout+stderr.
func runCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return out, err
}

// runCmdWithLog runs a command, streaming combined output line-by-line to logFn.
func runCmdWithLog(ctx context.Context, logFn func(string), name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	// Capture full output for error reporting while streaming a digest to logFn.
	cmd.Stdout = &logWriter{fn: logFn}
	cmd.Stderr = &logWriter{fn: logFn}
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return nil, nil
}

type logWriter struct {
	fn  func(string)
	buf []byte
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := -1
		for i, b := range w.buf {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:idx]), "\r")
		w.buf = w.buf[idx+1:]
		if w.fn != nil && line != "" {
			w.fn(line)
		}
	}
	return len(p), nil
}

// linuxLibProbes maps a Debian system library path to its package name.
// These are the standard Playwright/Chromium runtime deps on Debian/Ubuntu.
var linuxLibProbes = []struct {
	path string
	pkg  string
}{
	{"/usr/lib/x86_64-linux-gnu/libnss3.so", "libnss3"},
	{"/usr/lib/x86_64-linux-gnu/libnspr4.so", "libnspr4"},
	{"/usr/lib/x86_64-linux-gnu/libgtk-3.so.0", "libgtk-3-0"},
	{"/usr/lib/x86_64-linux-gnu/libgbm.so.1", "libgbm1"},
	{"/usr/lib/x86_64-linux-gnu/libasound.so.2", "libasound2"},
	{"/usr/lib/x86_64-linux-gnu/libdrm.so.2", "libdrm2"},
	{"/usr/lib/x86_64-linux-gnu/libatspi.so.0", "libatk-bridge2.0-0"},
	{"/usr/lib/x86_64-linux-gnu/libXcomposite.so.1", "libxcomposite1"},
	{"/usr/lib/x86_64-linux-gnu/libXdamage.so.1", "libxdamage1"},
	{"/usr/lib/x86_64-linux-gnu/libXrandr.so.2", "libxrandr2"},
	{"/usr/lib/x86_64-linux-gnu/libxkbcommon.so.0", "libxkbcommon0"},
}

func missingLinuxLibs() []string {
	var missing []string
	for _, probe := range linuxLibProbes {
		if _, err := os.Stat(probe.path); err != nil {
			missing = append(missing, probe.pkg)
		}
	}
	return missing
}

// ErrInstallerNotReady is returned when callers attempt to use the runtime before install.
var ErrInstallerNotReady = errors.New("aistudio-api runtime is not installed; run the installer first")
