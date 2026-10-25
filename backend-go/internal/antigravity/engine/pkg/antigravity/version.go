package antigravity

// Antigravity Hub 客户端版本发现。
//
// 对齐 CLIProxyAPI 的 internal/misc/antigravity_version.go：Cloud Code 对低于
// 2.23.0 的客户端拒绝新模型，所以这里的回退版本必须 >= 2.23.0，并且后台会定期
// 从官方 Hub 的 auto-updater manifest 拉取最新版本覆盖回退值。
//
// 为什么不自己拍版本号：上游每次 Hub 发版都会改 UA 里的 version 与 changelist，
// 写死会随时间推移逐渐被判为旧客户端。

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// AntigravityHubFallbackVersion 是拿不到 Hub manifest 时的兜底版本。
	// Cloud Code 拒绝 2.23.0 以下客户端访问新模型，因此该值不可下调。
	AntigravityHubFallbackVersion = "2.24.0"

	// antigravityHubPlatform 与官方 Hub 客户端保持一致的平台标识。
	antigravityHubPlatform = "os_type=darwin; arch=arm64"

	// antigravityVersionCacheTTL 是版本缓存有效期；后台按其一半频率刷新。
	antigravityVersionCacheTTL = 6 * time.Hour

	// antigravityVersionFetchTimeout 是拉取 manifest 的超时。
	antigravityVersionFetchTimeout = 10 * time.Second

	// AntigravityNodeAPIClientUA 附加在 onboardUser 等控制面请求上。
	AntigravityNodeAPIClientUA = "google-api-nodejs-client/10.3.0"

	// AntigravityGoogAPIClientUA 是控制面请求的 X-Goog-Api-Client。
	AntigravityGoogAPIClientUA = "gl-node/22.21.1"
)

// antigravityHubLatestManifestURL 是官方 Hub 的 darwin-arm64 auto-updater manifest。
// 变量而非常量，便于测试替换。
var antigravityHubLatestManifestURL = "https://antigravity-hub-auto-updater-974169037036.us-central1.run.app/manifest/latest-arm64-mac.yml"

// antigravityHubChangelists 记录各 Hub 版本的构建 changelist。
// changelist 属于具体发版，不能套用到未知版本上，因此这里按版本精确匹配。
var antigravityHubChangelists = map[string]string{
	"2.23.0": "998057262",
	"2.24.0": "1000335682",
}

var (
	// hubVersion 是缓存的上游版本；未拉取成功时等于回退版本。
	hubVersion      = AntigravityHubFallbackVersion
	hubVersionMu    sync.RWMutex
	hubVersionUntil time.Time
	versionOnce     sync.Once
)

// StartVersionUpdater 启动后台版本刷新。传入进程级 ctx；重复调用只生效一次。
// 与 CLIProxyAPI 一致，刷新与请求执行解耦，避免请求路径被 manifest 拉取阻塞。
func StartVersionUpdater(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	versionOnce.Do(func() {
		go runVersionUpdater(ctx)
	})
}

func runVersionUpdater(ctx context.Context) {
	ticker := time.NewTicker(antigravityVersionCacheTTL / 2)
	defer ticker.Stop()

	refreshHubVersion(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshHubVersion(ctx)
		}
	}
}

func refreshHubVersion(ctx context.Context) {
	version, err := fetchHubLatestVersion(ctx)

	hubVersionMu.Lock()
	defer hubVersionMu.Unlock()

	now := time.Now()
	if err == nil {
		hubVersion = version
		hubVersionUntil = now.Add(antigravityVersionCacheTTL)
		return
	}
	// 拉取失败时只在缓存缺失或过期后回退，否则保留上一次成功的版本。
	if hubVersion == "" || now.After(hubVersionUntil) {
		hubVersion = AntigravityHubFallbackVersion
		hubVersionUntil = now.Add(antigravityVersionCacheTTL)
	}
}

// HubLatestVersion 返回当前缓存的 Hub 版本；缓存缺失或过期时返回回退版本。
func HubLatestVersion() string {
	hubVersionMu.RLock()
	if hubVersion != "" && time.Now().Before(hubVersionUntil) {
		v := hubVersion
		hubVersionMu.RUnlock()
		return v
	}
	hubVersionMu.RUnlock()
	return AntigravityHubFallbackVersion
}

// HubUserAgent 返回 Antigravity Hub 家族的 User-Agent。
func HubUserAgent() string {
	version := HubLatestVersion()
	metadata := "aidev_client; " + antigravityHubPlatform
	if changelist := antigravityHubChangelists[version]; changelist != "" {
		metadata += "; cl=" + changelist
	}
	return fmt.Sprintf("antigravity/hub/%s (%s)", version, metadata)
}

