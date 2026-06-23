//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateServiceCacheStub struct {
	data string
}

func (s *updateServiceCacheStub) GetUpdateInfo(context.Context) (string, error) {
	if s.data == "" {
		return "", errors.New("cache miss")
	}
	return s.data, nil
}

func (s *updateServiceCacheStub) SetUpdateInfo(_ context.Context, data string, _ time.Duration) error {
	s.data = data
	return nil
}

type updateServiceGitHubClientStub struct {
	release  *GitHubRelease
	releases []GitHubRelease
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(context.Context, string) (*GitHubRelease, error) {
	return s.release, nil
}

func (s *updateServiceGitHubClientStub) FetchReleases(context.Context, string, int) ([]GitHubRelease, error) {
	if s.releases != nil {
		return s.releases, nil
	}
	if s.release == nil {
		return nil, nil
	}
	return []GitHubRelease{*s.release}, nil
}

func (s *updateServiceGitHubClientStub) DownloadFile(context.Context, string, string, int64) error {
	panic("DownloadFile should not be called when no update is available")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	panic("FetchChecksumFile should not be called when no update is available")
}

func TestUpdateServicePerformUpdateNoUpdateReturnsSentinel(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.132",
				Name:    "v0.1.132",
			},
		},
		"0.1.132",
		"release",
	)

	err := svc.PerformUpdate(context.Background())

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoUpdateAvailable))
	require.ErrorIs(t, err, ErrNoUpdateAvailable)
}

func TestUpdateServiceListVersionReleasesIncludesHistory(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.1",
				Name:    "v0.1.1",
				Body:    "latest",
			},
			releases: []GitHubRelease{
				{
					TagName:     "v0.1.1",
					Name:        "v0.1.1",
					Body:        "latest",
					PublishedAt: "2026-06-07T00:00:00Z",
					HTMLURL:     "https://example.test/releases/v0.1.1",
				},
				{
					TagName:     "v0.0.1",
					Name:        "v0.0.1",
					Body:        "first",
					PublishedAt: "2026-05-01T00:00:00Z",
					HTMLURL:     "https://example.test/releases/v0.0.1",
				},
			},
		},
		"0.0.1",
		"release",
	)

	releases, info, err := svc.ListVersionReleases(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, "0.1.1", info.LatestVersion)
	require.Len(t, releases, 2)
	require.Equal(t, "0.1.1", releases[0].Version)
	require.True(t, releases[0].Latest)
	require.False(t, releases[0].Current)
	require.Equal(t, "0.0.1", releases[1].Version)
	require.True(t, releases[1].Current)
	require.False(t, releases[1].Latest)
}

func TestUpdateServiceListVersionReleasesIncludesPreview(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{TagName: "v0.2.4", Name: "v0.2.4"},
			releases: []GitHubRelease{
				{
					TagName:     "v0.2.4-preview",
					Name:        "v0.2.4-preview",
					Prerelease:  true,
					PublishedAt: "2026-06-10T00:00:00Z",
				},
				{
					TagName:     "v0.2.4",
					Name:        "v0.2.4",
					PublishedAt: "2026-06-09T00:00:00Z",
				},
			},
		},
		"0.2.3",
		"release",
	)

	releases, _, err := svc.ListVersionReleases(context.Background(), true)

	require.NoError(t, err)
	require.Len(t, releases, 2)
	require.Equal(t, "0.2.4-preview", releases[0].Version)
	require.True(t, releases[0].Prerelease)
	require.Equal(t, "0.2.4", releases[1].Version)
	require.False(t, releases[1].Prerelease)
}

