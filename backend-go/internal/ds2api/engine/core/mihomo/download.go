package mihomo

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

const (
	// defaultMihomoVersion 与 scripts/build-bridge-packages.sh 保持一致。
	defaultMihomoVersion = "v1.19.29"
	// githubMihomoMirror 是 GitHub 下载加速前缀；失败后兜底直连 GitHub。
	githubMihomoMirror = "https://ghfast.top/"
	// downloadTimeout 限制内核下载总耗时（含重试）。
	downloadTimeout = 10 * time.Minute
	// downloadMaxBytes 防止异常响应撑爆磁盘（mihomo 内核 < 100MB）。
	downloadMaxBytes = 256 << 20
)

// downloadState 记录一次内核下载任务的进度与结果。
type downloadState struct {
	state      string // idle|downloading|done|error
	target     string
	asset      string
	version    string
	startedAt  time.Time
	downloaded int64
	total      int64
	error      string
}

// downloadHTTPClient 可替换以便测试。
var downloadHTTPClient = &http.Client{Timeout: downloadTimeout}

// BinaryTarget 返回下载后二进制放置的目标路径（ds2api 根目录）。
func BinaryTarget() string {
	name := "mihomo"
	if runtime.GOOS == "windows" {
		name = "mihomo.exe"
	}
	return filepath.Join(config.BaseDir(), name)
}

