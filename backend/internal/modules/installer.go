package modules

import (
	"archive/tar"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"gopkg.in/yaml.v3"
)

type packageInstaller struct {
	dataDir  string
	store    Store
	verifier SignatureVerifier
}

func NewPackageInstallerWithVerifier(dataDir string, store Store, verifier SignatureVerifier) Installer {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "data"
	}
	return &packageInstaller{dataDir: dataDir, store: store, verifier: verifier}
}
func InstallDir(dataDir, moduleID, version string) string {
	return filepath.Join(dataDir, "modules", moduleID, version)
}
func (p *packageInstaller) InstallArchive(ctx context.Context, archivePath string) (*InstalledModule, error) {
	tmp, err := os.MkdirTemp("", "lightbridge-module-install-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := extractTarZst(archivePath, tmp); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(tmp, "module.yaml"))
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if err := ValidateManifest(manifest); err != nil {
		return nil, err
	}
	if p.verifier != nil {
		checksums, err := os.ReadFile(filepath.Join(tmp, "checksums.txt"))
		if err != nil {
			return nil, err
		}
		sig, err := os.ReadFile(filepath.Join(tmp, "signature.sig"))
		if err != nil {
			return nil, err
		}
		if err := p.verifier.Verify(checksums, strings.TrimSpace(string(sig))); err != nil {
			return nil, err
		}
	}
	installPath := InstallDir(p.dataDir, manifest.ID, manifest.Version)
	_ = os.RemoveAll(installPath)
	if err := os.MkdirAll(filepath.Dir(installPath), 0o755); err != nil {
		return nil, err
	}
	if err := copyDir(tmp, installPath); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	installed := InstalledModule{ID: manifest.ID, Name: manifest.Name, Type: manifest.Type, Version: manifest.Version, Status: ModuleStatusInstalled, InstallPath: installPath, Manifest: manifest, InstalledAt: now}
	if p.store != nil {
		if err := p.store.SaveInstalled(ctx, installed); err != nil {
			return nil, err
		}
		if err := p.store.SavePermissions(ctx, manifest.ID, permissionRecords(manifest)); err != nil {
			return nil, err
		}
	}
	return &installed, nil
}

func (p *packageInstaller) VerifyInstalled(ctx context.Context, module InstalledModule) (*InstalledModule, error) {
	if p == nil || p.verifier == nil {
		return nil, errors.New("module package verifier is not configured")
	}
	installPath := strings.TrimSpace(module.InstallPath)
	if installPath == "" {
		installPath = InstallDir(p.dataDir, module.ID, module.Version)
	}
	checksums, err := os.ReadFile(filepath.Join(installPath, "checksums.txt"))
	if err != nil {
		return nil, err
	}
	sig, err := os.ReadFile(filepath.Join(installPath, "signature.sig"))
	if err != nil {
		return nil, err
	}
	if err := p.verifier.Verify(checksums, strings.TrimSpace(string(sig))); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(installPath, "module.yaml"))
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if err := ValidateManifest(manifest); err != nil {
		return nil, err
	}
	verified := module
	verified.ID = manifest.ID
	verified.Name = manifest.Name
	verified.Type = manifest.Type
	verified.Version = manifest.Version
	verified.InstallPath = installPath
	verified.Manifest = manifest
	if verified.InstalledAt.IsZero() {
		verified.InstalledAt = time.Now().UTC()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &verified, nil
}

type ed25519Verifier struct{ pub ed25519.PublicKey }

func NewEd25519SignatureVerifierFromFile(path string) (SignatureVerifier, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	b, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, err
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid ed25519 public key size")
	}
	return &ed25519Verifier{pub: ed25519.PublicKey(b)}, nil
}
func (v *ed25519Verifier) Verify(message []byte, signatureHex string) error {
	sig, err := hex.DecodeString(strings.TrimSpace(signatureHex))
	if err != nil {
		return err
	}
	if !ed25519.Verify(v.pub, message, sig) {
		return errors.New("signature verification failed")
	}
	return nil
}
func permissionRecords(m Manifest) []PermissionRecord {
	now := time.Now().UTC()
	var out []PermissionRecord
	for typ, vals := range m.Permissions {
		for _, val := range vals {
			out = append(out, PermissionRecord{ModuleID: m.ID, PermissionType: typ, PermissionValue: val, CreatedAt: now})
		}
	}
	return out
}
func extractTarZst(archivePath, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := zstd.NewReader(f)
	if err != nil {
		return err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.Clean(h.Name))
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe archive path %s", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(h.Mode))
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
}
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		defer out.Close()
		if _, err = io.Copy(out, in); err != nil {
			return err
		}
		return os.Chmod(target, mode)
	})
}