func TestUpdateServicePreviewProductionSameNumberAreInstallable(t *testing.T) {
	// 当前为正式版 0.2.4，应允许安装同号的 preview 0.2.4-preview（HasUpdate=true）。
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			releases: []GitHubRelease{
				{TagName: "v0.2.4-preview", Name: "v0.2.4-preview", Prerelease: true},
				{TagName: "v0.2.4", Name: "v0.2.4"},
			},
		},
		"0.2.4",
		"release",
	)

	info, err := svc.updateInfoForTargetVersion(context.Background(), "0.2.4-preview")

	require.NoError(t, err)
	require.True(t, info.HasUpdate)
	require.Equal(t, "0.2.4-preview", info.LatestVersion)
}

func TestParseVersionStripsPrereleaseSuffix(t *testing.T) {
	require.Equal(t, [3]int{0, 2, 4}, parseVersion("0.2.4-preview"))
	require.Equal(t, [3]int{0, 2, 4}, parseVersion("v0.2.4-rc.1"))
	require.Equal(t, [3]int{1, 2, 3}, parseVersion("1.2.3+build.5"))
	require.Equal(t, 0, compareVersions("0.2.4-preview", "0.2.4"))
}

func TestSelectUpdateAssetPreviewPrefersIncrementalBinary(t *testing.T) {
	svc := NewUpdateService(&updateServiceCacheStub{}, &updateServiceGitHubClientStub{}, "0.2.3", "release")
	archiveName := fmt.Sprintf("LightBridge_0.2.4-preview_%s.tar.gz", svc.getArchiveName())
	binaryName := fmt.Sprintf("LightBridge_0.2.4-preview_%s", svc.getArchiveName())
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	info := &UpdateInfo{
		LatestVersion: "0.2.4-preview",
		ReleaseInfo: &ReleaseInfo{
			Prerelease: true,
			Assets: []Asset{
				{Name: archiveName, DownloadURL: "https://github.com/example/archive"},
				{Name: binaryName, DownloadURL: "https://github.com/example/binary"},
				{Name: "checksums.txt", DownloadURL: "https://github.com/example/checksums"},
			},
		},
	}

	asset, checksumURL, directBinary := svc.selectUpdateAsset(info)

	require.NotNil(t, asset)
	require.Equal(t, binaryName, asset.Name)
	require.Equal(t, "https://github.com/example/checksums", checksumURL)
	require.True(t, directBinary)
}

func TestSelectUpdateAssetProductionUsesArchive(t *testing.T) {
	svc := NewUpdateService(&updateServiceCacheStub{}, &updateServiceGitHubClientStub{}, "0.2.3", "release")
	binaryName := fmt.Sprintf("LightBridge_0.2.4_%s", svc.getArchiveName())
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	archiveName := fmt.Sprintf("LightBridge_0.2.4_%s.tar.gz", svc.getArchiveName())
	info := &UpdateInfo{
		LatestVersion: "0.2.4",
		ReleaseInfo: &ReleaseInfo{
			Prerelease: false,
			Assets: []Asset{
				{Name: binaryName, DownloadURL: "https://github.com/example/binary"},
				{Name: archiveName, DownloadURL: "https://github.com/example/archive"},
			},
		},
	}

	asset, _, directBinary := svc.selectUpdateAsset(info)

	require.NotNil(t, asset)
	require.Equal(t, archiveName, asset.Name)
	require.False(t, directBinary)
}

func TestSelectUpdateAssetPreviewFallsBackToArchive(t *testing.T) {
	svc := NewUpdateService(&updateServiceCacheStub{}, &updateServiceGitHubClientStub{}, "0.2.3", "release")
	archiveName := fmt.Sprintf("LightBridge_0.2.4-preview_%s.tar.gz", svc.getArchiveName())
	info := &UpdateInfo{
		LatestVersion: "0.2.4-preview",
		ReleaseInfo: &ReleaseInfo{
			Prerelease: true,
			Assets: []Asset{
				{Name: archiveName, DownloadURL: "https://github.com/example/archive"},
			},
		},
	}

	asset, _, directBinary := svc.selectUpdateAsset(info)

	require.NotNil(t, asset)
	require.Equal(t, archiveName, asset.Name)
	require.False(t, directBinary)
}
