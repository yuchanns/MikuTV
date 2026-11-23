package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
	"golang.org/x/mod/semver"
)

const (
	DefaultGitHubAPIBase   = "https://api.github.com"
	DiscoveryTopic         = "mikutv-plugin"
	DiscoveryQuery         = "topic:mikutv-plugin archived:false"
	CatalogPageSize        = 20
	MaxInspectArchiveBytes = 8 << 20
	inspectConcurrency     = 4
)

var (
	ErrInvalidRepository  = errors.New("invalid github repository")
	ErrNoOfficialRelease  = errors.New("no official release with plugin.zip")
	ErrVersionTagMismatch = errors.New("release tag does not match manifest version")
	ErrDigestMismatch     = errors.New("downloaded archive digest mismatch")
	ErrSourceConflict     = errors.New("plugin id is already bound to another source")
	ErrKindChanged        = errors.New("plugin update cannot change kind")
)

func ParseRepository(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	lower := strings.ToLower(s)
	hadGitHubHost := false
	for _, prefix := range []string{
		"https://www.github.com/",
		"http://www.github.com/",
		"https://github.com/",
		"http://github.com/",
		"www.github.com/",
		"github.com/",
	} {
		if strings.HasPrefix(lower, prefix) {
			s = s[len(prefix):]
			hadGitHubHost = true
			break
		}
	}
	s = strings.Trim(s, "/")
	parts := strings.Split(s, "/")
	if hadGitHubHost {
		if len(parts) < 2 {
			return "", false
		}
		parts = parts[:2]
	} else if len(parts) != 2 {
		return "", false
	}
	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	if !validRepoSegment(owner) || !validRepoSegment(repo) {
		return "", false
	}
	return owner + "/" + repo, true
}

