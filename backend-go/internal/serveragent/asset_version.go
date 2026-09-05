package serveragent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ==================== Release 资产版本号动态解析 ====================
//
// 转发相关二进制（api-monitor-relay / api-monitor-auth-proxy / api-monitor-stun）
// 以 GitHub Release 资产分发。为避免每次发版都要手动同步 hardcode 的 tag 版本号，
// 这里在服务启动时解析仓库 latest release 的 tag，并按 TTL 后台刷新，缓存于内存。
// 解析失败或尚未成功时回退到各资产函数内置的常量 URL，保证旧版本可用的退化路径。

const (
	releaseOwner   = "iwvw"
	releaseRepo    = "API-Monitor"
	releaseBaseURL = "https://github.com/" + releaseOwner + "/" + releaseRepo + "/releases/download/"
	latestTagAPI   = "https://api.github.com/repos/" + releaseOwner + "/" + releaseRepo + "/releases/latest"
	assetRefreshTTL = time.Hour
	assetHTTPTimeout = 10 * time.Second
)

type assetResolver struct {
	mu       sync.RWMutex
	tag      string
	lastOK   bool
	lastSeen time.Time
}

var releaseAssets = &assetResolver{}

// StartReleaseAssetResolver 启动资产版本号后台解析循环：立即解析一次，之后按 TTL 刷新。
func StartReleaseAssetResolver(ctx context.Context) {
	resolve := func() {
		tag, err := fetchLatestReleaseTag(ctx)
		if err != nil {
			return
		}
		releaseAssets.mu.Lock()
		releaseAssets.tag = tag
		releaseAssets.lastOK = true
		releaseAssets.lastSeen = time.Now()
		releaseAssets.mu.Unlock()
	}
	resolve()
	ticker := time.NewTicker(assetRefreshTTL)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			resolve()
		}
	}
}

// fetchLatestReleaseTag 查询仓库 latest release 的 tag_name。
func fetchLatestReleaseTag(ctx context.Context) (string, error) {
	return fetchReleaseTagFromURL(ctx, latestTagAPI)
}

// fetchReleaseTagFromURL 向任意 release-latest 接口请求并解析 tag_name（便于测试）。
func fetchReleaseTagFromURL(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "api-monitor/1.0")
	client := &http.Client{Timeout: assetHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256))
		return "", fmt.Errorf("latest release: http %d", resp.StatusCode)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.TagName), nil
}

// ReleaseAssetURL 返回指定资产的下载地址。优先使用解析到的 latest tag；否则返回 fallback。
func (r *assetResolver) ReleaseAssetURL(assetName, fallback string) string {
	r.mu.RLock()
	tag := r.tag
	r.mu.RUnlock()
	if tag != "" {
		return releaseBaseURL + tag + "/" + assetName
	}
	return fallback
}

// releaseAssetURL 供各资产函数使用：从 fallback URL 提取资产文件名，
// 交给 resolver 动态替换 tag 版本号（失败时原样返回 fallback）。
func releaseAssetURL(fallbackURL string) string {
	name := fallbackURL
	if i := strings.LastIndex(fallbackURL, "/"); i >= 0 {
		name = fallbackURL[i+1:]
	}
	return releaseAssets.ReleaseAssetURL(name, fallbackURL)
}

func (r *assetResolver) setTag(tag string) {
	r.mu.Lock()
	r.tag = tag
	r.mu.Unlock()
}

func (r *assetResolver) currentTag() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tag
}
