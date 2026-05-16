package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const latestReleaseURL = "https://api.github.com/repos/paopaoandlingyia/PrismCat/releases/latest"

type Info struct {
	CurrentVersion  string    `json:"current_version"`
	LatestVersion   string    `json:"latest_version"`
	LatestTag       string    `json:"latest_tag"`
	UpdateAvailable bool      `json:"update_available"`
	ReleaseURL      string    `json:"release_url"`
	PublishedAt     time.Time `json:"published_at"`
	Platform        string    `json:"platform"`
	Arch            string    `json:"arch"`
	Assets          []Asset   `json:"assets"`
	MatchingAsset   *Asset    `json:"matching_asset,omitempty"`
}

type Asset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
	Size        int64  `json:"size"`
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	HTMLURL     string        `json:"html_url"`
	PublishedAt time.Time     `json:"published_at"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type Checker struct {
	client *http.Client
}

func NewChecker() *Checker {
	return &Checker{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Checker) Check(ctx context.Context, currentVersion string) (Info, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return Info{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "PrismCat/"+normalizeVersion(currentVersion))

	resp, err := c.client.Do(req)
	if err != nil {
		return Info{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Info{}, fmt.Errorf("GitHub releases API returned %s", resp.Status)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return Info{}, err
	}

	info := Info{
		CurrentVersion:  normalizeVersion(currentVersion),
		LatestVersion:   normalizeVersion(release.TagName),
		LatestTag:       release.TagName,
		UpdateAvailable: compareVersions(release.TagName, currentVersion) > 0,
		ReleaseURL:      release.HTMLURL,
		PublishedAt:     release.PublishedAt,
		Platform:        runtime.GOOS,
		Arch:            runtime.GOARCH,
		Assets:          make([]Asset, 0, len(release.Assets)),
	}

	for _, item := range release.Assets {
		asset := Asset{
			Name:        item.Name,
			DownloadURL: item.BrowserDownloadURL,
			Size:        item.Size,
		}
		info.Assets = append(info.Assets, asset)
		if info.MatchingAsset == nil && matchesCurrentPlatform(asset.Name) {
			copy := asset
			info.MatchingAsset = &copy
		}
	}

	return info, nil
}

func matchesCurrentPlatform(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, runtime.GOOS+"-"+runtime.GOARCH)
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	if version == "" {
		return "0.0.0"
	}
	return version
}

func compareVersions(left, right string) int {
	l := parseVersion(left)
	r := parseVersion(right)
	for i := 0; i < len(l) && i < len(r); i++ {
		if l[i] > r[i] {
			return 1
		}
		if l[i] < r[i] {
			return -1
		}
	}
	return 0
}

func parseVersion(version string) [3]int {
	normalized := normalizeVersion(version)
	if before, _, ok := strings.Cut(normalized, "-"); ok {
		normalized = before
	}
	parts := strings.Split(normalized, ".")

	var parsed [3]int
	for i := 0; i < len(parts) && i < len(parsed); i++ {
		value, err := strconv.Atoi(parts[i])
		if err != nil {
			return [3]int{}
		}
		parsed[i] = value
	}
	return parsed
}