func validRepoSegment(seg string) bool {
	if seg == "" || len(seg) > 100 || seg == "." || seg == ".." {
		return false
	}
	for i := range len(seg) {
		ch := seg[i]
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func ReleaseVersionMatches(tagName, manifestVersion string) bool {
	tag := strings.TrimSpace(tagName)
	ver := strings.TrimSpace(manifestVersion)
	if ver == "" {
		return false
	}
	if tag == "" {
		return true
	}
	tag = strings.TrimPrefix(strings.TrimPrefix(tag, "v"), "V")
	return tag == ver
}

// CompareVersions compares semantic versions candidate vs current.
// Returns +1 if candidate > current, 0 if equal, -1 if candidate < current.
func CompareVersions(current, candidate string) int {
	cur := strings.TrimPrefix(strings.TrimSpace(current), "v")
	cand := strings.TrimPrefix(strings.TrimSpace(candidate), "v")
	if !validVersion(cur) || !validVersion(cand) {
		return 0
	}
	return semver.Compare("v"+cand, "v"+cur)
}

type CompatibilityCode string

const (
	CompatibleOK                 CompatibilityCode = "compatible"
	IncompatibleProtocol         CompatibilityCode = "protocol"
	IncompatibleAppVersion       CompatibilityCode = "app_version"
	IncompatibleThemeUnavailable CompatibilityCode = "theme_unavailable"
	IncompatibleKind             CompatibilityCode = "unsupported_kind"
	IncompatibleAuth             CompatibilityCode = "unsupported_auth"
	IncompatibleManifest         CompatibilityCode = "invalid_manifest"
)

type Compatibility struct {
	Compatible         bool              `json:"compatible"`
	Code               CompatibilityCode `json:"code"`
	RequiredAppVersion string            `json:"requiredAppVersion,omitempty"`
	Detail             string            `json:"detail,omitempty"`
}

type Inspection struct {
	Manifest           Manifest
	NetworkPermissions []string
	Capabilities       provider.Capabilities
	Swatches           [6]theme.Color
	Language           string
	NativeName         string
	Compatibility      Compatibility
	Digest             [sha256.Size]byte
}

type ReleaseAsset struct {
	ReleaseID   int64  `json:"releaseId"`
	TagName     string `json:"tagName"`
	PublishedAt string `json:"publishedAt,omitempty"`
	AssetID     int64  `json:"assetId"`
	AssetName   string `json:"assetName"`
	DownloadURL string `json:"downloadUrl"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256,omitempty"`
}

type CatalogItem struct {
	Repository      string        `json:"repository"`
	RepoID          int64         `json:"repoId"`
	Owner           string        `json:"owner"`
	HTMLURL         string        `json:"htmlUrl,omitempty"`
	RepoDescription string        `json:"repoDescription,omitempty"`
	Stars           int           `json:"stars,omitempty"`
	UpdatedAt       string        `json:"updatedAt,omitempty"`
	Release         ReleaseAsset  `json:"release"`
	Manifest        Manifest        `json:"manifest"`
	Permissions     []string        `json:"permissions,omitempty"`
	Swatches        [6]theme.Color  `json:"swatches,omitempty"`
	Language        string          `json:"language,omitempty"`
	NativeName      string          `json:"nativeName,omitempty"`
	Compatibility   Compatibility   `json:"compatibility"`
}

type CatalogPage struct {
	Items       []CatalogItem
	Page        int
	TotalCount  int
	HasMore     bool
	ETag        string
	NotModified bool
}

type RateLimitError struct {
	ResetAt    time.Time
	RetryAfter time.Duration
	Message    string
}

func (e *RateLimitError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "github api rate limit exceeded"
}

// InspectPackage validates archive structure and reads plugin.json without
// executing JavaScript. Kind and compatibility are derived strictly from the
// archive's manifest rather than repository metadata.
func InspectPackage(data []byte, appVersion string) (Inspection, error) {
	if len(data) == 0 {
		return Inspection{}, errors.New("empty plugin package")
	}
	digest := sha256.Sum256(data)
	files, err := readArchive(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Inspection{}, err
	}
	header, err := files.read("plugin.json")
	if err != nil {
		return Inspection{}, fmt.Errorf("plugin.json: %w", err)
	}
	manifest, err := InspectManifest(header)
	if err != nil {
		return Inspection{}, err
	}
	result := Inspection{
		Manifest: manifest,
		Digest:   digest,
	}
	if !validVersion(appVersion) {
		return Inspection{}, fmt.Errorf("invalid host version %q", appVersion)
	}
	if manifest.APIVersion != protocolVersion {
		result.Compatibility = Compatibility{
			Compatible: false,
			Code:       IncompatibleProtocol,
			Detail:     fmt.Sprintf("apiVersion %d", manifest.APIVersion),
		}
		return result, nil
	}
	if semver.Compare("v"+appVersion, "v"+manifest.MinAppVersion) < 0 {
		result.Compatibility = Compatibility{
			Compatible:         false,
			Code:               IncompatibleAppVersion,
			RequiredAppVersion: manifest.MinAppVersion,
		}
		return result, nil
	}
	switch manifest.Kind {
	case SourceKind:
		src, err := decodeSource(header, appVersion)
		if err != nil {
			code := IncompatibleManifest
			if errors.Is(err, ErrIncompatible) && strings.Contains(err.Error(), "authentication") {
				code = IncompatibleAuth
			}
			result.Compatibility = Compatibility{
				Compatible: false,
				Code:       code,
				Detail:     err.Error(),
			}
			return result, nil
		}
		result.NetworkPermissions = append([]string(nil), src.Permissions.Network...)
		result.Capabilities = append(provider.Capabilities(nil), src.Capabilities...)
		result.Compatibility = Compatibility{Compatible: true, Code: CompatibleOK}
		return result, nil
	case ThemeKind:
		pkg, err := OpenTheme(data, appVersion)
		if err != nil {
			code := IncompatibleManifest
			if errors.Is(err, ErrIncompatible) || errors.Is(err, theme.ErrUnsupportedSchema) {
				code = IncompatibleProtocol
			}
			result.Compatibility = Compatibility{
				Compatible: false,
				Code:       code,
				Detail:     err.Error(),
			}
			return result, nil
		}
		result.Swatches = pkg.Theme().Swatches()
		result.Compatibility = Compatibility{Compatible: true, Code: CompatibleOK}
		return result, nil
	case LocaleKind:
		pkg, err := OpenLocale(data, appVersion)
		if err != nil {
			code := IncompatibleManifest
			if errors.Is(err, ErrIncompatible) {
				code = IncompatibleProtocol
			}
			result.Compatibility = Compatibility{
				Compatible: false,
				Code:       code,
				Detail:     err.Error(),
			}
			return result, nil
		}
		result.Language = pkg.Language()
		result.NativeName = pkg.NativeName()
		result.Compatibility = Compatibility{Compatible: true, Code: CompatibleOK}
		return result, nil
	default:
		result.Compatibility = Compatibility{
			Compatible: false,
			Code:       IncompatibleKind,
			Detail:     string(manifest.Kind),
		}
		return result, nil
	}
}

type CatalogClient struct {
	HTTPClient        *http.Client
	APIBase           string
	AppVersion        string
	UserAgent         string
	AllowInsecureHTTP bool

	mu           sync.Mutex
	inspectCache map[string]CatalogItem
}

func NewCatalogClient(appVersion, apiBase string) *CatalogClient {
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	allowInsecure := false
	if base == "" {
		base = DefaultGitHubAPIBase
	} else if parsed, err := url.Parse(base); err == nil && strings.EqualFold(parsed.Scheme, "http") {
		allowInsecure = true
	}
	return &CatalogClient{
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		APIBase:    base,
		AppVersion: appVersion,
		UserAgent:  "MikuTV/" + appVersion,
		AllowInsecureHTTP: allowInsecure,
		inspectCache:      make(map[string]CatalogItem),
	}
}

func (c *CatalogClient) SeedInspectCache(items []CatalogItem) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inspectCache == nil {
		c.inspectCache = make(map[string]CatalogItem)
	}
	for _, item := range items {
		key := inspectCacheKey(item.Repository, item.Release.ReleaseID, item.Release.AssetID)
		if key != "" && item.Manifest.ID != "" {
			c.inspectCache[key] = item
		}
	}
}

func inspectCacheKey(repo string, releaseID, assetID int64) string {
	norm := strings.ToLower(strings.TrimSpace(repo))
	if norm == "" || releaseID <= 0 || assetID <= 0 {
		return ""
	}
	return fmt.Sprintf("%s\x00%d\x00%d", norm, releaseID, assetID)
}

func (c *CatalogClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *CatalogClient) apiBase() string {
	if strings.TrimSpace(c.APIBase) == "" {
		return DefaultGitHubAPIBase
	}
	return strings.TrimRight(strings.TrimSpace(c.APIBase), "/")
}

func (c *CatalogClient) userAgent() string {
	if strings.TrimSpace(c.UserAgent) != "" {
		return c.UserAgent
	}
	return "MikuTV/" + c.AppVersion
}

type githubSearchResponse struct {
	TotalCount        int               `json:"total_count"`
	IncompleteResults bool              `json:"incomplete_results"`
	Items             []githubRepoEntry `json:"items"`
}

type githubRepoEntry struct {
	ID          int64  `json:"id"`
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	HTMLURL     string `json:"html_url"`
	Archived    bool   `json:"archived"`
	Disabled    bool   `json:"disabled"`
	Stars       int    `json:"stargazers_count"`
	UpdatedAt   string `json:"updated_at"`
	Owner       struct {
		Login string `json:"login"`
	} `json:"owner"`
}

type githubReleaseEntry struct {
	ID          int64  `json:"id"`
	TagName     string `json:"tag_name"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		ID                 int64  `json:"id"`
		Name               string `json:"name"`
		Size               int64  `json:"size"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func (c *CatalogClient) FetchPage(ctx context.Context, page int, etag string) (CatalogPage, error) {
	if page < 1 {
		page = 1
	}
	params := url.Values{}
	params.Set("q", DiscoveryQuery)
	params.Set("sort", "updated")
	params.Set("order", "desc")
	params.Set("per_page", strconv.Itoa(CatalogPageSize))
	params.Set("page", strconv.Itoa(page))
	endpoint := c.apiBase() + "/search/repositories?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return CatalogPage{}, err
	}
	c.setGitHubHeaders(req)
	if page == 1 && etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return CatalogPage{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return CatalogPage{
			Page:        page,
			ETag:        etag,
			NotModified: true,
		}, nil
	}
	if err := checkGitHubResponse(resp); err != nil {
		return CatalogPage{}, err
	}

	var payload githubSearchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return CatalogPage{}, fmt.Errorf("decode github repository search: %w", err)
	}

	respETag := strings.TrimSpace(resp.Header.Get("ETag"))
	if respETag == "" {
		respETag = etag
	}

	var repos []githubRepoEntry
	seenRepo := make(map[string]bool, len(payload.Items))
	seenID := make(map[int64]bool, len(payload.Items))
	for _, repo := range payload.Items {
		full := strings.TrimSpace(repo.FullName)
		if full == "" || repo.Archived || repo.Disabled {
			continue
		}
		key := strings.ToLower(full)
		if seenRepo[key] || (repo.ID > 0 && seenID[repo.ID]) {
			continue
		}
		seenRepo[key] = true
		if repo.ID > 0 {
			seenID[repo.ID] = true
		}
		repos = append(repos, repo)
	}

	items, err := c.inspectRepositories(ctx, repos)
	if err != nil {
		return CatalogPage{}, err
	}

	hasMore := hasNextLink(resp.Header.Get("Link"))
	if !hasMore && len(payload.Items) >= CatalogPageSize && page*CatalogPageSize < payload.TotalCount && page < 50 {
		hasMore = true
	}
	return CatalogPage{
		Items:      items,
		Page:       page,
		TotalCount: payload.TotalCount,
		HasMore:    hasMore,
		ETag:       respETag,
	}, nil
}

