package modulemigration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wei-Shaw/LightBridge/internal/config"
)

const (
	DefaultModuleMigrationRegistryURL = config.DefaultModuleMarketplaceRegistryURL
	DefaultOpenAIModuleID             = "openai"
	DefaultOpenAIModuleVersion        = "0.1.1"
)

type Registry struct {
	Modules []RegistryEntry `json:"modules"`
}

type RegistryEntry struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	DownloadURL string `json:"downloadUrl"`
	SHA256      string `json:"sha256,omitempty"`
}

func RunAutoOpenAIModuleMigration(ctx context.Context, cfg *config.Config) (*Report, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if !hasLegacyOpenAIAccounts(ctx, db) {
		return &Report{SourceKind: SourceLightBridge, OpenAIModuleStatus: "not_required"}, nil
	}
	moduleAlreadyInstalled := isOpenAIModuleInstalled(ctx, db)

	var packagePath string
	var publicKeyPath string
	if !moduleAlreadyInstalled {
		resolved, err := ResolveOpenAIModulePackageFromConfig(ctx, cfg)
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(resolved.Workspace) }()
		packagePath = resolved.PackagePath
		publicKeyPath = resolved.PublicKeyPath
	}

	opts := Options{
		SourceKind:                SourceLightBridge,
		SourceDriver:              "postgres",
		SourceDSN:                 cfg.Database.DSN(),
		TargetDriver:              "postgres",
		TargetDSN:                 cfg.Database.DSN(),
		OpenAIModulePackage:       packagePath,
		OpenAIModulePublicKeyPath: publicKeyPath,
		ModuleDataDir:             cfg.Modules.DataDir,
		InstallOpenAIModule:       !moduleAlreadyInstalled,
		EnableOpenAIModule:        true,
		// Source and target are the same database: convert legacy OpenAI
		// accounts in place rather than duplicating them into module copies.
		SameDatabase: true,
	}
	return Run(ctx, opts)
}

func hasLegacyOpenAIAccounts(ctx context.Context, db *sql.DB) bool {
	var count int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM accounts
WHERE deleted_at IS NULL
  AND platform = 'openai'`).Scan(&count)
	if err == nil && count > 0 {
		return true
	}
	err = db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM accounts
WHERE deleted_at IS NULL
  AND platform = 'openai'
  AND COALESCE(provider_id, '') = 'openai'`).Scan(&count)
	return err == nil && count > 0
}

func isOpenAIModuleInstalled(ctx context.Context, db *sql.DB) bool {
	var count int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM installed_modules
WHERE id = $1 AND status IN ('installed', 'enabled', 'disabled')`, DefaultOpenAIModuleID).Scan(&count)
	return err == nil && count > 0
}

func fetchModuleRegistry(ctx context.Context, url string, timeoutSeconds int) (*Registry, error) {
	pathOrURL := strings.TrimSpace(url)
	if pathOrURL == "" {
		return nil, fmt.Errorf("module registry URL is required")
	}
	var data []byte
	if strings.HasPrefix(pathOrURL, "http://") || strings.HasPrefix(pathOrURL, "https://") {
		client := &http.Client{Timeout: effectiveTimeout(timeoutSeconds)}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pathOrURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("download module registry: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("download module registry: %s", resp.Status)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		if err != nil {
			return nil, err
		}
	} else {
		content, err := os.ReadFile(pathOrURL)
		if err != nil {
			return nil, err
		}
		data = content
	}
	var registry Registry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("parse module registry: %w", err)
	}
	return &registry, nil
}

func selectRegistryEntry(registry *Registry, id string, version string) (RegistryEntry, bool) {
	if registry == nil {
		return RegistryEntry{}, false
	}
	for _, entry := range registry.Modules {
		if strings.TrimSpace(entry.ID) == id && strings.TrimSpace(entry.Version) == version {
			return entry, true
		}
	}
	return RegistryEntry{}, false
}

func downloadFile(ctx context.Context, url string, targetPath string, timeoutSeconds int) error {
	if strings.HasPrefix(url, "file://") {
		url = strings.TrimPrefix(url, "file://")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		in, err := os.Open(url)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		return writeReaderToFile(in, targetPath)
	}
	client := &http.Client{Timeout: effectiveTimeout(timeoutSeconds)}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s", resp.Status)
	}
	return writeReaderToFile(resp.Body, targetPath)
}

func writeReaderToFile(in io.Reader, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func registryAssetURL(registryURL string, assetName string) string {
	idx := strings.LastIndex(registryURL, "/")
	if idx < 0 {
		return assetName
	}
	return registryURL[:idx+1] + assetName
}

func effectiveTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = 20
	}
	return time.Duration(seconds) * time.Second
}

func verifyPackageSHA256(path string, expected string) error {
	actual, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(expected), actual) {
		return fmt.Errorf("module package checksum mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