// binaryAsset 计算当前平台对应的 mihomo 发布资产名与版本。
// 命名规则与 scripts/build-bridge-packages.sh 一致：
//   - windows: mihomo-windows-<arch>-<ver>.zip
//   - linux/darwin: mihomo-<os>-<arch>-<ver>.gz（单个二进制 gzip）
func binaryAsset() (asset, version string) {
	version = strings.TrimSpace(os.Getenv("MIHOMO_VERSION"))
	if version == "" {
		version = defaultMihomoVersion
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	ext := ".gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	asset = fmt.Sprintf("mihomo-%s-%s-%s%s", runtime.GOOS, runtime.GOARCH, version, ext)
	return asset, version
}

// releaseURL 返回 mihomo 发行资产的 GitHub 直连地址。
func releaseURL(asset, version string) string {
	return "https://github.com/MetaCubeX/mihomo/releases/download/" + version + "/" + asset
}

// DownloadInfo 汇总二进制探测结果与下载任务状态，供管理接口展示。
func (m *Manager) DownloadInfo() map[string]any {
	asset, ver := binaryAsset()
	info := map[string]any{
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
		"version":  ver,
		"asset":    asset,
		"target":   BinaryTarget(),
		"state":    "idle",
		"error":    "",
		"progress": 0,
		"found":    false,
	}
	if m == nil {
		return info
	}
	configured := ""
	if m.store != nil {
		configured = m.store.Snapshot().Mihomo.BinaryPath
	}
	if detectBinary(configured) {
		info["found"] = true
	}
	m.dlMu.Lock()
	dl := m.dl
	m.dlMu.Unlock()
	if dl.state != "" {
		info["state"] = dl.state
	}
	info["error"] = dl.error
	progress := 0
	if dl.total > 0 {
		progress = int(float64(dl.downloaded) / float64(dl.total) * 100)
		if progress > 100 {
			progress = 100
		}
	}
	info["progress"] = progress
	info["downloaded"] = dl.downloaded
	info["total"] = dl.total
	if !dl.startedAt.IsZero() {
		info["started_at"] = dl.startedAt.Unix()
	}
	return info
}

// StartBinaryDownload 在后台下载 mihomo 内核，解压改名后放到根目录。
// 二进制已存在或已有下载任务进行中时返回错误；下载在 goroutine 中执行，
// 通过 DownloadInfo 轮询进度。
func (m *Manager) StartBinaryDownload(_ context.Context) error {
	if m == nil {
		return errors.New("mihomo manager 未初始化")
	}
	if !m.Supported() {
		return errors.New("当前部署形态（Vercel）不支持 Mihomo 代理桥")
	}
	target := BinaryTarget()
	if st, err := os.Stat(target); err == nil && !st.IsDir() {
		return fmt.Errorf("mihomo 二进制已存在：%s", target)
	}

	m.dlMu.Lock()
	if m.dl.state == "downloading" {
		m.dlMu.Unlock()
		return errors.New("mihomo 内核正在下载中，请稍候")
	}
	asset, ver := binaryAsset()
	now := time.Now()
	m.dl = downloadState{
		state:     "downloading",
		target:    target,
		asset:     asset,
		version:   ver,
		startedAt: now,
	}
	m.dlMu.Unlock()

	config.Logger.Info("[mihomo] starting kernel download", "asset", asset, "target", target)
	go func() {
		err := runBinaryDownload(target, asset, ver, func(downloaded, total int64) {
			m.dlMu.Lock()
			if m.dl.state == "downloading" {
				m.dl.downloaded = downloaded
				m.dl.total = total
			}
			m.dlMu.Unlock()
		})
		m.dlMu.Lock()
		if err != nil {
			m.dl.state = "error"
			m.dl.error = err.Error()
		} else {
			m.dl.state = "done"
			m.dl.downloaded = m.dl.total
		}
		m.dlMu.Unlock()
		config.Logger.Info("[mihomo] kernel download finished", "target", target, "error", err)
	}()
	return nil
}

// runBinaryDownload 完成 下载 -> 解压 -> 改名 -> 落到根目录 的全流程。
func runBinaryDownload(target, asset, version string, onProgress func(downloaded, total int64)) error {
	partPath := target + ".part"
	if err := fetchArchive(partPath, asset, version, onProgress); err != nil {
		if removeErr := os.Remove(partPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			config.Logger.Warn("[mihomo] remove download part failed", "error", removeErr)
		}
		return err
	}
	tmpPath := target + ".tmp"
	if err := extractBinary(partPath, asset, tmpPath); err != nil {
		if removeErr := os.Remove(partPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			config.Logger.Warn("[mihomo] remove download part failed", "error", removeErr)
		}
		if removeErr := os.Remove(tmpPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			config.Logger.Warn("[mihomo] remove temp binary failed", "error", removeErr)
		}
		return err
	}
	if removeErr := os.Remove(partPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		config.Logger.Warn("[mihomo] remove download part failed", "error", removeErr)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		if removeErr := os.Remove(tmpPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			config.Logger.Warn("[mihomo] remove temp binary failed", "error", removeErr)
		}
		return fmt.Errorf("安装 mihomo 二进制失败: %w", err)
	}
	return nil
}

// fetchArchive 先走镜像再兜底直连 GitHub，把发布资产下载到 dst。
func fetchArchive(dst, asset, version string, onProgress func(downloaded, total int64)) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if onProgress == nil {
		onProgress = func(int64, int64) {}
	}
	urls := []string{
		githubMihomoMirror + releaseURL(asset, version),
		releaseURL(asset, version),
	}
	var lastErr error
	for _, u := range urls {
		if err := downloadURL(dst, u, onProgress); err != nil {
			lastErr = err
			config.Logger.Warn("[mihomo] download attempt failed", "url", u, "error", err)
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = errors.New("无可用下载地址")
	}
	return fmt.Errorf("mihomo 内核下载失败: %w", lastErr)
}

func downloadURL(dst, url string, onProgress func(downloaded, total int64)) error {
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ds2api")
	resp, err := downloadHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			config.Logger.Warn("[mihomo] close download body failed", "error", closeErr)
		}
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	total := resp.ContentLength
	if total < 0 {
		total = 0
	}
	if total > downloadMaxBytes {
		return fmt.Errorf("下载内容过大（%.1f MB）", float64(total)/(1<<20))
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := copyBounded(out, &progressReader{r: resp.Body, total: total, onProgress: onProgress}, downloadMaxBytes)
	if closeErr := out.Close(); closeErr != nil {
		config.Logger.Warn("[mihomo] close download output failed", "error", closeErr)
	}
	if copyErr != nil {
		if removeErr := os.Remove(dst); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			config.Logger.Warn("[mihomo] remove partial download failed", "error", removeErr)
		}
		if errors.Is(copyErr, errContentTooLarge) {
			return errors.New("下载内容过大")
		}
		return fmt.Errorf("写入下载内容失败: %w", copyErr)
	}
	return nil
}