func (c *CatalogClient) inspectRepositories(ctx context.Context, repos []githubRepoEntry) ([]CatalogItem, error) {
	if len(repos) == 0 {
		return nil, nil
	}
	results := make([]*CatalogItem, len(repos))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg        sync.WaitGroup
		sem       = make(chan struct{}, inspectConcurrency)
		errMu     sync.Mutex
		fatalErr  error
	)
	recordFatal := func(err error) {
		errMu.Lock()
		if fatalErr == nil {
			fatalErr = err
			cancel()
		}
		errMu.Unlock()
	}

	for i, repo := range repos {
		wg.Add(1)
		go func(idx int, r githubRepoEntry) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			item, ok, err := c.inspectRepository(ctx, r)
			if err != nil {
				var rateErr *RateLimitError
				if errors.As(err, &rateErr) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					recordFatal(err)
				}
				return
			}
			if ok {
				results[idx] = &item
			}
		}(i, repo)
	}
	wg.Wait()

	if fatalErr != nil {
		return nil, fatalErr
	}
	out := make([]CatalogItem, 0, len(repos))
	for _, item := range results {
		if item != nil {
			out = append(out, *item)
		}
	}
	return out, nil
}

func (c *CatalogClient) inspectRepository(ctx context.Context, repo githubRepoEntry) (CatalogItem, bool, error) {
	parts := strings.Split(repo.FullName, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return CatalogItem{}, false, nil
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=10",
		c.apiBase(), url.PathEscape(parts[0]), url.PathEscape(parts[1]))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return CatalogItem{}, false, err
	}
	c.setGitHubHeaders(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return CatalogItem{}, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return CatalogItem{}, false, nil
	}
	if err := checkGitHubResponse(resp); err != nil {
		return CatalogItem{}, false, err
	}

	var releases []githubReleaseEntry
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&releases); err != nil {
		return CatalogItem{}, false, err
	}

	asset, ok := selectOfficialPluginAsset(releases)
	if !ok {
		return CatalogItem{}, false, nil
	}

	cacheKey := inspectCacheKey(repo.FullName, asset.ReleaseID, asset.AssetID)
	if cacheKey != "" {
		c.mu.Lock()
		cached, hit := c.inspectCache[cacheKey]
		c.mu.Unlock()
		if hit {
			cached.RepoID = repo.ID
			cached.Owner = repo.Owner.Login
			cached.HTMLURL = repo.HTMLURL
			cached.RepoDescription = strings.TrimSpace(repo.Description)
			cached.Stars = repo.Stars
			cached.UpdatedAt = repo.UpdatedAt
			cached.Release.TagName = asset.TagName
			cached.Release.PublishedAt = asset.PublishedAt
			cached.Release.AssetName = asset.AssetName
			cached.Release.DownloadURL = asset.DownloadURL
			cached.Release.Size = asset.Size
			return cached, true, nil
		}
	}

	zipBytes, err := c.downloadArchive(ctx, asset.DownloadURL)
	if err != nil {
		return CatalogItem{}, false, err
	}
	inspected, err := InspectPackage(zipBytes, c.AppVersion)
	if err != nil {
		return CatalogItem{}, false, nil
	}
	if !ReleaseVersionMatches(asset.TagName, inspected.Manifest.Version) {
		return CatalogItem{}, false, nil
	}
	asset.SHA256 = fmt.Sprintf("%x", inspected.Digest)
	item := CatalogItem{
		Repository:      repo.FullName,
		RepoID:          repo.ID,
		Owner:           repo.Owner.Login,
		HTMLURL:         repo.HTMLURL,
		RepoDescription: strings.TrimSpace(repo.Description),
		Stars:           repo.Stars,
		UpdatedAt:       repo.UpdatedAt,
		Release:         asset,
		Manifest:        inspected.Manifest,
		Permissions:     inspected.NetworkPermissions,
		Swatches:        inspected.Swatches,
		Language:        inspected.Language,
		NativeName:      inspected.NativeName,
		Compatibility:   inspected.Compatibility,
	}
	if cacheKey != "" {
		c.mu.Lock()
		if c.inspectCache == nil {
			c.inspectCache = make(map[string]CatalogItem)
		}
		c.inspectCache[cacheKey] = item
		c.mu.Unlock()
	}
	return item, true, nil
}