// isHubFamilyUserAgent 判断 UA 是否已经是 Antigravity Hub 家族格式。
func isHubFamilyUserAgent(lower string) bool {
	return strings.HasPrefix(lower, "antigravity/hub/") || strings.HasPrefix(lower, "antigravity/")
}

// hubBaseUserAgent 去掉 UA 上追加的 google-api-nodejs-client 尾巴，得到基础 UA。
func hubBaseUserAgent(userAgent string) string {
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		return HubUserAgent()
	}
	lower := strings.ToLower(userAgent)
	if isHubFamilyUserAgent(lower) {
		if idx := strings.Index(lower, " google-api-nodejs-client/"); idx >= 0 {
			if trimmed := strings.TrimSpace(userAgent[:idx]); trimmed != "" {
				return trimmed
			}
		}
	}
	return userAgent
}

// RequestUserAgent 返回生成/流式/模型列表请求使用的 UA。
func RequestUserAgent(userAgent string) string {
	return hubBaseUserAgent(userAgent)
}

// LoadCodeAssistUserAgent 返回 loadCodeAssist 使用的 UA（与请求 UA 同形）。
func LoadCodeAssistUserAgent(userAgent string) string {
	return RequestUserAgent(userAgent)
}

// OnboardUserUserAgent 返回 onboardUser 使用的长控制面 UA。
func OnboardUserUserAgent(userAgent string) string {
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		return HubUserAgent() + " " + AntigravityNodeAPIClientUA
	}
	lower := strings.ToLower(userAgent)
	if !isHubFamilyUserAgent(lower) {
		return userAgent
	}
	if strings.Contains(lower, "google-api-nodejs-client/") {
		return userAgent
	}
	return hubBaseUserAgent(userAgent) + " " + AntigravityNodeAPIClientUA
}

// VersionFromUserAgent 从短/长 UA 中取出 Antigravity 版本号。
func VersionFromUserAgent(userAgent string) string {
	base := hubBaseUserAgent(userAgent)
	lower := strings.ToLower(base)

	const hubPrefix = "antigravity/hub/"
	const legacyPrefix = "antigravity/"
	rest := ""
	switch {
	case strings.HasPrefix(lower, hubPrefix):
		rest = base[len(hubPrefix):]
	case strings.HasPrefix(lower, legacyPrefix):
		rest = base[len(legacyPrefix):]
	default:
		return HubLatestVersion()
	}
	if idx := strings.IndexAny(rest, " \t"); idx >= 0 {
		rest = rest[:idx]
	}
	if rest = strings.TrimSpace(rest); rest == "" {
		return HubLatestVersion()
	}
	return rest
}

// fetchHubLatestVersion 拉取并解析官方 Hub 的 auto-updater manifest。
func fetchHubLatestVersion(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// manifest 拉取本身不应拖长调用方（后台刷新）的生命周期。
	reqCtx, cancel := context.WithTimeout(ctx, antigravityVersionFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, antigravityHubLatestManifestURL, nil)
	if err != nil {
		return "", fmt.Errorf("build antigravity Hub manifest request: %w", err)
	}
	// 官方 auto-updater 用 electron-builder 的默认 UA 访问该端点。
	req.Header.Set("User-Agent", "electron-builder")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch antigravity Hub manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("antigravity Hub manifest returned status %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", fmt.Errorf("read antigravity Hub manifest: %w", err)
	}

	version := parseManifestVersion(string(raw))
	if version == "" {
		return "", errors.New("antigravity Hub manifest returned empty version")
	}
	if !isValidSemVersion(version) {
		return "", fmt.Errorf("antigravity Hub manifest returned invalid version %q", version)
	}
	return version, nil
}

// parseManifestVersion 从 electron-builder 的 yml manifest 中取出 version 字段。
// manifest 结构极简（一层 key: value），按行解析即可，不必引入 yaml 依赖。
func parseManifestVersion(manifest string) string {
	for _, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "version:") {
			continue
		}
		return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "version:")), `"'`)
	}
	return ""
}

// isValidSemVersion 校验 x.y.z 全数字版本号。
func isValidSemVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}
	return true
}

// resetHubVersionForTest 重置版本缓存；仅供单测使用。
func resetHubVersionForTest() {
	hubVersionMu.Lock()
	defer hubVersionMu.Unlock()
	hubVersion = AntigravityHubFallbackVersion
	hubVersionUntil = time.Time{}
}