// progressReader 统计已下载字节数并回调进度。
type progressReader struct {
	r          io.Reader
	total      int64
	n          int64
	onProgress func(downloaded, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.n += int64(n)
	if p.onProgress != nil {
		p.onProgress(p.n, p.total)
	}
	return n, err
}

// errContentTooLarge 标记内容超出大小限制（异常响应 / 解压膨胀）。
var errContentTooLarge = errors.New("内容超出大小限制")

// copyBounded 最多从 src 复制 max 字节到 dst；超出立即返回错误并停止读取，
// 防止异常响应或解压炸弹把无限内容撑爆磁盘（Content-Length 未知时尤其关键）。
func copyBounded(dst io.Writer, src io.Reader, max int64) (int64, error) {
	written, err := io.Copy(dst, io.LimitReader(src, max+1))
	if err != nil {
		return written, err
	}
	if written > max {
		return written, errContentTooLarge
	}
	return written, nil
}

// extractBinary 从 zip / gzip 中解出 mihomo 可执行文件写到 destPath。
func extractBinary(archivePath, asset, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	if strings.HasSuffix(strings.ToLower(asset), ".zip") {
		return extractFromZip(archivePath, destPath)
	}
	return extractFromGzip(archivePath, destPath)
}

func extractFromZip(archivePath, destPath string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := r.Close(); closeErr != nil {
			config.Logger.Warn("[mihomo] close zip reader failed", "error", closeErr)
		}
	}()
	for _, f := range r.File {
		base := strings.ToLower(filepath.Base(f.Name))
		if !strings.Contains(base, "mihomo") || !strings.HasSuffix(base, ".exe") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			if closeErr := rc.Close(); closeErr != nil {
				config.Logger.Warn("[mihomo] close zip entry failed", "error", closeErr)
			}
			return err
		}
		_, copyErr := copyBounded(out, rc, downloadMaxBytes)
		if closeErr := out.Close(); closeErr != nil {
			config.Logger.Warn("[mihomo] close binary output failed", "error", closeErr)
		}
		if closeErr := rc.Close(); closeErr != nil {
			config.Logger.Warn("[mihomo] close zip entry failed", "error", closeErr)
		}
		if copyErr != nil {
			return fmt.Errorf("解压 mihomo 二进制失败: %w", copyErr)
		}
		return nil
	}
	return errors.New("压缩包中未找到 mihomo 可执行文件")
}

func extractFromGzip(archivePath, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		if closeErr := f.Close(); closeErr != nil {
			config.Logger.Warn("[mihomo] close gzip file failed", "error", closeErr)
		}
		return err
	}
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		if closeErr := gz.Close(); closeErr != nil {
			config.Logger.Warn("[mihomo] close gzip failed", "error", closeErr)
		}
		if closeErr := f.Close(); closeErr != nil {
			config.Logger.Warn("[mihomo] close gzip file failed", "error", closeErr)
		}
		return err
	}
	_, copyErr := copyBounded(out, gz, downloadMaxBytes)
	if closeErr := out.Close(); closeErr != nil {
		config.Logger.Warn("[mihomo] close binary output failed", "error", closeErr)
	}
	if closeErr := gz.Close(); closeErr != nil {
		config.Logger.Warn("[mihomo] close gzip failed", "error", closeErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		config.Logger.Warn("[mihomo] close gzip file failed", "error", closeErr)
	}
	if copyErr != nil {
		return fmt.Errorf("解压 mihomo 二进制失败: %w", copyErr)
	}
	return nil
}