func selectOfficialPluginAsset(releases []githubReleaseEntry) (ReleaseAsset, bool) {
	for _, rel := range releases {
		if rel.Draft || rel.Prerelease {
			continue
		}
		for _, a := range rel.Assets {
			if !strings.EqualFold(strings.TrimSpace(a.Name), "plugin.zip") {
				continue
			}
			if a.Size <= 0 || a.Size > MaxInspectArchiveBytes || strings.TrimSpace(a.BrowserDownloadURL) == "" {
				continue
			}
			return ReleaseAsset{
				ReleaseID:   rel.ID,
				TagName:     strings.TrimSpace(rel.TagName),
				PublishedAt: strings.TrimSpace(rel.PublishedAt),
				AssetID:     a.ID,
				AssetName:   a.Name,
				DownloadURL: strings.TrimSpace(a.BrowserDownloadURL),
				Size:        a.Size,
			}, true
		}
	}
	return ReleaseAsset{}, false
}

func (c *CatalogClient) ResolveRepository(ctx context.Context, rawRepo string) (CatalogItem, error) {
	canonical, ok := ParseRepository(rawRepo)
	if !ok {
		return CatalogItem{}, ErrInvalidRepository
	}
	parts := strings.Split(canonical, "/")
	repoEndpoint := fmt.Sprintf("%s/repos/%s/%s", c.apiBase(), url.PathEscape(parts[0]), url.PathEscape(parts[1]))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, repoEndpoint, nil)
	if err != nil {
		return CatalogItem{}, err
	}
	c.setGitHubHeaders(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return CatalogItem{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return CatalogItem{}, ErrNoOfficialRelease
	}
	if err := checkGitHubResponse(resp); err != nil {
		return CatalogItem{}, err
	}
	var repo githubRepoEntry
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&repo); err != nil {
		return CatalogItem{}, fmt.Errorf("decode repository: %w", err)
	}
	if repo.FullName == "" {
		repo.FullName = canonical
	}
	if repo.Owner.Login == "" {
		repo.Owner.Login = parts[0]
	}

	relEndpoint := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=10",
		c.apiBase(), url.PathEscape(parts[0]), url.PathEscape(parts[1]))
	relReq, err := http.NewRequestWithContext(ctx, http.MethodGet, relEndpoint, nil)
	if err != nil {
		return CatalogItem{}, err
	}
	c.setGitHubHeaders(relReq)
	relResp, err := c.httpClient().Do(relReq)
	if err != nil {
		return CatalogItem{}, err
	}
	defer relResp.Body.Close()
	if relResp.StatusCode == http.StatusNotFound {
		return CatalogItem{}, ErrNoOfficialRelease
	}
	if err := checkGitHubResponse(relResp); err != nil {
		return CatalogItem{}, err
	}
	var releases []githubReleaseEntry
	if err := json.NewDecoder(io.LimitReader(relResp.Body, 2<<20)).Decode(&releases); err != nil {
		return CatalogItem{}, fmt.Errorf("decode releases: %w", err)
	}
	asset, ok := selectOfficialPluginAsset(releases)
	if !ok {
		return CatalogItem{}, ErrNoOfficialRelease
	}
	zipBytes, err := c.downloadArchive(ctx, asset.DownloadURL)
	if err != nil {
		return CatalogItem{}, err
	}
	inspected, err := InspectPackage(zipBytes, c.AppVersion)
	if err != nil {
		return CatalogItem{}, err
	}
	if !ReleaseVersionMatches(asset.TagName, inspected.Manifest.Version) {
		return CatalogItem{}, fmt.Errorf("%w: release %q vs manifest %q", ErrVersionTagMismatch, asset.TagName, inspected.Manifest.Version)
	}
	asset.SHA256 = fmt.Sprintf("%x", inspected.Digest)
	item := CatalogItem{
		Repository:      repo.FullName,
		RepoID:          repo.ID,
		Owner:           repo.Owner.Login,
		HTMLURL:         repo.HTMLURL,
		RepoDescription: strings.TrimSpace(repo.Description),
		Stars:           repo.Stars,
		UpdatedAt:       repo.UpdatedAt,
		Release:         asset,
		Manifest:        inspected.Manifest,
		Permissions:     inspected.NetworkPermissions,
		Swatches:        inspected.Swatches,
		Language:        inspected.Language,
		NativeName:      inspected.NativeName,
		Compatibility:   inspected.Compatibility,
	}
	if key := inspectCacheKey(item.Repository, asset.ReleaseID, asset.AssetID); key != "" {
		c.mu.Lock()
		if c.inspectCache == nil {
			c.inspectCache = make(map[string]CatalogItem)
		}
		c.inspectCache[key] = item
		c.mu.Unlock()
	}
	return item, nil
}

func (c *CatalogClient) DownloadAsset(ctx context.Context, asset ReleaseAsset) ([]byte, [sha256.Size]byte, error) {
	data, err := c.downloadArchive(ctx, asset.DownloadURL)
	if err != nil {
		return nil, [sha256.Size]byte{}, err
	}
	digest := sha256.Sum256(data)
	if expected := strings.ToLower(strings.TrimSpace(asset.SHA256)); expected != "" {
		if fmt.Sprintf("%x", digest) != expected {
			return nil, [sha256.Size]byte{}, fmt.Errorf("%w: expected %s", ErrDigestMismatch, expected)
		}
	}
	return data, digest, nil
}

// DownloadReleaseAsset fetches the pinned plugin.zip for a previously installed
// repository and release tag (preferring exact assetID when available) and
// enforces expectedSHA256 verification before returning the bytes.
func (c *CatalogClient) DownloadReleaseAsset(ctx context.Context, rawRepo, tagName string, assetID int64, expectedSHA256 string) ([]byte, [sha256.Size]byte, error) {
	canonical, ok := ParseRepository(rawRepo)
	if !ok {
		return nil, [sha256.Size]byte{}, ErrInvalidRepository
	}
	parts := strings.Split(canonical, "/")
	var release *githubReleaseEntry
	if tag := strings.TrimSpace(tagName); tag != "" {
		endpoint := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s",
			c.apiBase(), url.PathEscape(parts[0]), url.PathEscape(parts[1]), url.PathEscape(tag))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, [sha256.Size]byte{}, err
		}
		c.setGitHubHeaders(req)
		resp, err := c.httpClient().Do(req)
		if err != nil {
			return nil, [sha256.Size]byte{}, err
		}
		if resp.StatusCode == http.StatusOK {
			var rel githubReleaseEntry
			decErr := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&rel)
			resp.Body.Close()
			if decErr != nil {
				return nil, [sha256.Size]byte{}, fmt.Errorf("decode release tag: %w", decErr)
			}
			release = &rel
		} else {
			err := checkGitHubResponse(resp)
			status := resp.StatusCode
			resp.Body.Close()
			if status != http.StatusNotFound {
				return nil, [sha256.Size]byte{}, err
			}
		}
	}

	var candidates []githubReleaseEntry
	if release != nil {
		candidates = []githubReleaseEntry{*release}
	} else {
		endpoint := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=20",
			c.apiBase(), url.PathEscape(parts[0]), url.PathEscape(parts[1]))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, [sha256.Size]byte{}, err
		}
		c.setGitHubHeaders(req)
		resp, err := c.httpClient().Do(req)
		if err != nil {
			return nil, [sha256.Size]byte{}, err
		}
		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			return nil, [sha256.Size]byte{}, ErrNoOfficialRelease
		}
		if err := checkGitHubResponse(resp); err != nil {
			resp.Body.Close()
			return nil, [sha256.Size]byte{}, err
		}
		decErr := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&candidates)
		resp.Body.Close()
		if decErr != nil {
			return nil, [sha256.Size]byte{}, fmt.Errorf("decode releases: %w", decErr)
		}
	}

	var matched ReleaseAsset
	found := false
	if assetID > 0 {
		for _, rel := range candidates {
			if rel.Draft {
				continue
			}
			for _, a := range rel.Assets {
				if a.ID == assetID && strings.EqualFold(strings.TrimSpace(a.Name), "plugin.zip") &&
					a.Size > 0 && a.Size <= MaxInspectArchiveBytes && strings.TrimSpace(a.BrowserDownloadURL) != "" {
					matched = ReleaseAsset{
						ReleaseID:   rel.ID,
						TagName:     strings.TrimSpace(rel.TagName),
						PublishedAt: strings.TrimSpace(rel.PublishedAt),
						AssetID:     a.ID,
						AssetName:   a.Name,
						DownloadURL: strings.TrimSpace(a.BrowserDownloadURL),
						Size:        a.Size,
					}
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	}
	if !found {
		matched, found = selectOfficialPluginAsset(candidates)
	}
	if !found {
		return nil, [sha256.Size]byte{}, ErrNoOfficialRelease
	}
	matched.SHA256 = strings.TrimSpace(expectedSHA256)
	return c.DownloadAsset(ctx, matched)
}

func (c *CatalogClient) downloadArchive(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return nil, errors.New("invalid plugin asset url")
	}
	if !strings.EqualFold(parsed.Scheme, "https") && !(c.AllowInsecureHTTP && strings.EqualFold(parsed.Scheme, "http")) {
		return nil, errors.New("plugin asset url must use https")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent())
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkGitHubResponse(resp); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxInspectArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxInspectArchiveBytes {
		return nil, errors.New("plugin archive exceeds inspection limit")
	}
	return data, nil
}

func (c *CatalogClient) setGitHubHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.userAgent())
}

func checkGitHubResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode == http.StatusForbidden && strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining")) == "0") {
		rl := &RateLimitError{}
		if resetRaw := strings.TrimSpace(resp.Header.Get("X-RateLimit-Reset")); resetRaw != "" {
			if sec, err := strconv.ParseInt(resetRaw, 10, 64); err == nil && sec > 0 {
				rl.ResetAt = time.Unix(sec, 0)
			}
		}
		if retryRaw := strings.TrimSpace(resp.Header.Get("Retry-After")); retryRaw != "" {
			if sec, err := strconv.Atoi(retryRaw); err == nil && sec > 0 {
				rl.RetryAfter = time.Duration(sec) * time.Second
			}
		}
		return rl
	}
	return fmt.Errorf("github api status %d", resp.StatusCode)
}

func hasNextLink(linkHeader string) bool {
	if linkHeader == "" {
		return false
	}
	for _, part := range strings.Split(linkHeader, ",") {
		if strings.Contains(part, `rel="next"`) {
			return true
		}
	}
	return false
}
